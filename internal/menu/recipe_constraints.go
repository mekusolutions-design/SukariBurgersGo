package menu

import (
	"context"
	"strings"
)

// enrichRecipeConstraints adds RecipeIngredient FIXED lines so POS matches Menu:
// min over bacon g, cheddar g, lettuce, etc.
//
// Keys tried (in order):
//  1. explicit recipe_id (catalog / event)
//  2. menu code / MENU- strip / root finished good
// Only applies an expansion when it returns a real BOM (2+ ingredients),
// so FIXED sodas are never pulled into a false single-ingredient recipe.
// Display name alone is never used (avoids CHEESE / BACON standalone SKUs).
func (s *ResolutionService) enrichRecipeConstraints(
	ctx context.Context,
	shopID string,
	lines []map[string]interface{},
	menuID, code, rootFG, recipeID string,
	cache map[string][]StockDeduction,
) []map[string]interface{} {
	if s == nil || s.pool == nil {
		return lines
	}

	keys := make([]string, 0, 6)
	seen := map[string]bool{}
	add := func(k string) {
		k = strings.TrimSpace(k)
		if k == "" {
			return
		}
		u := strings.ToUpper(k)
		if seen[u] {
			return
		}
		seen[u] = true
		keys = append(keys, k)
	}
	add(recipeID)
	add(code)
	if strings.HasPrefix(strings.ToUpper(menuID), "MENU-") && len(menuID) > 5 {
		add(menuID[5:])
	}
	add(menuID)
	add(rootFG)

	explicit := strings.TrimSpace(recipeID) != ""

	for _, key := range keys {
		ded := s.cachedRecipeExpand(ctx, shopID, key, cache)
		if len(ded) == 0 {
			continue
		}
		// Without explicit recipe_id, require multi-ingredient BOM (burger recipes).
		if !explicit && len(ded) < 2 {
			continue
		}
		out := make([]map[string]interface{}, len(lines))
		copy(out, lines)
		added := map[string]bool{}
		for _, d := range ded {
			if d.ItemID == "" || d.Quantity <= 0 {
				continue
			}
			ck := strings.ToUpper(strings.TrimSpace(d.ItemID))
			if added[ck] {
				continue
			}
			added[ck] = true
			out = append(out, map[string]interface{}{
				"componentType":     "FIXED",
				"component_type":    "FIXED",
				"componentKey":      "recipe_" + ck,
				"component_key":     "recipe_" + ck,
				"finishedGoodId":    d.ItemID,
				"finished_good_id":  d.ItemID,
				"quantityRequired":  d.Quantity,
				"quantity_required": d.Quantity,
				"unit":              firstNonEmpty(d.Unit, "pcs"),
			})
		}
		return out
	}
	return lines
}

func (s *ResolutionService) cachedRecipeExpand(
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
