package menu

import (
	"math"
	"strings"

	"github.com/restflow/api-go/internal/units"
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

// ComputeMenuAvailability — Nest MenuService.listMenus parity:
//
//	min over FIXED/INVENTORY lines of floor(available_stock / quantity_required)
//	CHOICE/MULTI_CHOICE: floor(sum(option stocks) / quantity_required)
//
// Lines without a stock SKU are skipped (Nest continues; does not 86 the whole item).
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
		// Nest: need = Math.max(quantityRequired || 1, 0.0001)
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

		needUnit := firstStr(line, "unit")
		finishedGoodID := firstStr(
			line,
			"finishedGoodId", "finished_good_id",
			"inventory_item_id", "inventoryItemId",
		)

		ids := collectLineIDs(line)

		// Nest FIXED/INVENTORY: only the finished_good_id (not stale option_item_ids)
		if componentType == "FIXED" || componentType == "INVENTORY" || componentType == "" {
			if finishedGoodID != "" {
				ids = []string{finishedGoodID}
			}
		}

		if len(ids) == 0 {
			// Nest skips drafts with no finishedGoodId — does NOT zero the whole menu
			continue
		}
		if firstFG == "" {
			firstFG = ids[0]
		}

		if componentType == "CHOICE" || componentType == "MULTI_CHOICE" {
			// Nest: floor(sum(option.availableStock) / need)
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

		// FIXED / INVENTORY / default: Nest floor(snapshot.available / need)
		// Prefer numeric Nest path; only convert when need unit is explicitly mass/volume.
		for _, id := range ids {
			have := stockGet(stock, id)
			haveUnit := stockUnitFor(stockUnits, id, needUnit)
			p := portionNestParity(have, need, haveUnit, needUnit)
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

// portionNestParity matches Nest Math.floor(available / need) in the common case.
// When recipe lines carry unit g/ml, convert stock into that unit first.
func portionNestParity(have, need float64, haveUnit, needUnit string) int {
	if need <= 0 {
		need = 1
	}
	if have <= 0 {
		return 0
	}

	nu := units.NormalizeUnit(needUnit)
	switch nu {
	case "g", "kg", "ml", "l":
		haveBase, haveDim := units.ToBaseQuantity(have, firstNonEmpty(haveUnit, nu))
		needBase, needDim := units.ToBaseQuantity(need, nu)
		if haveDim != units.DimOther && needDim != units.DimOther && haveDim != needDim {
			// Cannot convert — Nest would still divide raw numbers; use raw as fallback
			return int(math.Floor(have / need))
		}
		if needBase <= 0 {
			return 0
		}
		p := int(math.Floor(haveBase / needBase))
		if p < 0 {
			return 0
		}
		return p
	default:
		// Nest FIXED path: Math.floor(available / need) — no unit gate
		p := int(math.Floor(have / need))
		if p < 0 {
			return 0
		}
		return p
	}
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

// computePortions is used by list_menus portionsFromLines.
func computePortionsExport(lines []map[string]interface{}, stock map[string]float64, stockUnits map[string]string) (int, string) {
	return computePortions(lines, stock, stockUnits)
}
