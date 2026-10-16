package menu

import "testing"

// Nest uses raw floor(stock/need). Wrong quantity_required on a line is data, not
// something the formula should invent unit gates for. Correct cheddar line:
// need 43.48, stock 6.5 → 0.

func TestNest_CheddarNeedCorrect(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "CHEDDAR", "quantityRequired": 43.48, "unit": "g"},
	}
	r := ComputeMenuAvailability(lines, map[string]float64{"CHEDDAR": 6.5})
	if r.MaxPortions != 0 {
		t.Fatalf("expected 0, got %d", r.MaxPortions)
	}
}

func TestNest_BaconNeedCorrect(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "BACON", "quantityRequired": 30.0, "unit": "g"},
	}
	r := ComputeMenuAvailability(lines, map[string]float64{"BACON": 82.5})
	if r.MaxPortions != 2 {
		t.Fatalf("expected 2, got %d", r.MaxPortions)
	}
}
