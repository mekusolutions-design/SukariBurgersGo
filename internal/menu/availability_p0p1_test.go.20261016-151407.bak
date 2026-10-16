package menu

import "testing"

func TestP0_FixedSodaPcs(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "SODA-300", "quantityRequired": 1.0, "unit": "pcs"},
	}
	stock := map[string]float64{"SODA-300": 34}
	units := map[string]string{"SODA-300": "pcs"}
	p, _ := ComputeMenuAvailability(lines, stock, units)
	if p != 34 {
		t.Fatalf("soda expected 34, got %d", p)
	}
}

func TestP0_SodaEvenIfItemUnitOdd(t *testing.T) {
	// Baseline behaviour: projection count / need — do not 86 because unit string is weird
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "SODA-300", "quantityRequired": 1.0, "unit": "pcs"},
	}
	stock := map[string]float64{"SODA-300": 34}
	units := map[string]string{"SODA-300": "ml"} // mis-tagged unit must not zero FIXED pcs need
	p, _ := ComputeMenuAvailability(lines, stock, units)
	if p != 34 {
		t.Fatalf("expected 34 with need pcs, got %d", p)
	}
}

func TestP0_CaseInsensitiveStock(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "soda-300", "quantityRequired": 1.0, "unit": "pcs"},
	}
	stock := map[string]float64{"SODA-300": 10}
	p, _ := ComputeMenuAvailability(lines, stock, nil)
	if p != 10 {
		t.Fatalf("expected 10, got %d", p)
	}
}

func TestP1_RecipeBaconMin(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "LETTUCE", "quantityRequired": 1.0, "unit": "pcs"},
		{"componentType": "FIXED", "finishedGoodId": "BACON", "quantityRequired": 30.0, "unit": "g"},
	}
	stock := map[string]float64{"LETTUCE": 7, "BACON": 82.5}
	units := map[string]string{"LETTUCE": "pcs", "BACON": "g"}
	p, _ := ComputeMenuAvailability(lines, stock, units)
	if p != 2 {
		t.Fatalf("expected 2, got %d", p)
	}
}

func TestP1_RecipeCheddar86(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "LETTUCE", "quantityRequired": 1.0, "unit": "pcs"},
		{"componentType": "FIXED", "finishedGoodId": "CHEDDAR", "quantityRequired": 43.48, "unit": "g"},
	}
	stock := map[string]float64{"LETTUCE": 7, "CHEDDAR": 6.5}
	units := map[string]string{"LETTUCE": "pcs", "CHEDDAR": "g"}
	p, _ := ComputeMenuAvailability(lines, stock, units)
	if p != 0 {
		t.Fatalf("expected 0, got %d", p)
	}
}

func TestP1_DoubleBacon(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "LETTUCE", "quantityRequired": 1.0, "unit": "pcs"},
		{"componentType": "FIXED", "finishedGoodId": "BACON", "quantityRequired": 60.0, "unit": "g"},
	}
	stock := map[string]float64{"LETTUCE": 7, "BACON": 82.5}
	units := map[string]string{"LETTUCE": "pcs", "BACON": "g"}
	p, _ := ComputeMenuAvailability(lines, stock, units)
	if p != 1 {
		t.Fatalf("expected 1, got %d", p)
	}
}
