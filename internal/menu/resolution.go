package menu

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/eventstore"
	apperrors "github.com/mekusolutions-design/SukariBurgersGo/pkg/errors"
	"github.com/mekusolutions-design/SukariBurgersGo/pkg/money"
)

type ProductionType string

const (
	ProductionRecipe  ProductionType = "recipe"
	ProductionStocked ProductionType = "stocked"
)

// StockDeduction is one stock move + consumption cost snapshot (Michael consumption spec).
// Quantity = amount deducted from InventoryProjection (usually pieces for FG).
// UnitCost = cost per Quantity unit for value (for FG: unit_weight × cost_per_yield_unit).
// YieldQuantity = pieces × unit_weight (grams/ml) for Consumption page reporting.
type StockDeduction struct {
	ItemID        string  `json:"item_id"`
	ItemName      string  `json:"item_name"`
	Quantity      float64 `json:"quantity"`
	Unit          string  `json:"unit"`
	UnitCost      float64 `json:"unit_cost"`
	Kind          string  `json:"kind,omitempty"`           // inventory | finished_good
	UnitWeight    float64 `json:"unit_weight,omitempty"`    // per piece, in yield units
	CostPerYield  float64 `json:"cost_per_yield,omitempty"` // batch_cost / standard_yield
	YieldQuantity float64 `json:"yield_quantity,omitempty"` // pieces × unit_weight
	LineCost      float64 `json:"line_cost,omitempty"`      // quantity × unit_cost
}

type ResolvedMenuItem struct {
	MenuItemID     string           `json:"menu_item_id"`
	Name           string           `json:"name"`
	ProductionType ProductionType   `json:"production_type"`
	Quantity       float64          `json:"quantity"`
	UnitPrice      float64          `json:"unit_price"`
	Deductions     []StockDeduction `json:"deductions"`
	GroupIndex     *int             `json:"group_index,omitempty"`
	GroupName      string           `json:"group_name,omitempty"`
}

type ComboSelectionInput struct {
	GroupIndex  int      `json:"group_index"`
	MenuItemIDs []string `json:"menu_item_ids"`
}

// UnmarshalJSON accepts Nest/web snake_case and camelCase for combo selections.
func (c *ComboSelectionInput) UnmarshalJSON(b []byte) error {
	var raw map[string]interface{}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if v, ok := raw["group_index"]; ok {
		c.GroupIndex = int(asJSONFloat(v))
	} else if v, ok := raw["groupIndex"]; ok {
		c.GroupIndex = int(asJSONFloat(v))
	}
	c.MenuItemIDs = asJSONStringSlice(raw["menu_item_ids"])
	if len(c.MenuItemIDs) == 0 {
		c.MenuItemIDs = asJSONStringSlice(raw["menuItemIds"])
	}
	return nil
}

func asJSONFloat(v interface{}) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	}
	return 0
}

func asJSONStringSlice(v interface{}) []string {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

type ResolvedSaleLine struct {
	LineType        string                 `json:"line_type"`
	Revenue         float64                `json:"revenue"`
	Label           string                 `json:"label"`
	MenuItems       []ResolvedMenuItem     `json:"menu_items"`
	StockDeductions []StockDeduction       `json:"stock_deductions"`
	Metadata        map[string]interface{} `json:"metadata"`
}

type ResolutionService struct {
	pool       *pgxpool.Pool
	eventStore *eventstore.Store
}

func NewResolutionService(pool *pgxpool.Pool) *ResolutionService {
	return &ResolutionService{pool: pool}
}

// SetEventStore wires event append for Nest-parity menu create.
func (s *ResolutionService) SetEventStore(es *eventstore.Store) {
	s.eventStore = es
}

// ResolveMenuItemLine resolves a single menu item to stock deductions.
// Matches Nest sellable-resolution:
//
//	CatalogMenuItem → recipe / stock_item
//	else menu_created events → finished_good_id (or lines)
//	else recipe by code / item_id
//	else direct inventory SKU
func (s *ResolutionService) ResolveMenuItemLine(ctx context.Context, shopID, menuItemID string, qty float64) (*ResolvedMenuItem, error) {
	if qty <= 0 {
		return nil, apperrors.BadRequest("quantity must be positive")
	}
	menuItemID = strings.TrimSpace(menuItemID)
	if menuItemID == "" {
		return nil, apperrors.BadRequest("menu_item_id is required")
	}

	// 1) CatalogMenuItem (optional table; may be empty in Nest-first shops)
	const cq = `
		SELECT menu_item_id, name, production_type::text, recipe_id, stock_item_id,
		       COALESCE(selling_price::text, '0')
		FROM "CatalogMenuItem"
		WHERE shop_id = $1 AND menu_item_id = $2 AND active = true
	`
	var mid, name, prodType string
	var recipeID, stockItemID *string
	var priceStr string
	err := s.pool.QueryRow(ctx, cq, shopID, menuItemID).Scan(&mid, &name, &prodType, &recipeID, &stockItemID, &priceStr)
	if err == nil {
		price := 0.0
		if m, e := money.New(priceStr); e == nil {
			price = m.Float64()
		}
		// POS rule (Michael 3 Oct 2026): never explode recipe to raw ingredients at sale.
		// Catalog recipe → deduct finished output SKU of the recipe (or stock_item_id), not ingredients.
		if (prodType == "recipe" || prodType == "RECIPE") && recipeID != nil && *recipeID != "" {
			if outSKU := s.recipeOutputItemID(ctx, *recipeID); outSKU != "" {
				d, e2 := s.directStockDeduction(ctx, shopID, outSKU, qty)
				if e2 == nil {
					return &ResolvedMenuItem{
						MenuItemID: mid, Name: name, ProductionType: ProductionStocked,
						Quantity: qty, UnitPrice: price, Deductions: []StockDeduction{d},
					}, nil
				}
			}
		}
		sku := ""
		if stockItemID != nil {
			sku = strings.TrimSpace(*stockItemID)
		}
		if sku != "" {
			d, e2 := s.directStockDeduction(ctx, shopID, sku, qty)
			if e2 == nil {
				return &ResolvedMenuItem{
					MenuItemID: mid, Name: name, ProductionType: ProductionStocked,
					Quantity: qty, UnitPrice: price, Deductions: []StockDeduction{d},
				}, nil
			}
		}
	}

	// 2) Nest menu fold: FIXED FG + Inventory Select option_item_ids (Michael POS deduct)
	menuName, price, rawLines := s.lookupMenuRecord(ctx, shopID, menuItemID)
	if len(rawLines) > 0 || menuName != "" {
		deductions, anyRecipe, errD := s.deductionsFromMenuLines(ctx, shopID, qty, rawLines)
		if errD != nil {
			return nil, errD
		}
		if len(deductions) > 0 {
			pt := ProductionStocked
			if anyRecipe {
				pt = ProductionRecipe
			}
			if menuName == "" {
				menuName = menuItemID
			}
			return &ResolvedMenuItem{
				MenuItemID: menuItemID, Name: menuName, ProductionType: pt,
				Quantity: qty, UnitPrice: price, Deductions: AggregateDeductions(deductions),
			}, nil
		}
		// Legacy: only finished_good_id on header
		fgID, _, _ := s.lookupMenuFinishedGood(ctx, shopID, menuItemID)
		if fgID != "" {
			// FG component → FG stock only (never raw recipe of FG)
			d, e3 := s.directStockDeduction(ctx, shopID, fgID, qty)
			if e3 != nil {
				return nil, e3
			}
			return &ResolvedMenuItem{
				MenuItemID: menuItemID, Name: menuName, ProductionType: ProductionStocked,
				Quantity: qty, UnitPrice: price, Deductions: []StockDeduction{d},
			}, nil
		}
	}

	// 3) Last-resort: if this id is a stocked FG/SKU, deduct it — do NOT expand recipe ingredients at POS
	// (Raw chicken was already deducted at production when FG was made.)

	// 4) Direct inventory SKU (last resort)
	d, e3 := s.directStockDeduction(ctx, shopID, menuItemID, qty)
	if e3 != nil {
		return nil, e3
	}
	return &ResolvedMenuItem{
		MenuItemID: menuItemID, Name: d.ItemName, ProductionType: ProductionStocked,
		Quantity: qty, UnitPrice: 0, Deductions: []StockDeduction{d},
	}, nil
}

// lookupMenuFinishedGood scans Nest menu events for finished_good_id / lines.
func (s *ResolutionService) lookupMenuFinishedGood(ctx context.Context, shopID, menuItemID string) (fgID, name string, price float64) {
	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(payload::text, '{}')
		FROM "Event"
		WHERE shop_id = $1 AND event_type IN ('menu_created', 'menu_item_added')
		ORDER BY created_at ASC
		LIMIT 3000
	`, shopID)
	if err != nil {
		return "", "", 0
	}
	defer rows.Close()

	want := strings.ToUpper(strings.TrimSpace(menuItemID))
	for rows.Next() {
		var payloadStr string
		if err := rows.Scan(&payloadStr); err != nil {
			continue
		}
		var p map[string]interface{}
		if json.Unmarshal([]byte(payloadStr), &p) != nil || p == nil {
			continue
		}
		candidates := []string{
			strVal(p["menu_id"]),
			strVal(p["menuId"]),
			strVal(p["menu_item_id"]),
			strVal(p["menu_code"]),
			strVal(p["menuCode"]),
		}
		match := false
		for _, c := range candidates {
			if strings.ToUpper(strings.TrimSpace(c)) == want {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		name = strVal(p["name"])
		if name == "" {
			name = strVal(p["menu_name"])
		}
		if name == "" {
			name = menuItemID
		}
		price = numVal(p["selling_price"])
		if price == 0 {
			price = numVal(p["price"])
		}
		fgID = strVal(p["finished_good_id"])
		if fgID == "" {
			fgID = strVal(p["finishedGoodId"])
		}
		// Nested lines: first FIXED finished good
		if fgID == "" {
			if lines, ok := p["lines"].([]interface{}); ok {
				for _, raw := range lines {
					lm, ok := raw.(map[string]interface{})
					if !ok {
						continue
					}
					ct := strings.ToUpper(strVal(lm["component_type"]))
					if ct == "" {
						ct = strings.ToUpper(strVal(lm["componentType"]))
					}
					if ct != "" && ct != "FIXED" {
						continue
					}
					fgID = strVal(lm["finished_good_id"])
					if fgID == "" {
						fgID = strVal(lm["finishedGoodId"])
					}
					if fgID != "" {
						break
					}
				}
			}
		}
		if fgID != "" {
			return fgID, name, price
		}
		// Keep last matched name/price even without FG (fall through callers)
		return "", name, price
	}
	return "", "", 0
}

func strVal(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return fmt.Sprintf("%.0f", t)
	default:
		return ""
	}
}

func numVal(v interface{}) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case string:
		var f float64
		fmt.Sscanf(t, "%f", &f)
		return f
	default:
		return 0
	}
}

// ResolveComboLine validates combo selection groups and resolves each chosen menu item.
func (s *ResolutionService) ResolveComboLine(ctx context.Context, shopID, comboID string, soldQty float64, selections []ComboSelectionInput) (*ResolvedSaleLine, error) {
	if soldQty <= 0 {
		return nil, apperrors.BadRequest("combo quantity must be positive")
	}

	const cq = `
		SELECT id, combo_id, name, selling_price
		FROM "Combo"
		WHERE shop_id = $1 AND combo_id = $2 AND active = true
	`
	var comboUUID, cid, cname string
	var priceStr string
	err := s.pool.QueryRow(ctx, cq, shopID, comboID).Scan(&comboUUID, &cid, &cname, &priceStr)
	if err != nil {
		return nil, apperrors.NotFound(fmt.Sprintf("Combo %s not found", comboID))
	}
	price := 0.0
	if m, e := money.New(priceStr); e == nil {
		price = m.Float64()
	}

	// Load selection groups
	const gq = `
		SELECT group_index, menu_category_id, quantity
		FROM "ComboSelectionGroup"
		WHERE combo_id = $1
		ORDER BY group_index
	`
	rows, err := s.pool.Query(ctx, gq, comboUUID)
	if err != nil {
		return nil, fmt.Errorf("combo groups: %w", err)
	}
	defer rows.Close()

	type group struct {
		Index      int
		CategoryID string
		Quantity   int
	}
	var groups []group
	for rows.Next() {
		var g group
		if err := rows.Scan(&g.Index, &g.CategoryID, &g.Quantity); err != nil {
			return nil, err
		}
		if g.Quantity < 1 {
			g.Quantity = 1
		}
		groups = append(groups, g)
	}

	// Validate selections against groups
	selByGroup := map[int][]string{}
	for _, sel := range selections {
		selByGroup[sel.GroupIndex] = append(selByGroup[sel.GroupIndex], sel.MenuItemIDs...)
	}
	// Nest parity: classic (len==group.qty → ×soldQty) OR expanded (len==group.qty×soldQty → ×1).
	// Never both — that doubles consumption.
	groupMode := map[int]string{}
	for _, g := range groups {
		chosen := selByGroup[g.Index]
		perCombo := g.Quantity
		if perCombo < 0 {
			perCombo = 0
		}
		sq := int(soldQty)
		if sq < 1 {
			sq = 1
		}
		classicN := perCombo
		expandedN := perCombo * sq
		if len(chosen) == classicN {
			groupMode[g.Index] = "classic"
		} else if len(chosen) == expandedN {
			groupMode[g.Index] = "expanded"
		} else {
			return nil, apperrors.BadRequest(fmt.Sprintf(
				"combo group %d requires %d selection(s) (or %d for qty %.0f), got %d",
				g.Index, classicN, expandedN, soldQty, len(chosen),
			))
		}
	}

	knownGroups := map[int]bool{}
	for _, g := range groups {
		knownGroups[g.Index] = true
	}
	for idx := range selByGroup {
		if !knownGroups[idx] {
			return nil, apperrors.BadRequest(fmt.Sprintf("unknown combo group_index %d", idx))
		}
	}

	var allMenuItems []ResolvedMenuItem
	var allDeductions []StockDeduction
	for _, g := range groups {
		chosen := selByGroup[g.Index]
		unitQty := 1.0
		if groupMode[g.Index] == "classic" {
			unitQty = soldQty
		}
		for _, mid := range chosen {
			if err := s.assertMenuItemInCategory(ctx, shopID, g.CategoryID, mid); err != nil {
				return nil, err
			}
			resolved, err := s.ResolveMenuItemLine(ctx, shopID, mid, unitQty)
			if err != nil {
				return nil, err
			}
			gi := g.Index
			resolved.GroupIndex = &gi
			allMenuItems = append(allMenuItems, *resolved)
			allDeductions = append(allDeductions, resolved.Deductions...)
		}
	}
	agg := AggregateDeductions(allDeductions)
	if len(agg) == 0 {
		return nil, apperrors.BadRequest("combo resolves to zero stock deductions")
	}
	return &ResolvedSaleLine{
		LineType: "combo", Revenue: price * soldQty, Label: cname,
		MenuItems: allMenuItems, StockDeductions: agg,
		Metadata: map[string]interface{}{"combo_id": comboID, "combo_name": cname, "quantity": soldQty, "unit_selling_price": price},
	}, nil
}

// assertMenuItemInCategory matches Nest getCategory + allowed Set(menuItemIds).
// Nest stores membership on menu_category_* events (menu_item_ids), not only CatalogMenuItem.
func (s *ResolutionService) assertMenuItemInCategory(ctx context.Context, shopID, categoryKey, menuItemID string) error {
	menuItemID = strings.TrimSpace(menuItemID)
	categoryKey = strings.TrimSpace(categoryKey)
	if menuItemID == "" {
		return apperrors.BadRequest("menu item id required for combo selection")
	}
	if categoryKey == "" {
		// No category constraint — allow if menu resolves
		return nil
	}

	allowed, catName, err := s.categoryAllowedMenuIDs(ctx, shopID, categoryKey)
	if err != nil {
		return err
	}
	if len(allowed) == 0 {
		// Open / empty category pool: accept any resolvable menu item (Nest UX when category not yet linked)
		if _, rerr := s.ResolveMenuItemLine(ctx, shopID, menuItemID, 1); rerr == nil {
			return nil
		}
		return apperrors.BadRequest(fmt.Sprintf(
			"menu item %s is not valid for this combo selection group", menuItemID,
		))
	}

	want := strings.ToUpper(menuItemID)
	for id := range allowed {
		if strings.ToUpper(id) == want {
			return nil
		}
		// MENU-04 vs 04, MENU-BBQ vs BBQ CHICKEN style codes
		if strings.ToUpper("MENU-"+id) == want || strings.ToUpper(strings.TrimPrefix(id, "MENU-")) == strings.TrimPrefix(want, "MENU-") {
			return nil
		}
	}

	// Catalog join fallback (legacy table membership)
	const q = `
		SELECT 1 FROM "MenuCategoryItem" mci
		LEFT JOIN "CatalogMenuItem" cmi ON (cmi.id = mci.menu_item_id OR cmi.menu_item_id = mci.menu_item_id)
		WHERE (mci.category_id = $1)
		  AND (
		    mci.menu_item_id = $2
		    OR cmi.menu_item_id = $2
		    OR cmi.id = $2
		    OR LOWER(cmi.menu_item_id) = LOWER($2)
		  )
		LIMIT 1`
	var one int
	if err := s.pool.QueryRow(ctx, q, categoryKey, menuItemID).Scan(&one); err == nil {
		return nil
	}

	label := catName
	if label == "" {
		label = categoryKey
	}
	return apperrors.BadRequest(fmt.Sprintf(
		"menu item %s is not in category %s (not valid for this combo selection group)",
		menuItemID, label,
	))
}

// categoryAllowedMenuIDs folds Nest menu_category_created/updated + MenuCategoryItem rows.
func (s *ResolutionService) categoryAllowedMenuIDs(ctx context.Context, shopID, categoryKey string) (map[string]struct{}, string, error) {
	allowed := map[string]struct{}{}
	catName := ""

	// 1) Event-sourced categories (Nest listMenuCategories)
	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(payload::text, '{}') FROM "Event"
		WHERE shop_id = $1
		  AND event_type IN ('menu_category_created','menu_category_updated','MENU_CATEGORY_CREATED')
		ORDER BY created_at ASC
		LIMIT 2000`, shopID)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var raw []byte
			if rows.Scan(&raw) != nil {
				continue
			}
			var p map[string]interface{}
			if json.Unmarshal(raw, &p) != nil {
				continue
			}
			id := strVal(p["category_id"])
			if id == "" {
				id = strVal(p["categoryId"])
			}
			if id == "" {
				id = strVal(p["id"])
			}
			if id != categoryKey && !strings.EqualFold(id, categoryKey) {
				// Also match UUID primary key stored on ComboSelectionGroup vs business category_id
				continue
			}
			if n := strVal(p["name"]); n != "" {
				catName = n
			}
			for _, key := range []string{"menu_item_ids", "menuItemIds", "menu_ids", "menuIds"} {
				switch arr := p[key].(type) {
				case []interface{}:
					for _, e := range arr {
						if s, ok := e.(string); ok && strings.TrimSpace(s) != "" {
							allowed[strings.TrimSpace(s)] = struct{}{}
						}
					}
				case []string:
					for _, s := range arr {
						if strings.TrimSpace(s) != "" {
							allowed[strings.TrimSpace(s)] = struct{}{}
						}
					}
				}
			}
		}
	}

	// 2) Table MenuCategory by id OR category_id
	var tableID, tableCatID, tableName string
	_ = s.pool.QueryRow(ctx, `
		SELECT id, category_id, name FROM "MenuCategory"
		WHERE shop_id = $1 AND (id = $2 OR category_id = $2)
		LIMIT 1`, shopID, categoryKey).Scan(&tableID, &tableCatID, &tableName)
	if tableName != "" {
		catName = tableName
	}
	catPK := tableID
	if catPK == "" {
		catPK = categoryKey
	}
	irows, ierr := s.pool.Query(ctx, `
		SELECT COALESCE(mci.menu_item_id,''), COALESCE(cmi.menu_item_id,''), COALESCE(cmi.id::text,'')
		FROM "MenuCategoryItem" mci
		LEFT JOIN "CatalogMenuItem" cmi ON (cmi.id = mci.menu_item_id OR cmi.menu_item_id = mci.menu_item_id)
		WHERE mci.category_id = $1 OR mci.category_id = $2`, catPK, categoryKey)
	if ierr == nil {
		defer irows.Close()
		for irows.Next() {
			var a, b, c string
			if irows.Scan(&a, &b, &c) != nil {
				continue
			}
			for _, x := range []string{a, b, c} {
				if strings.TrimSpace(x) != "" {
					allowed[strings.TrimSpace(x)] = struct{}{}
				}
			}
		}
	}

	// 3) Menus whose payload.category matches category name/code (soft Nest fold)
	if len(allowed) == 0 && catName != "" {
		mrows, _ := s.pool.Query(ctx, `
			SELECT COALESCE(payload::text,'{}') FROM "Event"
			WHERE shop_id = $1 AND event_type IN ('menu_created','menu_item_added')
			ORDER BY created_at ASC LIMIT 3000`, shopID)
		if mrows != nil {
			defer mrows.Close()
			for mrows.Next() {
				var raw []byte
				if mrows.Scan(&raw) != nil {
					continue
				}
				var p map[string]interface{}
				if json.Unmarshal(raw, &p) != nil {
					continue
				}
				cat := strVal(p["category"])
				if cat == "" {
					continue
				}
				if !strings.EqualFold(cat, catName) && !strings.EqualFold(cat, categoryKey) {
					continue
				}
				mid := strVal(p["menu_id"])
				if mid == "" {
					mid = strVal(p["menuId"])
				}
				if mid != "" {
					allowed[mid] = struct{}{}
				}
			}
		}
	}

	return allowed, catName, nil
}

// recipeOutputItemID returns the finished-good / output item linked to a recipe (POS deduct target).
func (s *ResolutionService) recipeOutputItemID(ctx context.Context, recipeKey string) string {
	var itemID string
	_ = s.pool.QueryRow(ctx, `
		SELECT COALESCE(item_id, '') FROM "Recipe"
		WHERE id = $1 OR recipe_code = $1 OR item_id = $1
		LIMIT 1`, recipeKey).Scan(&itemID)
	return strings.TrimSpace(itemID)
}

func (s *ResolutionService) expandRecipe(ctx context.Context, shopID, recipeKey string, qty float64) ([]StockDeduction, error) {
	// recipeKey may be recipe.id or recipe_code
	const rq = `
		SELECT r.id, r.recipe_code
		FROM "Recipe" r
		WHERE r.is_active = true AND (r.id = $1 OR r.recipe_code = $1 OR r.item_id = $1)
		LIMIT 1
	`
	var recipeID, code string
	err := s.pool.QueryRow(ctx, rq, recipeKey).Scan(&recipeID, &code)
	if err != nil {
		return nil, nil // not a recipe
	}

	const iq = `
		SELECT raw_item_id, raw_item_name, quantity_per_unit, unit, unit_cost
		FROM "RecipeIngredient"
		WHERE recipe_id = $1
	`
	rows, err := s.pool.Query(ctx, iq, recipeID)
	if err != nil {
		return nil, fmt.Errorf("recipe ingredients: %w", err)
	}
	defer rows.Close()

	var lines []StockDeduction
	for rows.Next() {
		var rawID, rawName, unit string
		var qtyPer float64
		var unitCostStr *string
		if err := rows.Scan(&rawID, &rawName, &qtyPer, &unit, &unitCostStr); err != nil {
			return nil, err
		}
		deductQty := qtyPer * qty
		uc := 0.0
		if unitCostStr != nil {
			if m, e := money.New(*unitCostStr); e == nil {
				uc = m.Float64()
			}
		}
		if uc <= 0 {
			uc, _ = s.unitCost(ctx, shopID, rawID)
		}
		lines = append(lines, StockDeduction{
			ItemID: rawID, ItemName: rawName, Quantity: deductQty, Unit: unit, UnitCost: uc,
		})
	}
	return lines, rows.Err()
}

func (s *ResolutionService) directStockDeduction(ctx context.Context, shopID, itemID string, qty float64) (StockDeduction, error) {
	name := itemID
	// ── Finished good via RecipeOutput: cost = pieces × unit_weight × (batch/yield) ──
	if d, ok := s.finishedGoodDeduction(ctx, shopID, itemID, qty); ok {
		return d, nil
	}
	uc, n := s.unitCost(ctx, shopID, itemID)
	if n != "" {
		name = n
	}
	line := qty * uc
	return StockDeduction{
		ItemID: itemID, ItemName: name, Quantity: qty, Unit: "pcs", UnitCost: uc,
		Kind: "inventory", LineCost: line,
	}, nil
}

// finishedGoodDeduction implements Michael FG cost:
// pieces × unit_weight × (batch_cost / standard_yield). Never treat cpy as per-piece alone.
func (s *ResolutionService) finishedGoodDeduction(ctx context.Context, shopID, itemID string, pieces float64) (StockDeduction, bool) {
	if pieces <= 0 {
		return StockDeduction{}, false
	}
	var outID, recipeID, outName string
	var unitWeight, stdYield float64
	err := s.pool.QueryRow(ctx, `
		SELECT ro.id, ro.recipe_id, COALESCE(ro.unit_weight, 0),
		       COALESCE(r.standard_yield, COALESCE(r.yield_quantity, 0)),
		       COALESCE(ro.item_name, ro.item_id, $1)
		FROM "RecipeOutput" ro
		JOIN "Recipe" r ON r.id = ro.recipe_id
		WHERE ro.item_id = $1 AND COALESCE(r.is_active, true) AND COALESCE(ro.is_active, true)
		ORDER BY r.updated_at DESC NULLS LAST
		LIMIT 1`, itemID).Scan(&outID, &recipeID, &unitWeight, &stdYield, &outName)
	if err != nil || recipeID == "" {
		// Fallback: Recipe.item_id is the FG itself (single-output)
		_ = s.pool.QueryRow(ctx, `
			SELECT id, COALESCE(standard_yield, 0), COALESCE(item_name, item_id, $1)
			FROM "Recipe"
			WHERE is_active AND (item_id = $1 OR recipe_code = $1)
			ORDER BY updated_at DESC NULLS LAST LIMIT 1`, itemID).Scan(&recipeID, &stdYield, &outName)
		if recipeID == "" {
			return StockDeduction{}, false
		}
		unitWeight = 1 // treat as 1 yield-unit per piece when no RecipeOutput.weight
	}
	if outName == "" {
		outName = itemID
	}
	if stdYield <= 0 {
		stdYield = 1
	}
	batchCost := s.recipeBatchCost(ctx, shopID, recipeID)
	cpy := batchCost / stdYield
	// Prefer inventory/production WAC as cost-per-piece when no unit_weight (already pcs)
	if unitWeight <= 0 {
		unitWeight = 1
	}
	costPerPiece := unitWeight * cpy
	// If production/inventory has a stronger per-piece WAC and unit_weight is 1, prefer WAC
	if unitWeight == 1 {
		if wac, n := s.unitCost(ctx, shopID, itemID); wac > 0 {
			costPerPiece = wac
			if n != "" {
				outName = n
			}
		}
	}
	line := pieces * costPerPiece
	return StockDeduction{
		ItemID: itemID, ItemName: outName, Quantity: pieces, Unit: "pcs",
		UnitCost: costPerPiece, Kind: "finished_good",
		UnitWeight: unitWeight, CostPerYield: cpy,
		YieldQuantity: pieces * unitWeight, LineCost: line,
	}, true
}

func (s *ResolutionService) recipeBatchCost(ctx context.Context, shopID, recipeID string) float64 {
	// Sum ingredient costs for one standard batch (no batch_cost column on Recipe in Nest schema)
	rows, err := s.pool.Query(ctx, `
		SELECT quantity_per_unit, COALESCE(unit_cost,0)::text, COALESCE(raw_item_id,'')
		FROM "RecipeIngredient" WHERE recipe_id = $1`, recipeID)
	if err != nil {
		return 0
	}
	defer rows.Close()
	batch := 0.0
	for rows.Next() {
		var q float64
		var ucS, rawID string
		if rows.Scan(&q, &ucS, &rawID) != nil {
			continue
		}
		uc := 0.0
		if m, e := money.New(ucS); e == nil {
			uc = m.Float64()
		}
		if uc <= 0 && rawID != "" {
			uc, _ = s.unitCost(ctx, shopID, rawID)
		}
		batch += q * uc
	}
	return batch
}

// unitCost follows Nest COG priority so Go POS COGS matches Nest inventory/recipes:
// on-hand WAC → avg_unit_cost → latest production unit cost → recipe cost/yield.
func (s *ResolutionService) unitCost(ctx context.Context, shopID, itemID string) (float64, string) {
	const q = `
		SELECT COALESCE(p.available_stock, 0),
		       COALESCE(p.total_value::text, '0'),
		       COALESCE(p.avg_unit_cost::text, '0'),
		       COALESCE(i.name, $2)
		FROM "Item" i
		LEFT JOIN "InventoryProjection" p ON p.item_id = i.item_id AND p.shop_id = $1
		WHERE i.item_id = $2
		LIMIT 1
	`
	var avail float64
	var valStr, avgStr, name string
	err := s.pool.QueryRow(ctx, q, shopID, itemID).Scan(&avail, &valStr, &avgStr, &name)
	if err != nil {
		name = itemID
	}
	if name == "" {
		name = itemID
	}
	if m, e := money.New(avgStr); e == nil && m.IsPositive() {
		return m.Float64(), name
	}
	if avail > 0 {
		if m, e := money.New(valStr); e == nil && m.IsPositive() {
			return m.Float64() / avail, name
		}
	}
	// Latest production_finished unit cost (Nest or Go kitchen)
	var ucStr *string
	_ = s.pool.QueryRow(ctx, `
		SELECT unit_cost::text FROM "Event"
		WHERE shop_id = $1 AND item_id = $2 AND event_type = 'production_finished'
		  AND unit_cost IS NOT NULL
		ORDER BY created_at DESC LIMIT 1`, shopID, itemID).Scan(&ucStr)
	if ucStr != nil {
		if m, e := money.New(*ucStr); e == nil && m.IsPositive() {
			return m.Float64(), name
		}
	}
	// Recipe standard cost per yield (Nest Recipe tables)
	var recipeID string
	var yield float64
	_ = s.pool.QueryRow(ctx, `
		SELECT id, COALESCE(standard_yield, 0) FROM "Recipe"
		WHERE is_active AND (item_id = $1 OR recipe_code = $1)
		ORDER BY updated_at DESC NULLS LAST LIMIT 1`, itemID).Scan(&recipeID, &yield)
	if recipeID != "" {
		rows, err := s.pool.Query(ctx, `
			SELECT quantity_per_unit, COALESCE(unit_cost,0)::text, raw_item_id
			FROM "RecipeIngredient" WHERE recipe_id = $1`, recipeID)
		if err == nil {
			defer rows.Close()
			batch := 0.0
			for rows.Next() {
				var qty float64
				var ucS, rawID string
				if rows.Scan(&qty, &ucS, &rawID) != nil {
					continue
				}
				uc := 0.0
				if m, e := money.New(ucS); e == nil {
					uc = m.Float64()
				}
				if uc <= 0 && rawID != "" {
					var a string
					_ = s.pool.QueryRow(ctx, `
						SELECT COALESCE(avg_unit_cost,0)::text FROM "InventoryProjection"
						WHERE shop_id=$1 AND item_id=$2`, shopID, rawID).Scan(&a)
					if m, e := money.New(a); e == nil {
						uc = m.Float64()
					}
				}
				batch += qty * uc
			}
			if yield <= 0 {
				_ = s.pool.QueryRow(ctx, `
					SELECT COALESCE(SUM(standard_quantity),0) FROM "RecipeOutput"
					WHERE recipe_id=$1 AND COALESCE(is_active,true)`, recipeID).Scan(&yield)
			}
			if batch > 0 && yield > 0 {
				return batch / yield, name
			}
		}
	}
	return 0, name
}

// lookupMenuRecord returns name, price, and component lines from Nest menu events.
func (s *ResolutionService) lookupMenuRecord(ctx context.Context, shopID, menuItemID string) (name string, price float64, lines []map[string]interface{}) {
	rows, err := s.pool.Query(ctx, `
		SELECT COALESCE(payload::text, '{}')
		FROM "Event"
		WHERE shop_id = $1 AND event_type IN ('menu_created', 'menu_item_added', 'menu_item_created')
		ORDER BY created_at ASC
		LIMIT 3000
	`, shopID)
	if err != nil {
		return "", 0, nil
	}
	defer rows.Close()
	want := strings.ToUpper(strings.TrimSpace(menuItemID))
	var matchedLines []map[string]interface{}
	for rows.Next() {
		var payloadStr string
		if rows.Scan(&payloadStr) != nil {
			continue
		}
		var p map[string]interface{}
		if json.Unmarshal([]byte(payloadStr), &p) != nil || p == nil {
			continue
		}
		candidates := []string{
			strVal(p["menu_id"]), strVal(p["menuId"]), strVal(p["menu_item_id"]),
			strVal(p["menu_code"]), strVal(p["menuCode"]),
		}
		match := false
		for _, c := range candidates {
			if strings.ToUpper(strings.TrimSpace(c)) == want {
				match = true
				break
			}
		}
		if !match {
			continue
		}
		if n := strVal(p["name"]); n != "" {
			name = n
		}
		if n := strVal(p["menu_name"]); name == "" && n != "" {
			name = n
		}
		if pr := numVal(p["selling_price"]); pr > 0 {
			price = pr
		} else if pr := numVal(p["sellingPrice"]); pr > 0 {
			price = pr
		}
		if arr, ok := p["lines"].([]interface{}); ok {
			matchedLines = matchedLines[:0]
			for _, e := range arr {
				if lm, ok := e.(map[string]interface{}); ok {
					matchedLines = append(matchedLines, lm)
				}
			}
		}
	}
	return name, price, matchedLines
}

// deductionsFromMenuLines implements Nest FIXED + Inventory Select auto-deduct.
// FIXED finished_good_id → recipe or stock.
// CHOICE/MULTI_CHOICE with option_item_ids: auto-deduct when exactly one option
// (Michael: inventory select SKUs must leave stock on POS sale).
func (s *ResolutionService) deductionsFromMenuLines(
	ctx context.Context, shopID string, soldQty float64, lines []map[string]interface{},
) ([]StockDeduction, bool, error) {
	// Michael POS deduction chain (3 Oct 2026):
	// FIXED FG / inventory SKU → direct stock only (qty_required × soldQty).
	// Inventory Select (CHOICE) → direct on chosen/single option only.
	// NEVER expandRecipe → never block sale on raw ingredients of an FG.
	var out []StockDeduction
	anyRecipe := false // kept for API shape; POS path no longer marks recipe explosions

	for _, lm := range lines {
		if lm == nil {
			continue
		}
		ct := strings.ToUpper(strVal(lm["component_type"]))
		if ct == "" {
			ct = strings.ToUpper(strVal(lm["componentType"]))
		}
		if ct == "" {
			ct = "FIXED"
		}
		need := numVal(lm["quantity_required"])
		if need <= 0 {
			need = numVal(lm["quantityRequired"])
		}
		if need <= 0 {
			need = 1
		}
		qty := soldQty * need

		if ct == "FIXED" {
			ids := []string{}
			if fg := strVal(lm["finished_good_id"]); fg != "" {
				ids = append(ids, fg)
			}
			if fg := strVal(lm["finishedGoodId"]); fg != "" && len(ids) == 0 {
				ids = append(ids, fg)
			}
			// Inventory select stored as FIXED with option_item_ids
			ids = append(ids, stringSliceFromLine(lm)...)
			seen := map[string]bool{}
			for _, id := range ids {
				id = strings.TrimSpace(id)
				if id == "" || seen[id] {
					continue
				}
				seen[id] = true
				d, err := s.directStockDeduction(ctx, shopID, id, qty)
				if err != nil {
					return nil, false, err
				}
				out = append(out, d)
			}
			continue
		}

		if ct == "CHOICE" || ct == "MULTI_CHOICE" {
			optIDs := stringSliceFromLine(lm)
			if len(optIDs) == 0 {
				continue
			}
			// Single-option Inventory Select: auto-fill full Need
			id := optIDs[0]
			if len(optIDs) > 1 {
				// Prefer explicit selection fields if present on line payload
				if sel := strVal(lm["selected_item_id"]); sel != "" {
					id = sel
				} else if sel := strVal(lm["selectedItemId"]); sel != "" {
					id = sel
				}
			}
			d, err := s.directStockDeduction(ctx, shopID, id, qty)
			if err != nil {
				return nil, false, err
			}
			out = append(out, d)
		}
	}
	return out, anyRecipe, nil
}

func stringSliceFromLine(lm map[string]interface{}) []string {
	out := []string{}
	for _, key := range []string{"option_item_ids", "optionItemIds", "option_items", "optionItems"} {
		switch t := lm[key].(type) {
		case []interface{}:
			for _, e := range t {
				switch x := e.(type) {
				case string:
					if s := strings.TrimSpace(x); s != "" {
						out = append(out, s)
					}
				case map[string]interface{}:
					id := strVal(x["item_id"])
					if id == "" {
						id = strVal(x["itemId"])
					}
					if id == "" {
						id = strVal(x["finished_good_id"])
					}
					if id == "" {
						id = strVal(x["finishedGoodId"])
					}
					if id != "" {
						out = append(out, id)
					}
				}
			}
		case []string:
			out = append(out, t...)
		}
	}
	if opts, ok := lm["options"].([]interface{}); ok {
		for _, o := range opts {
			if om, ok := o.(map[string]interface{}); ok {
				id := strVal(om["finishedGoodId"])
				if id == "" {
					id = strVal(om["finished_good_id"])
				}
				if id != "" {
					out = append(out, id)
				}
			}
		}
	}
	return out
}

// AggregateDeductions merges same SKU lines.
func AggregateDeductions(rows []StockDeduction) []StockDeduction {
	m := map[string]*StockDeduction{}
	var order []string
	for _, r := range rows {
		if prev, ok := m[r.ItemID]; ok {
			prev.Quantity += r.Quantity
			prev.YieldQuantity += r.YieldQuantity
			prev.LineCost += r.LineCost
			if prev.UnitCost <= 0 && r.UnitCost > 0 {
				prev.UnitCost = r.UnitCost
			}
			if prev.Kind == "" && r.Kind != "" {
				prev.Kind = r.Kind
			}
			if prev.UnitWeight <= 0 && r.UnitWeight > 0 {
				prev.UnitWeight = r.UnitWeight
			}
			if prev.CostPerYield <= 0 && r.CostPerYield > 0 {
				prev.CostPerYield = r.CostPerYield
			}
		} else {
			cp := r
			if cp.LineCost <= 0 && cp.UnitCost > 0 {
				cp.LineCost = cp.Quantity * cp.UnitCost
			}
			m[r.ItemID] = &cp
			order = append(order, r.ItemID)
		}
	}
	out := make([]StockDeduction, 0, len(order))
	for _, id := range order {
		out = append(out, *m[id])
	}
	return out
}

// CheckAvailability ensures enough stock for deductions.
func (s *ResolutionService) CheckAvailability(ctx context.Context, shopID string, deductions []StockDeduction) error {
	for _, d := range deductions {
		const q = `
			SELECT COALESCE(available_stock, 0)
			FROM "InventoryProjection"
			WHERE shop_id = $1 AND item_id = $2
		`
		var avail float64
		err := s.pool.QueryRow(ctx, q, shopID, d.ItemID).Scan(&avail)
		if err != nil || avail < d.Quantity {
			return apperrors.BadRequest(fmt.Sprintf(
				"Insufficient stock for %s (%s): need %.4f, have %.4f",
				d.ItemName, d.ItemID, d.Quantity, avail,
			))
		}
	}
	return nil
}

// CheckAvailabilityTx verifies stock under a transaction with row locks (FOR UPDATE).
func (s *ResolutionService) CheckAvailabilityTx(ctx context.Context, tx pgx.Tx, shopID string, deductions []StockDeduction) error {
	for _, d := range deductions {
		var avail float64
		err := tx.QueryRow(ctx, `
			SELECT COALESCE(available_stock, 0)
			FROM "InventoryProjection"
			WHERE shop_id = $1 AND item_id = $2
			FOR UPDATE`, shopID, d.ItemID).Scan(&avail)
		if err != nil || avail < d.Quantity {
			return apperrors.BadRequest(fmt.Sprintf(
				"Insufficient stock for %s (%s): need %.4f, have %.4f",
				d.ItemName, d.ItemID, d.Quantity, avail,
			))
		}
	}
	return nil
}
