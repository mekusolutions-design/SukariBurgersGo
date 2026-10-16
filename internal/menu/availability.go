package menu

import (
	"math"
	"strings"

	"github.com/mekusolutions-design/SukariBurgersGo/internal/units"
)

// portionsFromHaveNeed — Nest/Menu-aligned.
//
// • need in g/ml (recipe ingredients): convert to same base, then floor divide.
// • need in pcs / empty (FIXED sellable): raw projection qty / need — same as the
//   working baseline. Do NOT zero sodas when Item.unit is oddly set.
func portionsFromHaveNeed(have, need float64, haveUnit, needUnit string) int {
	if need <= 0 {
		need = 1
	}
	if have <= 0 {
		return 0
	}
	needU := units.NormalizeUnit(needUnit)
	haveU := units.NormalizeUnit(haveUnit)

	switch needU {
	case "g", "kg", "ml", "l":
		haveBase, haveDim := units.ToBaseQuantity(have, firstNonEmpty(haveU, needU))
		needBase, needDim := units.ToBaseQuantity(need, needU)
		if haveDim != units.DimOther && needDim != units.DimOther && haveDim != needDim {
			return 0
		}
		if needBase <= 0 {
			return 0
		}
		p := int(math.Floor(haveBase / needBase))
		if p < 0 {
			return 0
		}
		if p > 9999 {
			return 9999
		}
		return p
	default:
		// Baseline FIXED / pcs path
		p := int(math.Floor(have / need))
		if p < 0 {
			return 0
		}
		if p > 9999 {
			return 9999
		}
		return p
	}
}

func stockLookup(stock map[string]float64, itemID string) float64 {
	if itemID == "" || stock == nil {
		return 0
	}
	if v, ok := stock[itemID]; ok {
		return v
	}
	if v, ok := stock[strings.ToUpper(itemID)]; ok {
		return v
	}
	if v, ok := stock[strings.ToLower(itemID)]; ok {
		return v
	}
	return 0
}

func stockUnitLookup(stockUnits map[string]string, itemID string) string {
	if itemID == "" || stockUnits == nil {
		return "pcs"
	}
	if u, ok := stockUnits[itemID]; ok && u != "" {
		return u
	}
	if u, ok := stockUnits[strings.ToUpper(itemID)]; ok && u != "" {
		return u
	}
	return "pcs"
}

// ComputeMenuAvailability — single formula for POS + Menu chips.
func ComputeMenuAvailability(lines []map[string]interface{}, stock map[string]float64, stockUnits map[string]string) (maxPortions int, firstFG string) {
	if len(lines) == 0 {
		return 0, ""
	}
	minP := math.MaxInt32
	for _, lm := range lines {
		ct := strings.ToUpper(firstStr(lm, "componentType", "component_type"))
		need := floatAny(lm["quantityRequired"])
		if need <= 0 {
			need = floatAny(lm["quantity_required"])
		}
		if need <= 0 {
			need = 1
		}
		needUnit := firstStr(lm, "unit")
		if needUnit == "" {
			needUnit = "pcs"
		}

		ids := []string{}
		if fg := firstStr(lm, "finishedGoodId", "finished_good_id", "inventory_item_id", "inventoryItemId"); fg != "" {
			ids = append(ids, fg)
		}
		ids = append(ids, stringSliceAny(lm["option_item_ids"])...)
		ids = append(ids, stringSliceAny(lm["optionItemIds"])...)
		if opts, ok := lm["options"].([]map[string]interface{}); ok {
			for _, o := range opts {
				if oid := firstStr(o, "finishedGoodId", "finished_good_id", "item_id", "itemId"); oid != "" {
					ids = append(ids, oid)
				}
			}
		}
		if opts, ok := lm["options"].([]interface{}); ok {
			for _, o := range opts {
				if om, ok := o.(map[string]interface{}); ok {
					if oid := firstStr(om, "finishedGoodId", "finished_good_id", "item_id", "itemId"); oid != "" {
						ids = append(ids, oid)
					}
				}
			}
		}
		if len(ids) == 0 {
			continue
		}
		if firstFG == "" {
			firstFG = ids[0]
		}

		if ct == "CHOICE" || ct == "MULTI_CHOICE" {
			total := 0.0
			for _, id := range ids {
				total += stockLookup(stock, id)
			}
			p := portionsFromHaveNeed(total, need, needUnit, needUnit)
			if p < minP {
				minP = p
			}
			continue
		}

		for _, id := range ids {
			h := stockLookup(stock, id)
			hu := stockUnitLookup(stockUnits, id)
			nu := needUnit
			// Recipe line with qty in grams but unit omitted on line
			if firstStr(lm, "unit") == "" && need > 1.0001 {
				hn := units.NormalizeUnit(hu)
				if hn == "g" || hn == "ml" {
					nu = hu
				}
			}
			p := portionsFromHaveNeed(h, need, hu, nu)
			if p < minP {
				minP = p
			}
		}
	}
	if minP == math.MaxInt32 {
		return 0, firstFG
	}
	if minP < 0 {
		return 0, firstFG
	}
	return minP, firstFG
}
