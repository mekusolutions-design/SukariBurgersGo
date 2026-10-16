package menu

import (
	"math"
	"strings"
)

// MenuAvailabilityResult mirrors Nest listMenus availability fields.
type MenuAvailabilityResult struct {
	MaxPortions int
	IsAvailable bool
	Lines       []map[string]interface{}
}

func stockGet(stock map[string]float64, id string) float64 {
	if id == "" || stock == nil {
		return 0
	}
	if v, ok := stock[id]; ok {
		return v
	}
	for key, v := range stock {
		if strings.EqualFold(key, id) {
			return v
		}
	}
	return 0
}

// stockUnitFor kept for normalizeMenuLines display only (not used in Nest portion math).
func stockUnitFor(stockUnits map[string]string, id, fallback string) string {
	if stockUnits != nil {
		for key, unit := range stockUnits {
			if strings.EqualFold(key, id) && strings.TrimSpace(unit) != "" {
				return unit
			}
		}
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	return "pcs"
}

// ComputeMenuAvailability — Nest MenuService.listMenus parity (single source of truth).
func ComputeMenuAvailability(
	lines []map[string]interface{},
	stock map[string]float64,
	stockUnits ...map[string]string,
) MenuAvailabilityResult {
	portions, _ := computePortions(lines, stock, nil)
	if portions < 0 {
		portions = 0
	}
	return MenuAvailabilityResult{
		MaxPortions: portions,
		IsAvailable: portions > 0,
		Lines:       lines,
	}
}

func hasStockConstraints(lines []map[string]interface{}) bool {
	for _, line := range lines {
		if line == nil {
			continue
		}
		if len(collectLineIDs(line)) > 0 {
			return true
		}
	}
	return false
}

// computePortions implements Nest listMenus exactly:
//
//	FIXED/INVENTORY: floor(available_stock / max(quantity_required, 0.0001))
//	CHOICE/MULTI_CHOICE: floor(sum(option stocks) / need)
//	maxPortions = min across lines; lines with no finished_good_id are skipped
func computePortions(
	lines []map[string]interface{},
	stock map[string]float64,
	_ map[string]string, // stockUnits ignored for Nest raw divide
) (int, string) {
	if len(lines) == 0 {
		return 0, ""
	}

	minPortions := math.MaxInt32
	firstFG := ""

	for _, line := range lines {
		if line == nil {
			continue
		}

		componentType := strings.ToUpper(firstStr(line, "componentType", "component_type"))
		need := floatAny(line["quantityRequired"])
		if need <= 0 {
			need = floatAny(line["quantity_required"])
		}
		if need <= 0 {
			need = 1
		}
		if need < 0.0001 {
			need = 0.0001
		}

		finishedGoodID := firstStr(
			line,
			"finishedGoodId", "finished_good_id",
			"inventory_item_id", "inventoryItemId",
		)

		// Nest FIXED / INVENTORY path
		if componentType == "FIXED" || componentType == "INVENTORY" || componentType == "" {
			if finishedGoodID == "" {
				// Nest: if (!finishedGoodId) continue;
				continue
			}
			if firstFG == "" {
				firstFG = finishedGoodID
			}
			have := stockGet(stock, finishedGoodID)
			p := int(math.Floor(have / need))
			if p < 0 {
				p = 0
			}
			if p < minPortions {
				minPortions = p
			}
			continue
		}

		if componentType == "CHOICE" || componentType == "MULTI_CHOICE" {
			ids := collectLineIDs(line)
			if len(ids) == 0 {
				continue
			}
			if firstFG == "" {
				firstFG = ids[0]
			}
			total := 0.0
			for _, id := range ids {
				total += stockGet(stock, id)
			}
			p := int(math.Floor(total / need))
			if p < 0 {
				p = 0
			}
			if p < minPortions {
				minPortions = p
			}
			continue
		}

		// Unknown type with a SKU: treat like FIXED (Nest-safe)
		if finishedGoodID != "" {
			if firstFG == "" {
				firstFG = finishedGoodID
			}
			have := stockGet(stock, finishedGoodID)
			p := int(math.Floor(have / need))
			if p < 0 {
				p = 0
			}
			if p < minPortions {
				minPortions = p
			}
		}
	}

	if minPortions == math.MaxInt32 || minPortions < 0 {
		return 0, firstFG
	}
	return minPortions, firstFG
}

// portionOne — Nest raw floor only (kept name for normalizeMenuLines callers).
func portionOne(have, need float64, _, _ string) int {
	if need <= 0 {
		need = 1
	}
	if need < 0.0001 {
		need = 0.0001
	}
	if have <= 0 {
		return 0
	}
	p := int(math.Floor(have / need))
	if p < 0 {
		return 0
	}
	return p
}

func collectLineIDs(line map[string]interface{}) []string {
	ids := []string{}
	seen := map[string]bool{}

	add := func(id string) {
		id = strings.TrimSpace(id)
		key := strings.ToUpper(id)
		if id != "" && !seen[key] {
			seen[key] = true
			ids = append(ids, id)
		}
	}

	add(firstStr(line,
		"finishedGoodId", "finished_good_id",
		"inventory_item_id", "inventoryItemId"))

	for _, key := range []string{"option_item_ids", "optionItemIds", "option_items", "optionItems"} {
		for _, id := range stringSliceAny(line[key]) {
			add(id)
		}
	}

	addOptions := func(options []interface{}) {
		for _, option := range options {
			if m, ok := option.(map[string]interface{}); ok {
				add(firstStr(m,
					"finishedGoodId", "finished_good_id",
					"item_id", "itemId"))
			}
		}
	}

	switch options := line["options"].(type) {
	case []interface{}:
		addOptions(options)
	case []map[string]interface{}:
		raw := make([]interface{}, 0, len(options))
		for _, option := range options {
			raw = append(raw, option)
		}
		addOptions(raw)
	}

	return ids
}
