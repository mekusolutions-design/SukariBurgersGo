package menu

import "testing"

func TestPortionsFIXEDUsesFinishedGoodOnly(t *testing.T) {
	// FIXED with finished_good_id + stale option_item_ids that are zero must NOT zero out availability
	lines := []map[string]interface{}{
		{
			"componentType":     "FIXED",
			"finishedGoodId":    "CHICKEN-BREAST",
			"quantityRequired":  1.0,
			"option_item_ids":   []interface{}{"UNUSED-ZERO-SKU"},
		},
	}
	stock := map[string]float64{
		"CHICKEN-BREAST": 10,
		"UNUSED-ZERO-SKU": 0,
	}
	p, fg := portionsFromLines(lines, stock, nil)
	if p != 10 {
		t.Fatalf("expected 10 portions from FG stock, got %d (fg=%s)", p, fg)
	}
}

func TestPortionsFIXEDInventorySelectOptionsSum(t *testing.T) {
	// FIXED without finished_good_id — inventory select options only
	lines := []map[string]interface{}{
		{
			"componentType":    "FIXED",
			"quantityRequired": 2.0,
			"option_item_ids":  []interface{}{"MOZZ", "CHEDDAR"},
		},
	}
	stock := map[string]float64{"MOZZ": 4, "CHEDDAR": 6}
	p, _ := portionsFromLines(lines, stock, nil)
	// sum 10 / need 2 = 5
	if p != 5 {
		t.Fatalf("expected 5, got %d", p)
	}
}

func TestPortionsCaseInsensitiveStock(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "chicken-breast", "quantityRequired": 1.0},
	}
	stock := map[string]float64{"CHICKEN-BREAST": 7}
	p, _ := portionsFromLines(lines, stock, nil)
	if p != 7 {
		t.Fatalf("case-insensitive stock expected 7, got %d", p)
	}
}

func TestPortionsCHOICESumsOptions(t *testing.T) {
	lines := []map[string]interface{}{
		{
			"componentType":    "CHOICE",
			"quantityRequired": 1.0,
			"option_item_ids":  []interface{}{"A", "B"},
		},
	}
	stock := map[string]float64{"A": 3, "B": 2}
	p, _ := portionsFromLines(lines, stock, nil)
	if p != 5 {
		t.Fatalf("expected 5, got %d", p)
	}
}
