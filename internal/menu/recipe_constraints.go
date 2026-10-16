package menu

import (
	"context"
	"strings"
)

// enrichLinesWithRecipeConstraints adds FIXED constraints from RecipeIngredient so
// maxPortions = min over the real sellable BOM (same idea as the Menu page).
//
// Rules (Michael / ops):
//   - POS must show the same sellable quantity as Menu — no separate POS math.
//   - FIXED / stocked items (sodas, direct inventory): use the linked inventory
//     quantity only. Do NOT attach a random recipe via display-name match.
//   - Recipe / burger menus: expand the linked recipe (or root FG → recipe) and
//     take min over every ingredient (bacon g, cheddar g, lettuce, …).
func (s *ResolutionService) enrichLinesWithRecipeConstraints(
	ctx context.Context,
	shopID string,
	lines []map[string]interface{},
	menuID, code, name, rootFG, recipeID string,
	cache map[string][]StockDeduction,
) []map[string]interface{} {
	if s == nil || s.pool == nil {
		return lines
	}
	out := make([]map[string]interface{}, len(lines))
	copy(out, lines)

	keys := make([]string, 0, 12)
	seenKey := map[string]bool{}
	addKey := func(k string) {
		k = strings.TrimSpace(k)
		if k == "" || seenKey[strings.ToUpper(k)] {
			return
		}
		seenKey[strings.ToUpper(k)] = true
		keys = append(keys, k)
	}

	explicitRecipe := strings.TrimSpace(recipeID) != ""

	// Always prefer explicit recipe id / commercial codes / root FG.
	addKey(recipeID)
	addKey(rootFG)
	addKey(menuID)
	addKey(code)
	if strings.HasPrefix(strings.ToUpper(menuID), "MENU-") && len(menuID) > 5 {
		addKey(menuID[5:])
	}
	if strings.HasPrefix(strings.ToUpper(code), "MENU-") && len(code) > 5 {
		addKey(code[5:])
	}

	// Name + line FGs: safe because we only *apply* expansions with 2+ ingredients
	// when recipeID is empty (standalone FIXED "Cheese" matching a 1-line recipe is skipped).
	addKey(name)
	for _, lm := range lines {
		addKey(firstStr(lm, "finishedGoodId", "finished_good_id", "inventory_item_id", "inventoryItemId"))
	}
	_ = explicitRecipe // used below when filtering BOM size

	added := map[string]bool{}
	for _, key := range keys {
		ded := s.cachedExpandRecipe(ctx, shopID, key, cache)
		// Without an explicit recipe link, ignore 0–1 ingredient expansions
		// (stocked FG or false-positive name/code match). Real burgers have a BOM.
		if !explicitRecipe && len(ded) <= 1 {
			continue
		}
		for _, d := range ded {
			if d.ItemID == "" || d.Quantity <= 0 {
				continue
			}
			ck := "recipe_" + strings.ToUpper(strings.TrimSpace(d.ItemID))
			if added[ck] {
				continue
			}
			added[ck] = true
			out = append(out, map[string]interface{}{
				"componentType":     "FIXED",
				"component_type":    "FIXED",
				"componentKey":      ck,
				"component_key":     ck,
				"finishedGoodId":    d.ItemID,
				"finished_good_id":  d.ItemID,
				"quantityRequired":  d.Quantity,
				"quantity_required": d.Quantity,
				"unit":              firstNonEmpty(d.Unit, "pcs"),
			})
		}
	}
	return out
}

func (s *ResolutionService) cachedExpandRecipe(
	ctx context.Context,
	shopID, key string,
	cache map[string][]StockDeduction,
) []StockDeduction {
	ck := strings.ToUpper(strings.TrimSpace(key))
	if ck == "" {
		return nil
	}
	if cache != nil {
		if v, ok := cache[ck]; ok {
			return v
		}
	}
	ded, err := s.expandRecipe(ctx, shopID, key, 1)
	if err != nil || len(ded) == 0 {
		if cache != nil {
			cache[ck] = nil
		}
		return nil
	}
	if cache != nil {
		cache[ck] = ded
	}
	return ded
}
