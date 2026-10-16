package menu

import (
	"context"
	"strings"
)

// enrichLinesWithRecipeConstraints adds recipe BOM constraints for menus that are
// actually recipe-linked. Standalone FIXED / inventory items must keep their
// InventoryProjection sellable qty (Menu legacy) — never get zeroed by a speculative
// name match to an unrelated recipe.
//
// Apply BOM only when:
//   - menu has explicit recipeID, or
//   - expand via root FG / menu code finds a multi-ingredient recipe
// Display-name expansion is only used with explicit recipeID.
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

	explicitRecipe := strings.TrimSpace(recipeID) != ""

	keys := make([]string, 0, 8)
	seenKey := map[string]bool{}
	addKey := func(k string) {
		k = strings.TrimSpace(k)
		if k == "" || seenKey[strings.ToUpper(k)] {
			return
		}
		seenKey[strings.ToUpper(k)] = true
		keys = append(keys, k)
	}

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
	// Name only when catalog/event explicitly links a recipe
	if explicitRecipe {
		addKey(name)
	}

	added := map[string]bool{}
	appliedBOM := false
	for _, key := range keys {
		ded := s.cachedExpandRecipe(ctx, shopID, key, cache)
		if len(ded) == 0 {
			continue
		}
		// Without explicit recipeID, require a real BOM (2+ ingredients).
		// Single-ingredient expand is treated as stocked FG — already on FIXED line.
		if !explicitRecipe && len(ded) < 2 {
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
			appliedBOM = true
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
		// One successful BOM is enough
		if appliedBOM {
			break
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
