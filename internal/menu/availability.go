package menu

import (
	"math"
	"strings"

	"github.com/restflow/api-go/internal/units"
)

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

// ComputeMenuAvailability calculates the minimum portions across required
// lines. The optional unit map contains authoritative stock units when known.
func ComputeMenuAvailability(
	lines []map[string]interface{},
	stock map[string]float64,
	stockUnits ...map[string]string,
) MenuAvailabilityResult {
	var unitsByID map[string]string
	if len(stockUnits) > 0 {
		unitsByID = stockUnits[0]
	}

	portions, _ := computePortions(lines, stock, unitsByID)
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

func computePortions(
	lines []map[string]interface{},
	stock map[string]float64,
	stockUnits map[string]string,
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
		needUnit := firstStr(line, "unit")
		if needUnit == "" {
			needUnit = "pcs"
		}

		ids := collectLineIDs(line)
		finishedGoodID := firstStr(
			line,
			"finishedGoodId", "finished_good_id",
			"inventory_item_id", "inventoryItemId",
		)

		// For FIXED lines, an explicit finished good is authoritative.
		// This prevents stale option IDs from inflating or limiting availability.
		if componentType == "FIXED" && finishedGoodID != "" {
			ids = []string{finishedGoodID}
		}
		if len(ids) == 0 {
			// A required component without an inventory link cannot be
			// counted as sellable; fail closed instead of ignoring it.
			return 0, firstFG
		}
		if firstFG == "" {
			firstFG = ids[0]
		}

		// FIXED without an explicit finished good uses its inventory options.
		// CHOICE and MULTI_CHOICE retain summed alternative availability.
		if (componentType == "FIXED" && finishedGoodID == "") ||
			componentType == "CHOICE" ||
			componentType == "MULTI_CHOICE" {
			// Calculate each alternative independently using its stock unit.
			// Sum sellable portions, not incompatible raw stock quantities.
			totalPortions := 0
			for _, id := range ids {
				have := stockGet(stock, id)
				haveUnit := stockUnitFor(stockUnits, id, needUnit)
				totalPortions += portionOne(have, need, haveUnit, needUnit)
			}
			if totalPortions < minPortions {
				minPortions = totalPortions
			}
			continue
		}

		for _, id := range ids {
			have := stockGet(stock, id)
			haveUnit := stockUnitFor(stockUnits, id, needUnit)
			p := portionOne(have, need, haveUnit, needUnit)
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

func portionOne(have, need float64, haveUnit, needUnit string) int {
	if need <= 0 {
		need = 1
	}
	if have <= 0 {
		return 0
	}

	haveNormalized := units.NormalizeUnit(haveUnit)
	needNormalized := units.NormalizeUnit(needUnit)

	// Preserve legacy handling for a mis-tagged volume unit on a fixed
	// count-based sellable (e.g. a soda SKU tagged ml but sold in pcs).
	// Mass inventory must never be treated as a count of pieces.
	if needNormalized == "pcs" {
		if haveNormalized == "g" || haveNormalized == "kg" {
			return 0
		}
		p := int(math.Floor(have / need))
		if p < 0 {
			return 0
		}
		return p
	}

	haveBase, haveDimension := units.ToBaseQuantity(have, haveUnit)
	needBase, needDimension := units.ToBaseQuantity(need, needUnit)

	if haveDimension == units.DimOther ||
		needDimension == units.DimOther ||
		haveDimension != needDimension ||
		needBase <= 0 {
		return 0
	}

	p := int(math.Floor(haveBase / needBase))
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
