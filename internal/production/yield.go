package production

import "strings"

// actualYieldMassGrams totals output mass in grams (pieces × unit_weight).
// Returns 0 when no line has a positive unit_weight (piece-only recipes).
func actualYieldMassGrams(outputs []Line) float64 {
	var grams float64
	any := false
	for _, o := range outputs {
		qty := o.ActualQuantity
		if qty < 0 {
			qty = 0
		}
		uw := o.UnitWeight
		if !(uw > 0) || qty <= 0 {
			continue
		}
		any = true
		u := strings.ToLower(strings.TrimSpace(o.WeightUnit))
		if u == "" {
			u = "g"
		}
		line := qty * uw
		if u == "kg" || u == "l" {
			grams += line * 1000
		} else {
			grams += line // g, ml, default
		}
	}
	if !any {
		return 0
	}
	return grams
}

// toRecipeYieldUnit converts mass in grams into the recipe yield unit.
func toRecipeYieldUnit(massGrams float64, recipeUnit string) float64 {
	u := strings.ToLower(strings.TrimSpace(recipeUnit))
	if u == "kg" || u == "l" {
		return massGrams / 1000
	}
	return massGrams // g, ml, or treat as gram-batch
}

// pieceSumOutputs sums discrete output quantities (stock units).
func pieceSumOutputs(outputs []Line) float64 {
	var s float64
	for _, o := range outputs {
		if o.ActualQuantity > 0 {
			s += o.ActualQuantity
		}
	}
	return s
}

// normalizeHeaderYield picks header actual yield for variance / % the same way Nest does:
// prefer mass in recipe unit when unit_weight is present; else client ActualQty; else piece sum.
func normalizeHeaderYield(in FinishInput) (actualQty float64, massGrams float64, fromMass bool) {
	massGrams = actualYieldMassGrams(in.Outputs)
	pieces := pieceSumOutputs(in.Outputs)

	if massGrams > 0 {
		// Infer unit from first weighted line when recipe unit unknown
		unit := ""
		for _, o := range in.Outputs {
			if o.UnitWeight > 0 {
				unit = o.WeightUnit
				if unit == "" {
					unit = o.Unit
				}
				break
			}
		}
		// If planned looks like grams (>= 50), report actual in grams for variance parity
		if in.PlannedQty >= 50 && (strings.EqualFold(unit, "g") || strings.EqualFold(unit, "ml") || unit == "") {
			return massGrams, massGrams, true
		}
		return toRecipeYieldUnit(massGrams, unit), massGrams, true
	}

	if in.ActualQty > 0 {
		return in.ActualQty, 0, false
	}
	return pieces, 0, false
}
