package menu

import (
	"context"
	"strings"
)

// enrichLinesWithRecipeConstraints adds FIXED constraints from RecipeIngredient
// only when we have an explicit recipe key — never by fuzzy menu name.
// Nest listMenus does NOT expand recipes for maxPortions; it mins FIXED lines.
// We expand only CatalogMenuItem.recipe_id (and exact FG recipe keys) so
// "Single Patty Cheese" is not forced onto the same recipe as "Single Patty".
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

	keys := make([]string, 0, 4)
	seenKey := map[string]bool{}
	addKey := func(k string) {
		k = strings.TrimSpace(k)
		if k == "" || seenKey[k] {
			return
		}
		// Never use display names as recipe keys (collapses all "Patty *" menus)
		if strings.Contains(strings.ToLower(k), " ") {
			return
		}
		seenKey[k] = true
		keys = append(keys, k)
	}

	// Explicit catalog recipe only (per menu item)
	addKey(recipeID)

	// Root FG / stock SKU if it is itself a recipe product (exact item_id / recipe_code)
	addKey(rootFG)

	// Do NOT add menuID/code/name — those cause every burger to share one recipe.

	added := map[string]bool{}
	for _, key := range keys {
		ded := s.cachedExpandRecipe(ctx, shopID, key, cache)
		for _, d := range ded {
			if d.ItemID == "" || d.Quantity <= 0 {
				continue
			}
			ck := "recipe_" + d.ItemID
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
