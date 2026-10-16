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
		createdAt                    string
		visible                      bool
		status                       string
		lines                        []map[string]interface{}
		finishedGoodID               string
		quantityRequired             float64
		unit                         string
		recipeID                     string
	}
	byID := map[string]*menuRow{}
	order := []string{}

	upsert := func(menuID string) *menuRow {
		m, ok := byID[menuID]
		if !ok {
			m = &menuRow{
				menuID: menuID, lines: []map[string]interface{}{},
				visible: true, status: "active",
			}
			byID[menuID] = m
			order = append(order, menuID)
		}
		return m
	}

	applyPayload := func(et string, p map[string]interface{}, created time.Time) {
		if p == nil {
			return
		}
		// shop filter (payload or already scoped by query)
		eventShop := strAny(p["shop_id"])
		if eventShop == "" {
			eventShop = strAny(p["shopId"])
		}
		if eventShop != "" && eventShop != shopID && !(shopID == "1" && eventShop == "") {
			// allow missing shop in payload; only skip explicit mismatch
			if eventShop != shopID {
				return
			}
		}

		menuID := firstStr(p, "menu_id", "menuId", "menu_item_id", "menuItemId")
		if menuID == "" {
			// Nest create: MENU-{code}
			code := firstStr(p, "menu_code", "menuCode")
			if code != "" {
				menuID = "MENU-" + code
			}
		}
		if menuID == "" {
			menuID = firstStr(p, "id")
		}
		if menuID == "" {
			return
		}

		m := upsert(menuID)

		// Nest: root finished_good_id / inventory_item_id on menu or primary FIXED line
		if fg := firstStr(p, "finished_good_id", "finishedGoodId", "inventory_item_id", "inventoryItemId", "item_id", "itemId"); fg != "" {
			// Prefer non-MENU ids as stock SKU (MENU-* is commercial id, not inventory)
			if !strings.HasPrefix(strings.ToUpper(fg), "MENU-") {
				m.finishedGoodID = fg
			} else if m.finishedGoodID == "" {
				m.finishedGoodID = fg
			}
		}
		if q := floatAny(p["quantity_required"]); q > 0 {
			m.quantityRequired = q
		} else if q := floatAny(p["quantityRequired"]); q > 0 {
			m.quantityRequired = q
		}
		if u := firstStr(p, "unit"); u != "" {
			m.unit = u
		}

		if et == "menu_created" || et == "MENU_CREATED" || et == "menu_item_created" {
			if n := firstStr(p, "name", "menu_name", "menuName"); n != "" {
				m.name = n
			}
			if c := firstStr(p, "menu_code", "menuCode"); c != "" {
				m.code = c
			}
			if c := firstStr(p, "category"); c != "" {
				m.category = c
			}
			if pr := floatAny(p["selling_price"]); pr > 0 {
				m.price = pr
			} else if pr := floatAny(p["sellingPrice"]); pr > 0 {
				m.price = pr
			} else if pr := floatAny(p["price"]); pr > 0 {
				m.price = pr
			}
			if v, ok := p["is_visible"].(bool); ok {
				m.visible = v
			}
			if v, ok := p["isVisible"].(bool); ok {
				m.visible = v
			}
			if st := firstStr(p, "status"); st != "" {
				m.status = st
			}
			m.createdAt = created.UTC().Format(time.RFC3339)
		}

		// menu_item_added may only carry lines / component — ensure row exists
		if et == "menu_item_added" {
			if m.name == "" {
				if n := firstStr(p, "name", "menu_name"); n != "" {
					m.name = n
				}
			}
		}

		// Replace lines on create/adjust; append single component on item_added
		appendLines := func(arr []interface{}) {
			for _, e := range arr {
				lm, _ := e.(map[string]interface{})
				if lm == nil {
					continue
				}
				m.lines = append(m.lines, lm)
			}
		}
		etU := strings.ToUpper(et)
		replaceLines := etU == "MENU_CREATED" || etU == "MENU_ITEM_CREATED" ||
			etU == "MENU_UPDATED" || etU == "MENU_ADJUSTED" ||
			et == "menu_created" || et == "menu_item_created" ||
			et == "menu_updated" || et == "menu_adjusted"
		if arr, ok := p["lines"].([]interface{}); ok && len(arr) > 0 {
			if replaceLines {
				m.lines = nil
			}
			appendLines(arr)
		}
		if arr, ok := p["components"].([]interface{}); ok && len(arr) > 0 {
			if replaceLines && len(m.lines) == 0 {
				// components-only payload on adjust
			}
			if replaceLines && p["lines"] == nil {
				m.lines = nil
			}
			appendLines(arr)
		}
		// Single component on menu_item_added
		if et == "menu_item_added" || et == "MENU_ITEM_ADDED" {
			if firstStr(p, "component_key", "componentKey", "finished_good_id", "finishedGoodId") != "" ||
				len(stringSliceAny(p["option_item_ids"])) > 0 {
				m.lines = append(m.lines, p)
			}
		}
	}

	// ── Query 1: shop-scoped events (Nest default) ─────────
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
				    'menu_created','menu_item_added','menu_item_created','MENU_CREATED',
				    'menu_updated','MENU_ITEM_ADDED','menu_adjusted','MENU_ADJUSTED'
				  )
				ORDER BY created_at ASC
				LIMIT 5000`, shopID)
			err = e
			rows = r
		} else {
			r, e := s.pool.Query(ctx, `
				SELECT event_type, payload, created_at FROM "Event"
				WHERE event_type IN (
				    'menu_created','menu_item_added','menu_item_created','MENU_CREATED',
				    'menu_updated','MENU_ITEM_ADDED','menu_adjusted','MENU_ADJUSTED'
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

	loadEvents(true)
	if len(order) == 0 {
		// Fallback: Nest sometimes wrote events under another shop_id / default
		loadEvents(false)
	}

	// ── CatalogMenuItem merge ─────────────────────────────
	catalog, _ := s.ListCatalog(ctx, shopID)
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
		// Catalog stocked SKU → POS FIXED availability (sodas, direct inventory)
		if c.StockItemID != nil {
			sid := strings.TrimSpace(*c.StockItemID)
			if sid != "" && m.finishedGoodID == "" {
				m.finishedGoodID = sid
			}
		}
		if c.RecipeID != nil {
			rid := strings.TrimSpace(*c.RecipeID)
			if rid != "" {
				m.recipeID = rid
			}
		}
	}

	// stock map
	stock := map[string]float64{}
	srows, _ := s.pool.Query(ctx, `SELECT item_id, COALESCE(available_stock,0) FROM "InventoryProjection" WHERE shop_id=$1`, shopID)
	if srows != nil {
		for srows.Next() {
			var id string
			var a float64
			if srows.Scan(&id, &a) == nil {
				stock[id] = a
				// Mirror keys so menu item_ids with different casing still resolve
				if u := strings.ToUpper(id); u != id {
					if _, exists := stock[u]; !exists {
						stock[u] = a
					}
				}
			}
		}
		srows.Close()
	}

	// Fallback: sum *usable* open batches (not past expiry) when projection is low/missing
	brows, berr := s.pool.Query(ctx, `
		SELECT item_id, COALESCE(SUM(qty_remaining), 0)
		FROM "InventoryBatch"
		WHERE shop_id = $1 AND COALESCE(qty_remaining, 0) > 0
		  AND (expiry_date IS NULL OR expiry_date >= (CURRENT_TIMESTAMP AT TIME ZONE 'Africa/Nairobi')::date)
		GROUP BY item_id`, shopID)
	if berr == nil && brows != nil {
		for brows.Next() {
			var id string
			var qty float64
			if brows.Scan(&id, &qty) == nil && id != "" {
				cur := stockLookup(stock, id)
				if qty > cur {
					stock[id] = qty
					if u := strings.ToUpper(id); u != id {
						if _, exists := stock[u]; !exists {
							stock[u] = qty
						}
					}
				}
			}
		}
		brows.Close()
	}

	// Name → stock index (menu "300 ml SODA" vs inventory "300ml soda")
	stockByName := map[string]float64{}
	nrows, nerr := s.pool.Query(ctx, `
		SELECT i.item_id, i.name, COALESCE(p.available_stock, 0)
		FROM "Item" i
		LEFT JOIN "InventoryProjection" p
		  ON p.item_id = i.item_id AND p.shop_id = $1
	`, shopID)
	if nerr == nil && nrows != nil {
		for nrows.Next() {
			var id, name string
			var avail float64
			if nrows.Scan(&id, &name, &avail) != nil {
				continue
			}
			if avail <= 0 {
				avail = stockLookup(stock, id)
			}
			if avail <= 0 {
				continue
			}
			stock[id] = maxFloat(stockLookup(stock, id), avail)
			for _, key := range stockNameKeys(name, id) {
				if prev, ok := stockByName[key]; !ok || avail > prev {
					stockByName[key] = avail
				}
			}
		}
		nrows.Close()
	}

	recipeCache := map[string][]StockDeduction{}
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
		linesIn := m.lines
		// Nest: if no component lines but menu has finishedGoodId, synthesize FIXED line
		if len(linesIn) == 0 && m.finishedGoodID != "" {
			need := m.quantityRequired
			if need <= 0 {
				need = 1
			}
			unit := m.unit
			if unit == "" {
				unit = "pcs"
			}
			linesIn = []map[string]interface{}{
				{
					"componentType": "FIXED", "component_type": "FIXED",
					"componentKey": "main", "component_key": "main",
					"finishedGoodId": m.finishedGoodID, "finished_good_id": m.finishedGoodID,
					"quantityRequired": need, "quantity_required": need,
					"unit": unit,
				},
			}
		}
		// If lines exist but none carry a stock SKU, attach root FG as FIXED
		if m.finishedGoodID != "" {
			hasSKU := false
			for _, lm := range linesIn {
				if firstStr(lm, "finishedGoodId", "finished_good_id", "inventory_item_id") != "" {
					hasSKU = true
					break
				}
				if len(stringSliceAny(lm["option_item_ids"])) > 0 || len(stringSliceAny(lm["optionItemIds"])) > 0 {
					hasSKU = true
					break
				}
			}
			if !hasSKU {
				need := m.quantityRequired
				if need <= 0 {
					need = 1
				}
				linesIn = append(linesIn, map[string]interface{}{
					"componentType": "FIXED", "component_type": "FIXED",
					"componentKey": "main_fg", "component_key": "main_fg",
					"finishedGoodId": m.finishedGoodID, "finished_good_id": m.finishedGoodID,
					"quantityRequired": need, "quantity_required": need,
					"unit": firstNonEmpty(m.unit, "pcs"),
				})
			}
		}
		linesIn = dedupeComponentLines(linesIn)
		// Nest/ops: also constrain by recipe ingredients (bacon g, cheddar g, …)
		linesIn = s.enrichLinesWithRecipeConstraints(ctx, shopID, linesIn, m.menuID, m.code, m.name, m.finishedGoodID, m.recipeID, recipeCache)
		normLines, reqSel := normalizeMenuLines(linesIn, stock)
		maxP, fgID := portionsFromLines(normLines, stock)
		if fgID == "" && m.finishedGoodID != "" {
			fgID = m.finishedGoodID
		}
		// Fast path only — no per-menu ResolveMenuItemLine (that made POS load 30s+)
		// Menus with no linked lines/stock still appear; availability shows 0 / 86'd
		if maxP < 0 {
			maxP = 0
		}
		// Name-based stock ONLY when there are no component SKU constraints
		// (sodas/juices). Never override multi-line recipe/burger min-portions.
		if maxP == 0 && !hasStockConstraints(normLines) {
			need := m.quantityRequired
			if need <= 0 {
				need = 1
			}
			if av := stockByNameLookup(stockByName, m.name, m.code, m.menuID); av > 0 {
				maxP = int(av / need)
				if maxP < 0 {
					maxP = 0
				}
			}
		}
		// Sellable without stock linkage (price-only): show as available for order UI
		if maxP == 0 && len(normLines) == 0 && m.price > 0 {
			maxP = 999
		}
		item := map[string]interface{}{
			"menuId": m.menuID, "menu_id": m.menuID, "id": m.menuID,
			"menuItemId": m.menuID, "menu_item_id": m.menuID,
			"menuCode": m.code, "menu_code": m.code,
			"name": m.name, "category": nilIfEmpty(m.category),
			"sellingPrice": m.price, "selling_price": m.price,
			"taxRate": 16.0, "tax_rate": 16.0,
			"lines":       normLines,
			"isAvailable": maxP > 0, "is_available": maxP > 0,
			"isVisible": true, "status": m.status,
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
func normalizeMenuLines(lines []map[string]interface{}, stock map[string]float64) ([]map[string]interface{}, bool) {
	if lines == nil {
		return []map[string]interface{}{}, false
	}
	out := make([]map[string]interface{}, 0, len(lines))
	requiresSelection := false
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
				st := stockLookup(stock, oid)
				opts = append(opts, map[string]interface{}{
					"finishedGoodId": oid, "finished_good_id": oid,
					"finishedGoodName": firstNonEmpty(firstStr(om, "finishedGoodName", "name"), oid),
					"unit":             firstNonEmpty(firstStr(om, "unit"), "pcs"),
					"availableStock":   st, "available_stock": st,
					"unitCost": floatAny(om["unitCost"]), "maxPortions": int(st / need),
				})
			}
		}
		if len(opts) == 0 {
			for _, oid := range optIDs {
				if oid == "" {
					continue
				}
				st := stockLookup(stock, oid)
				opts = append(opts, map[string]interface{}{
					"finishedGoodId": oid, "finished_good_id": oid,
					"finishedGoodName": oid, "unit": "pcs",
					"availableStock": st, "available_stock": st,
					"unitCost": 0, "maxPortions": int(st / need),
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

func portionsFromLines(lines []map[string]interface{}, stock map[string]float64) (int, string) {
	r := ComputeMenuAvailability(lines, stock)
	return r.MaxPortions, r.FirstFG
}


func stockLookup(stock map[string]float64, itemID string) float64 {
	if itemID == "" {
		return 0
	}
	if v, ok := stock[itemID]; ok {
		return v
	}
	// Case variants (menu events vs projection keys)
	up := strings.ToUpper(itemID)
	if v, ok := stock[up]; ok {
		return v
	}
	low := strings.ToLower(itemID)
	if v, ok := stock[low]; ok {
		return v
	}
	for k, v := range stock {
		if strings.EqualFold(k, itemID) {
			return v
		}
	}
	return 0
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


func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func normalizeStockKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func stockNameKeys(name, itemID string) []string {
	keys := []string{}
	seen := map[string]bool{}
	add := func(k string) {
		k = normalizeStockKey(k)
		if k == "" || seen[k] {
			return
		}
		seen[k] = true
		keys = append(keys, k)
	}
	add(name)
	add(itemID)
	// "300 ml SODA" / "300ml soda" / "SODA 300ML"
	n := strings.ToLower(name + " " + itemID)
	add(strings.ReplaceAll(n, " ", ""))
	return keys
}

func stockByNameLookup(byName map[string]float64, parts ...string) float64 {
	best := 0.0
	for _, p := range parts {
		for _, k := range stockNameKeys(p, "") {
			if v, ok := byName[k]; ok && v > best {
				best = v
			}
		}
	}
	// Partial contains: menu "PASSION JUICE" vs item "Passion juice"
	if best <= 0 {
		joined := normalizeStockKey(strings.Join(parts, " "))
		if len(joined) >= 4 {
			for k, v := range byName {
				if strings.Contains(k, joined) || strings.Contains(joined, k) {
					if v > best {
						best = v
					}
				}
			}
		}
	}
	return best
}
