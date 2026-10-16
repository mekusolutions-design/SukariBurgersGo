package menu

import (
	"context"
	"strings"
)

// enrichExplicitRecipe appends RecipeIngredient lines only for a known recipe_id.
// Never matches by display name (protects FIXED sodas / standalone BACON chips).
func (s *ResolutionService) enrichExplicitRecipe(
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
		}
	}
	if ded == nil {
		got, err := s.expandRecipe(ctx, shopID, recipeID, 1)
		if err != nil || len(got) == 0 {
			if cache != nil {
				cache[ck] = []StockDeduction{}
			}
			return lines
		}
		if cache != nil {
			cache[ck] = got
		}
		ded = got
	}
	if len(ded) == 0 {
		return lines
	}

	out := append([]map[string]interface{}{}, lines...)
	seen := map[string]bool{}
	for _, d := range ded {
		if d.ItemID == "" || d.Quantity <= 0 {
			continue
		}
		k := strings.ToUpper(strings.TrimSpace(d.ItemID))
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, map[string]interface{}{
			"componentType": "FIXED", "component_type": "FIXED",
			"componentKey": "recipe_" + k, "component_key": "recipe_" + k,
			"finishedGoodId": d.ItemID, "finished_good_id": d.ItemID,
			"quantityRequired": d.Quantity, "quantity_required": d.Quantity,
			"unit": firstNonEmpty(d.Unit, "pcs"),
		})
	}
	return out
}
