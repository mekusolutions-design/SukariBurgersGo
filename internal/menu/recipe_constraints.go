package menu

import (
	"context"
	"strings"
)

// enrichLinesWithRecipeConstraints adds FIXED constraints from RecipeIngredient
// so POS maxPortions matches the Menu page: min over every recipe ingredient.
//
// Key order matters: resolve the *menu/burger recipe first* (recipeID, menu id/code/name,
// root finished good). Only then expand per-line finished goods. Expanding lettuce FG
// first used to attach a lettuce-only recipe and miss bacon/cheddar — POS then showed
// lettuce stock (e.g. 7) for every burger.
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

	// 1) Commercial / recipe identity first (burger BOM)
	addKey(recipeID)
	addKey(menuID)
	addKey(code)
	addKey(name)
	addKey(rootFG)
	if strings.HasPrefix(strings.ToUpper(menuID), "MENU-") && len(menuID) > 5 {
		addKey(menuID[5:])
	}
	if strings.HasPrefix(strings.ToUpper(code), "MENU-") && len(code) > 5 {
		addKey(code[5:])
	}

	// 2) Line-level FGs last (shared toppings / inventory links)
	for _, lm := range lines {
		addKey(firstStr(lm, "finishedGoodId", "finished_good_id", "inventory_item_id", "inventoryItemId"))
	}

	added := map[string]bool{}
	for _, key := range keys {
		ded := s.cachedExpandRecipe(ctx, shopID, key, cache)
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
