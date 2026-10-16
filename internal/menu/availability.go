package menu

import (
	"math"
	"strings"

	"github.com/mekusolutions-design/SukariBurgersGo/internal/units"
)

// AvailabilityLine is one stock constraint used for max portions.
type AvailabilityLine struct {
	ItemID   string
	Need     float64
	Have     float64
	Portions int
}

// MenuAvailabilityResult is the single source of truth for POS + Menu parity.
type MenuAvailabilityResult struct {
	MaxPortions int
	IsAvailable bool
	Lines       []AvailabilityLine
	FirstFG     string
}

// hasStockConstraints reports whether normalized lines bind any inventory SKU.
func hasStockConstraints(lines []map[string]interface{}) bool {
	for _, lm := range lines {
		if firstStr(lm, "finishedGoodId", "finished_good_id", "inventory_item_id", "inventoryItemId") != "" {
			return true
		}
		if len(stringSliceAny(lm["option_item_ids"])) > 0 || len(stringSliceAny(lm["optionItemIds"])) > 0 {
			return true
		}
		if opts, ok := lm["options"].([]map[string]interface{}); ok && len(opts) > 0 {
			return true
		}
		if opts, ok := lm["options"].([]interface{}); ok && len(opts) > 0 {
			return true
		}
	}
	return false
}

// portionsFromHaveNeed converts stock into the need unit before floor division.
// Prevents "49953 left" when inventory is grams and need defaults to 1 pcs.
func portionsFromHaveNeed(have, need float64, haveUnit, needUnit string) int {
	if need <= 0 {
		need = 1
	}
	if have <= 0 {
		return 0
	}
	haveU := units.NormalizeUnit(haveUnit)
	needU := units.NormalizeUnit(needUnit)
	if needU == "" {
		needU = "pcs"
	}
	if haveU == "" {
		haveU = needU
	}

	haveBase, haveDim := units.ToBaseQuantity(have, haveU)
	needBase, needDim := units.ToBaseQuantity(need, needU)

	// Dimension mismatch (e.g. grams of cheese vs need 1 "pcs"): do not treat raw mass as portions.
	if haveDim != units.DimOther && needDim != units.DimOther && haveDim != needDim {
		return 0
	}
	// Mass/volume with need still at default 1 and huge on-hand → refuse (likely missing recipe qty).
	if (haveDim == units.DimMass || haveDim == units.DimVolume) && need <= 1.0001 && haveBase > 500 {
		// only allow if need unit is also mass/volume with real recipe qty (need already > 1 after conversion path)
		if needU == "pcs" || needU == "" {
			return 0
		}
	}
	if needBase <= 0 {
		return 0
	}
	p := int(math.Floor(haveBase / needBase))
	if p < 0 {
		return 0
	}
	// Hard sanity: no menu should claim tens of thousands of portions
	if p > 9999 {
		return 9999
	}
	return p
}

// ComputeMenuAvailability: portions = min over constraints of floor(have/need) with unit conversion.
func ComputeMenuAvailability(lines []map[string]interface{}, stock map[string]float64, stockUnits map[string]string) MenuAvailabilityResult {
	res := MenuAvailabilityResult{MaxPortions: 0, Lines: nil}
	if len(lines) == 0 {
		return res
	}

	minP := math.MaxInt32
	firstFG := ""
	breakdown := make([]AvailabilityLine, 0, len(lines))

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

		fg := firstStr(lm, "finishedGoodId", "finished_good_id", "inventory_item_id", "inventoryItemId")
		optIDs := collectOptionIDs(lm)

		switch ct {
		case "CHOICE", "MULTI_CHOICE":
			ids := optIDs
			if fg != "" {
				ids = append([]string{fg}, ids...)
			}
			if len(ids) == 0 {
				continue
			}
			if firstFG == "" {
				firstFG = ids[0]
			}
			total := 0.0
			// Sum in need-unit after conversion per option
			for _, id := range ids {
				h := stockLookup(stock, id)
				hu := stockUnitLookup(stockUnits, id)
				// convert each have into need unit
				conv := units.ConvertQuantity(h, hu, needUnit)
				// if dimension mismatch ConvertQuantity returns unchanged — skip bad dims
				hb, hd := units.ToBaseQuantity(h, hu)
				nb, nd := units.ToBaseQuantity(1, needUnit)
				if hd != units.DimOther && nd != units.DimOther && hd != nd {
					continue
				}
				_ = hb
				_ = nb
				total += conv
			}
			p := portionsFromHaveNeed(total, need, needUnit, needUnit)
			breakdown = append(breakdown, AvailabilityLine{ItemID: ids[0], Need: need, Have: total, Portions: p})
			if p < minP {
				minP = p
			}

		default: // FIXED, INVENTORY, empty
			if fg != "" {
				if firstFG == "" {
					firstFG = fg
				}
				have := stockLookup(stock, fg)
				haveUnit := stockUnitLookup(stockUnits, fg)
				if needUnit == "pcs" && haveUnit != "" && haveUnit != "pcs" {
					// Prefer inventory unit as need unit when line forgot grams
					// only if quantity looks like a recipe weight (need != 1 or unit was set on line)
					if firstStr(lm, "unit") != "" {
						// keep needUnit from line
					} else if units.NormalizeUnit(haveUnit) == "g" || units.NormalizeUnit(haveUnit) == "ml" {
						// need is likely wrong (default 1 pcs) against mass stock — zero this constraint
						// unless quantityRequired was explicitly set > 0 from recipe
						if floatAny(lm["quantityRequired"]) <= 0 && floatAny(lm["quantity_required"]) <= 0 {
							have = 0
						} else {
							needUnit = haveUnit
						}
					}
				}
				p := portionsFromHaveNeed(have, need, haveUnit, needUnit)
				breakdown = append(breakdown, AvailabilityLine{ItemID: fg, Need: need, Have: have, Portions: p})
				if p < minP {
					minP = p
				}
				continue
			}
			if len(optIDs) == 0 {
				continue
			}
			if firstFG == "" {
				firstFG = optIDs[0]
			}
			total := 0.0
			for _, id := range optIDs {
				h := stockLookup(stock, id)
				hu := stockUnitLookup(stockUnits, id)
				total += units.ConvertQuantity(h, hu, needUnit)
			}
			p := portionsFromHaveNeed(total, need, needUnit, needUnit)
			breakdown = append(breakdown, AvailabilityLine{ItemID: optIDs[0], Need: need, Have: total, Portions: p})
			if p < minP {
				minP = p
			}
		}
	}

	if minP == math.MaxInt32 {
		return MenuAvailabilityResult{MaxPortions: 0, FirstFG: firstFG, Lines: breakdown}
	}
	if minP < 0 {
		minP = 0
	}
	return MenuAvailabilityResult{
		MaxPortions: minP,
		IsAvailable: minP > 0,
		FirstFG:     firstFG,
		Lines:       breakdown,
	}
}

func stockUnitLookup(stockUnits map[string]string, itemID string) string {
	if stockUnits == nil || itemID == "" {
		return "pcs"
	}
	if u, ok := stockUnits[itemID]; ok && u != "" {
		return u
	}
	up := strings.ToUpper(itemID)
	if u, ok := stockUnits[up]; ok && u != "" {
		return u
	}
	return "pcs"
}

func collectOptionIDs(lm map[string]interface{}) []string {
	ids := stringSliceAny(lm["option_item_ids"])
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
	return ids
}

// dedupeComponentLines keeps last line per component_key (Nest Map semantics on adjust).
func dedupeComponentLines(lines []map[string]interface{}) []map[string]interface{} {
	if len(lines) == 0 {
		return lines
	}
	order := make([]string, 0, len(lines))
	byKey := map[string]map[string]interface{}{}
	anon := 0
	for _, lm := range lines {
		if lm == nil {
			continue
		}
		key := firstStr(lm, "componentKey", "component_key")
		if key == "" {
			key = firstStr(lm, "finishedGoodId", "finished_good_id")
		}
		if key == "" {
			anon++
			key = "_anon_" + itoa(anon)
		}
		if _, exists := byKey[key]; !exists {
			order = append(order, key)
		}
		byKey[key] = lm
	}
	out := make([]map[string]interface{}, 0, len(order))
	for _, k := range order {
		out = append(out, byKey[k])
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
