package costing

import "math"

// ResolveProductionCost prefers actual input sum; treats mislabeled unit_cost as batch total
// when it is close to totalInputCost (Nest/Michael UI sends batch total in "unit_cost").
func ResolveProductionCost(totalInputCost, manualTotal, manualUnitOrTotal, totalMade float64) float64 {
	if manualTotal > 0 && !math.IsNaN(manualTotal) {
		return manualTotal
	}
	if manualUnitOrTotal > 0 && !math.IsNaN(manualUnitOrTotal) {
		v := manualUnitOrTotal
		if totalInputCost > 0 {
			rel := math.Abs(v-totalInputCost) / math.Max(totalInputCost, 1e-9)
			if rel < 0.15 || (totalMade > 1 && v > totalInputCost*0.5) {
				return v // batch total mislabeled as unit cost
			}
		}
		if totalMade > 0 {
			return v * totalMade
		}
		return v
	}
	if totalInputCost > 0 {
		return totalInputCost
	}
	return 0
}

// FinishCostMath implements Michael / Oct 3 rules:
// unitCost = productionCost / (good + waste); stock = good only; waste value = waste * unitCost
func FinishCostMath(good, waste, productionCost, planned float64) (
	totalMade, unitCost, toStockValue, wasteValue, unaccounted float64, reconciliationOK bool,
) {
	if good < 0 {
		good = 0
	}
	if waste < 0 {
		waste = 0
	}
	totalMade = good + waste
	if totalMade > 0 {
		unitCost = RoundMoney6(productionCost / totalMade)
	} else if good > 0 {
		unitCost = RoundMoney6(productionCost / good)
	}
	toStockValue = RoundMoney4(good * unitCost)
	wasteValue = RoundMoney4(waste * unitCost)
	if planned > totalMade {
		unaccounted = planned - totalMade
	}
	reconciliationOK = math.Abs(toStockValue+wasteValue-productionCost) < 0.02
	return
}

func RoundMoney6(f float64) float64 {
	return math.Round(f*1e6) / 1e6
}

func RoundMoney4(f float64) float64 {
	return math.Round(f*1e4) / 1e4
}
