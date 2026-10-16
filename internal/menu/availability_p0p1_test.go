package menu

import "testing"

func TestP0_FixedSodaPcs(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "SODA-300", "quantityRequired": 1.0, "unit": "pcs"},
	}
	r := ComputeMenuAvailability(lines, map[string]float64{"SODA-300": 34})
	if r.MaxPortions != 34 {
		t.Fatalf("got %d", r.MaxPortions)
	}
}

func TestP0_CaseInsensitiveStock(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "soda-300", "quantityRequired": 1.0},
	}
	r := ComputeMenuAvailability(lines, map[string]float64{"SODA-300": 10})
	if r.MaxPortions != 10 {
		t.Fatalf("got %d", r.MaxPortions)
	}
}

func TestP1_RecipeBaconMin(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "LETTUCE", "quantityRequired": 1.0},
		{"componentType": "FIXED", "finishedGoodId": "BACON", "quantityRequired": 30.0},
	}
	r := ComputeMenuAvailability(lines, map[string]float64{"LETTUCE": 7, "BACON": 82.5})
	if r.MaxPortions != 2 {
		t.Fatalf("got %d", r.MaxPortions)
	}
}
