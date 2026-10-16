package menu

import (
	"math"
	"strings"
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

// ComputeMenuAvailability implements Nest Menu maxPortions:
//
//	portions = min over ALL required constraints of floor(have / need)
//
// FIXED / INVENTORY: finished_good_id only (not option min).
// CHOICE / MULTI_CHOICE: sum of option stocks / need.
// FIXED with only option_item_ids: sum options / need (inventory-select).
func ComputeMenuAvailability(lines []map[string]interface{}, stock map[string]float64) MenuAvailabilityResult {
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
			for _, id := range ids {
				total += stockLookup(stock, id)
			}
			p := int(math.Floor(total / need))
			if p < 0 {
				p = 0
			}
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
				p := int(math.Floor(have / need))
				if p < 0 {
					p = 0
				}
				breakdown = append(breakdown, AvailabilityLine{ItemID: fg, Need: need, Have: have, Portions: p})
				if p < minP {
					minP = p
				}
				continue
			}
			// Inventory-select FIXED without finished_good_id
			if len(optIDs) == 0 {
				continue
			}
			if firstFG == "" {
				firstFG = optIDs[0]
			}
			total := 0.0
			for _, id := range optIDs {
				total += stockLookup(stock, id)
			}
			p := int(math.Floor(total / need))
			if p < 0 {
				p = 0
			}
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
