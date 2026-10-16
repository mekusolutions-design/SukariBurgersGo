package menu

import (
	"context"
	"strings"
)

// enrichWithExplicitRecipe appends FIXED constraints from RecipeIngredient
// only when recipeID is set on the menu/catalog. No display-name guessing.
func (s *ResolutionService) enrichWithExplicitRecipe(
	ctx context.Context,
	shopID, recipeID string,
	lines []map[string]interface{},
	cache map[string][]StockDeduction,
) []map[string]interface{} {
	recipeID = strings.TrimSpace(recipeID)
	if s == nil || s.pool == nil || recipeID == "" {
		return lines
	}

	ck := strings.ToUpper(recipeID)
	var ded []StockDeduction
	if cache != nil {
		if v, ok := cache[ck]; ok {
			ded = v
		} else {
			got, err := s.expandRecipe(ctx, shopID, recipeID, 1)
			if err != nil || len(got) == 0 {
				cache[ck] = []StockDeduction{}
				return lines
			}
			cache[ck] = got
			ded = got
		}
	} else {
		got, err := s.expandRecipe(ctx, shopID, recipeID, 1)
		if err != nil || len(got) == 0 {
			return lines
		}
		ded = got
	}
	if len(ded) == 0 {
		return lines
	}

	out := make([]map[string]interface{}, len(lines))
	copy(out, lines)
	seen := map[string]bool{}
	for _, d := range ded {
		if d.ItemID == "" || d.Quantity <= 0 {
			continue
		}
		key := strings.ToUpper(strings.TrimSpace(d.ItemID))
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, map[string]interface{}{
			"componentType":     "FIXED",
			"component_type":    "FIXED",
			"componentKey":      "recipe_" + key,
			"component_key":     "recipe_" + key,
			"finishedGoodId":    d.ItemID,
			"finished_good_id":  d.ItemID,
			"quantityRequired":  d.Quantity,
			"quantity_required": d.Quantity,
			"unit":              firstNonEmpty(d.Unit, "pcs"),
		})
	}
	return out
}
