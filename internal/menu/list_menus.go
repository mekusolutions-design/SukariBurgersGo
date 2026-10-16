package menu

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ListMenus folds menu_created / menu_item_added events (Nest parity) and merges CatalogMenuItem.
// Critical for POS: must surface the same menus Nest shows on the shared DB.
func (s *ResolutionService) ListMenus(ctx context.Context, shopID string) ([]map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}

	type menuRow struct {
		menuID, name, code, category string
		price                        float64
		taxRate                      float64
		available                    bool
		createdAt                    string
		visible                      bool
		status                       string
		lines                        []map[string]interface{}
	}
	byID := map[string]*menuRow{}
	order := []string{}

	upsert := func(menuID string) *menuRow {
		m, ok := byID[menuID]
		if !ok {
			m = &menuRow{
				menuID: menuID, lines: []map[string]interface{}{},
				visible: true, available: true, taxRate: 16, status: "active",
			}
			byID[menuID] = m
			order = append(order, menuID)
		}
		return m
	}

	applyPayload := func(et string, p map[string]interface{}, created time.Time) {
		if p == nil || !menuEventMatchesShop(p, shopID) {
			return
		}
		et = strings.ToLower(strings.TrimSpace(et))

		menuID := firstStr(p, "menu_id", "menuId", "menu_item_id", "menuItemId")
		if menuID == "" {
			if code := firstStr(p, "menu_code", "menuCode"); code != "" {
				menuID = "MENU-" + code
			}
		}
		if menuID == "" {
			menuID = firstStr(p, "id")
		}
		if menuID == "" {
			return
		}

		// NestJS ignores adjustments/additions for menus not yet created.
		_, exists := byID[menuID]
		if !exists && (et == "menu_adjusted" || et == "menu_updated") {
			return
		}
		m := upsert(menuID)

		isCreate := et == "menu_created" || et == "menu_item_created"
		if isCreate {
			if v := firstStr(p, "name", "menu_name", "menuName"); v != "" {
				m.name = v
			}
			if v := firstStr(p, "menu_code", "menuCode"); v != "" {
				m.code = v
			}
			if v, ok := p["category"]; ok {
				if v == nil {
					m.category = ""
				} else {
					m.category = strings.TrimSpace(fmt.Sprint(v))
				}
			}
			if v, ok := p["selling_price"]; ok {
				m.price = floatAny(v)
			} else if v, ok := p["sellingPrice"]; ok {
				m.price = floatAny(v)
			} else if v, ok := p["price"]; ok {
				m.price = floatAny(v)
			}
			if v, ok := p["is_visible"].(bool); ok {
				m.visible = v
			}
			if v, ok := p["isVisible"].(bool); ok {
				m.visible = v
			}
			if v, ok := p["is_available"].(bool); ok {
				m.available = v
			}
			if v, ok := p["isAvailable"].(bool); ok {
				m.available = v
			}
			if v, ok := p["tax_rate"]; ok {
				m.taxRate = floatAny(v)
			} else if v, ok := p["taxRate"]; ok {
				m.taxRate = floatAny(v)
			}
			if v := firstStr(p, "status"); v != "" {
				m.status = v
			}
			m.createdAt = created.UTC().Format(time.RFC3339)
		}

		if et == "menu_adjusted" || et == "menu_updated" {
			if v, ok := p["name"]; ok && v != nil {
				m.name = strings.TrimSpace(fmt.Sprint(v))
			}
			if v, ok := p["selling_price"]; ok && v != nil {
				m.price = floatAny(v)
			} else if v, ok := p["sellingPrice"]; ok && v != nil {
				m.price = floatAny(v)
			}
			if v, ok := p["category"]; ok {
				if v == nil {
					m.category = ""
				} else {
					m.category = strings.TrimSpace(fmt.Sprint(v))
				}
			}
			if v, ok := p["is_available"].(bool); ok {
				m.available = v
			} else if v, ok := p["isAvailable"].(bool); ok {
				m.available = v
			}
			if v, ok := p["is_visible"].(bool); ok {
				m.visible = v
			} else if v, ok := p["isVisible"].(bool); ok {
				m.visible = v
			}
			if v, ok := p["tax_rate"]; ok && v != nil {
				m.taxRate = floatAny(v)
			} else if v, ok := p["taxRate"]; ok && v != nil {
				m.taxRate = floatAny(v)
			}
			if v := firstStr(p, "menu_code", "menuCode"); v != "" {
				m.code = v
			}

		}

		if et == "menu_item_added" && m.name == "" {
			m.name = firstStr(p, "name", "menu_name", "menuName")
		}

		lineKey := func(line map[string]interface{}, idx int) string {
			key := firstStr(line, "component_key", "componentKey")
			if key == "" {
				key = firstStr(line, "finished_good_id", "finishedGoodId")
			}
			if key == "" {
				key = fmt.Sprintf("line_%d", idx+1)
			}
			return key
		}
		mergeLine := func(line map[string]interface{}, idx int) {
			key := lineKey(line, idx)
			for i, old := range m.lines {
				if lineKey(old, i) == key {
					m.lines[i] = line
					return
				}
			}
			m.lines = append(m.lines, line)
		}
		replaceLines := func(raw []interface{}) {
			m.lines = []map[string]interface{}{}
			for i, value := range raw {
				line, ok := value.(map[string]interface{})
				if ok && line != nil {
					mergeLine(line, i)
				}
			}
		}

		if isCreate {
			m.lines = []map[string]interface{}{}
			if raw, ok := p["lines"].([]interface{}); ok && len(raw) > 0 {
				replaceLines(raw)
			} else if fg := firstStr(p, "finished_good_id", "finishedGoodId"); fg != "" {
				mergeLine(map[string]interface{}{
					"component_key": "main", "componentKey": "main",
					"component_type": "FIXED", "componentType": "FIXED",
					"finished_good_id": fg, "finishedGoodId": fg,
					"quantity_required": float64(1), "quantityRequired": float64(1),
					"unit": firstNonEmpty(firstStr(p, "unit"), "pcs"),
				}, 0)
			}
			if raw, ok := p["components"].([]interface{}); ok {
				for i, value := range raw {
					if line, ok := value.(map[string]interface{}); ok && line != nil {
						mergeLine(line, i)
					}
				}
			}
		} else if et == "menu_adjusted" || et == "menu_updated" {
			// Empty or omitted adjustment lines preserve the existing snapshot.
			if raw, supplied := p["lines"]; supplied {
				if arr, ok := raw.([]interface{}); ok && len(arr) > 0 {
					replaceLines(arr)
				}
			}
		} else {
			if raw, ok := p["lines"].([]interface{}); ok {
				for i, value := range raw {
					if line, ok := value.(map[string]interface{}); ok && line != nil {
						mergeLine(line, i)
					}
				}
			}
			if raw, ok := p["components"].([]interface{}); ok {
				for i, value := range raw {
					if line, ok := value.(map[string]interface{}); ok && line != nil {
						mergeLine(line, i)
					}
				}
			}
			if et == "menu_item_added" &&
				(firstStr(p, "component_key", "componentKey", "finished_good_id", "finishedGoodId") != "" ||
					len(stringSliceAny(p["option_item_ids"])) > 0) {
				mergeLine(p, len(m.lines))
			}
		}
	}
	// Query 1: shop-scoped events (Nest default)
	loadEvents := func(shopFilter bool) {
		var rows interface {
			Next() bool
			Scan(dest ...any) error
			Close()
			Err() error
		}
		var err error
		if shopFilter {
			r, e := s.pool.Query(ctx, `
				SELECT event_type, payload, created_at FROM "Event"
				WHERE shop_id = $1
				  AND event_type IN (
				    'menu_created','menu_item_added','menu_item_created','menu_adjusted','menu_updated','MENU_CREATED',
				    'menu_updated','MENU_ITEM_ADDED'
				  )
				ORDER BY created_at ASC
				LIMIT 5000`, shopID)
			err = e
			rows = r
		} else {
			r, e := s.pool.Query(ctx, `
				SELECT event_type, payload, created_at FROM "Event"
				WHERE event_type IN (
				    'menu_created','menu_item_added','menu_item_created','menu_adjusted','menu_updated','MENU_CREATED',
				    'menu_updated','MENU_ITEM_ADDED'
				  )
				ORDER BY created_at ASC
				LIMIT 5000`)
			err = e
			rows = r
		}
		if err != nil || rows == nil {
			return
		}
		defer rows.Close()
		for rows.Next() {
			var et string
			var raw interface{}
			var created time.Time
			if rows.Scan(&et, &raw, &created) != nil {
				continue
			}
			var bytes []byte
			switch v := raw.(type) {
			case []byte:
				bytes = v
			case string:
				bytes = []byte(v)
			default:
				// pgx may deliver map already
				if m, ok := raw.(map[string]interface{}); ok {
					applyPayload(et, m, created)
					continue
				}
				b, _ := json.Marshal(raw)
				bytes = b
			}
			p := decodePayload(bytes)
			applyPayload(et, p, created)
		}
	}

	// Never fall back to an unscoped Event query: a shop with no menu events
	// must not inherit another shop's menu. CatalogMenuItem remains shop-scoped.
	loadEvents(true)

	// CatalogMenuItem merge
	catalog, _ := s.ListCatalog(ctx, shopID)
	recipeCache := map[string][]StockDeduction{}
	for _, c := range catalog {
		id := strings.TrimSpace(c.MenuItemID)
		if id == "" {
			continue
		}

		m, ok := byID[id]
		if !ok {
			m = upsert(id)
			m.name = c.Name
			m.price = c.SellingPrice
			m.code = id
		} else {
			if m.name == "" {
				m.name = c.Name
			}
			if m.price <= 0 {
				m.price = c.SellingPrice
			}
		}

		// Do not override event-defined components or choice groups.
		// Only materialize an explicit catalog link when no menu lines exist.
		if len(m.lines) == 0 {
			stockItemID := ""
			if c.StockItemID != nil {
				stockItemID = strings.TrimSpace(*c.StockItemID)
			}

			if stockItemID != "" {
				m.lines = append(m.lines, map[string]interface{}{
					"componentType":     "FIXED",
					"component_type":    "FIXED",
					"finishedGoodId":    stockItemID,
					"finished_good_id":  stockItemID,
					"quantityRequired":  float64(1),
					"quantity_required": float64(1),
					"unit":              "pcs",
				})
			} else if c.RecipeID != nil && strings.TrimSpace(*c.RecipeID) != "" {
				// Expand only the explicitly linked recipe. Never guess by name.
				m.lines = s.enrichExplicitRecipe(
					ctx, shopID, *c.RecipeID, m.lines, recipeCache,
				)
			}
		}
	}

	// stock map
	stock := map[string]float64{}
	stockUnits := map[string]string{}
	srows, _ := s.pool.Query(ctx, `SELECT p.item_id, COALESCE(p.available_stock,0), COALESCE(i.unit,'') FROM "InventoryProjection" p LEFT JOIN "Item" i ON i.item_id = p.item_id WHERE p.shop_id=$1`, shopID)
	if srows != nil {
		for srows.Next() {
			var id, unit string
			var a float64
			if srows.Scan(&id, &a, &unit) == nil {
				stock[id] = a
				stockUnits[id] = unit
			}
		}
		srows.Close()
	}

	out := make([]map[string]interface{}, 0, len(order))
	for _, id := range order {
		m := byID[id]
		if m == nil {
			continue
		}
		if !m.visible || strings.EqualFold(m.status, "deleted") || strings.EqualFold(m.status, "inactive") {
			continue
		}
		if m.name == "" {
			m.name = m.menuID
		}
		if m.code == "" {
			m.code = m.menuID
		}
		normLines, reqSel := normalizeMenuLines(m.lines, stock, stockUnits)
		maxP, fgID := portionsFromLines(normLines, stock, stockUnits)
		// Fast path only Ã¢â‚¬â€ no per-menu ResolveMenuItemLine (that made POS load 30s+)
		// Menus with no linked lines/stock still appear; availability shows 0 / 86'd
		if maxP < 0 {
			maxP = 0
		}
		item := map[string]interface{}{
			"menuId": m.menuID, "menu_id": m.menuID, "id": m.menuID,
			"menuItemId": m.menuID, "menu_item_id": m.menuID,
			"menuCode": m.code, "menu_code": m.code,
			"name": m.name, "category": nilIfEmpty(m.category),
			"sellingPrice": m.price, "selling_price": m.price,
			"taxRate": m.taxRate, "tax_rate": m.taxRate,
			"lines":       normLines,
			"isAvailable": m.available && maxP > 0, "is_available": m.available && maxP > 0,
			"isVisible": m.visible, "is_visible": m.visible, "status": m.status,
			"maxPortions": maxP, "availableQuantity": maxP,
			"finishedGoodId": fgID, "finished_good_id": fgID,
			"unitCost": 0, "foodCost": 0, "margin": m.price,
			"requiresSelection": reqSel, "createdAt": m.createdAt,
			"shopId": shopID, "shop_id": shopID,
		}
		out = append(out, item)
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}

// menuEventMatchesShop rejects explicit cross-shop payloads. Missing payload
// scope is accepted only because callers obtain these rows from a shop-scoped
// Event query; this helper must not be used with unscoped event results.
func menuEventMatchesShop(payload map[string]interface{}, shopID string) bool {
	if payload == nil || strings.TrimSpace(shopID) == "" {
		return false
	}
	eventShop := strAny(payload["shop_id"])
	if eventShop == "" {
		eventShop = strAny(payload["shopId"])
	}
	return eventShop == "" || eventShop == shopID
}

func decodePayload(raw []byte) map[string]interface{} {
	if len(raw) == 0 {
		return nil
	}
	var p map[string]interface{}
	if err := json.Unmarshal(raw, &p); err == nil && p != nil {
		return p
	}
	// payload sometimes double-encoded string
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		_ = json.Unmarshal([]byte(s), &p)
		return p
	}
	return nil
}

func firstStr(p map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if s := strAny(p[k]); s != "" {
			return s
		}
	}
	return ""
}

func strAny(v interface{}) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case fmt.Stringer:
		return strings.TrimSpace(t.String())
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case json.Number:
		return t.String()
	default:
		return ""
	}
}

func floatAny(v interface{}) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f
	default:
		return 0
	}
}

// normalizeMenuLines ensures web POS never sees undefined options/lines.
func normalizeMenuLines(lines []map[string]interface{}, stock map[string]float64, stockUnits ...map[string]string) ([]map[string]interface{}, bool) {
	if lines == nil {
		return []map[string]interface{}{}, false
	}
	out := make([]map[string]interface{}, 0, len(lines))
	requiresSelection := false
	var unitsByID map[string]string
	if len(stockUnits) > 0 {
		unitsByID = stockUnits[0]
	}
	for _, lm := range lines {
		if lm == nil {
			continue
		}
		n := map[string]interface{}{}
		for k, v := range lm {
			n[k] = v
		}
		ct := strings.ToUpper(firstStr(n, "component_type", "componentType"))
		if ct == "" {
			ct = "FIXED"
		}
		n["componentType"] = ct
		n["component_type"] = ct

		fg := firstStr(n, "finished_good_id", "finishedGoodId")
		n["finishedGoodId"] = nilIfEmpty(fg)
		n["finished_good_id"] = nilIfEmpty(fg)

		need := floatAny(n["quantity_required"])
		if need <= 0 {
			need = floatAny(n["quantityRequired"])
		}
		if need <= 0 {
			need = 1
		}
		n["quantityRequired"] = need
		n["quantity_required"] = need

		optIDs := stringSliceAny(n["option_item_ids"])
		if len(optIDs) == 0 {
			optIDs = stringSliceAny(n["optionItemIds"])
		}
		if len(optIDs) == 0 {
			optIDs = stringSliceAny(n["option_items"])
			if len(optIDs) == 0 {
				optIDs = stringSliceAny(n["optionItems"])
			}
		}

		opts := []map[string]interface{}{}
		if rawOpts, ok := n["options"].([]interface{}); ok {
			for _, o := range rawOpts {
				om, ok := o.(map[string]interface{})
				if !ok {
					continue
				}
				oid := firstStr(om, "finishedGoodId", "finished_good_id", "item_id", "itemId")
				if oid == "" {
					continue
				}
				st := stockGet(stock, oid)
				optionUnit := stockUnitFor(unitsByID, oid, firstNonEmpty(firstStr(om, "unit"), "pcs"))
				needUnit := firstNonEmpty(firstStr(n, "unit"), "pcs")
				optionPortions := portionOne(st, need, optionUnit, needUnit)
				opts = append(opts, map[string]interface{}{
					"finishedGoodId": oid, "finished_good_id": oid,
					"finishedGoodName": firstNonEmpty(firstStr(om, "finishedGoodName", "name"), oid),
					"unit":             optionUnit,
					"availableStock":   st, "available_stock": st,
					"unitCost": floatAny(om["unitCost"]), "maxPortions": optionPortions,
				})
			}
		}
		if len(opts) == 0 {
			for _, oid := range optIDs {
				if oid == "" {
					continue
				}
				st := stockGet(stock, oid)
				optionUnit := stockUnitFor(unitsByID, oid, "pcs")
				needUnit := firstNonEmpty(firstStr(n, "unit"), "pcs")
				optionPortions := portionOne(st, need, optionUnit, needUnit)
				opts = append(opts, map[string]interface{}{
					"finishedGoodId": oid, "finished_good_id": oid,
					"finishedGoodName": oid, "unit": optionUnit,
					"availableStock": st, "available_stock": st,
					"unitCost": 0, "maxPortions": optionPortions,
				})
			}
		}
		n["options"] = opts
		n["option_item_ids"] = optIDs
		n["optionItemIds"] = optIDs

		if ct == "CHOICE" || ct == "MULTI_CHOICE" {
			requiresSelection = true
		}
		if (ct == "CHOICE" || ct == "MULTI_CHOICE") && len(opts) == 1 {
			n["autoDeductItemId"] = opts[0]["finishedGoodId"]
		}
		out = append(out, n)
	}
	return out, requiresSelection
}

func portionsFromLines(
	lines []map[string]interface{},
	stock map[string]float64,
	stockUnits ...map[string]string,
) (int, string) {
	var unitsByID map[string]string
	if len(stockUnits) > 0 {
		unitsByID = stockUnits[0]
	}
	return computePortions(lines, stock, unitsByID)
}

func stringSliceAny(v interface{}) []string {
	out := []string{}
	switch t := v.(type) {
	case []string:
		return t
	case []interface{}:
		for _, e := range t {
			switch x := e.(type) {
			case string:
				if s := strings.TrimSpace(x); s != "" {
					out = append(out, s)
				}
			case map[string]interface{}:
				id := firstStr(x, "item_id", "itemId", "finished_good_id", "finishedGoodId")
				if id != "" {
					out = append(out, id)
				}
			}
		}
	}
	return out
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
