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

func TestP0_CaseInsensitiveStock(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "soda-300", "quantityRequired": 1.0, "unit": "pcs"},
	}
	stock := map[string]float64{"SODA-300": 10}
	units := map[string]string{"SODA-300": "pcs"}
	p, _ := ComputeMenuAvailability(lines, stock, units)
	if p != 10 {
		t.Fatalf("expected 10, got %d", p)
	}
}

func TestP0_NoGramAsPortion(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "CHEDDAR", "quantityRequired": 1.0, "unit": "pcs"},
	}
	stock := map[string]float64{"CHEDDAR": 49953}
	units := map[string]string{"CHEDDAR": "g"}
	p, _ := ComputeMenuAvailability(lines, stock, units)
	if p != 0 {
		t.Fatalf("grams with need=1 pcs must be 0, got %d", p)
	}
}

func TestP1_RecipeGramsMin(t *testing.T) {
	// Explicit recipe BOM lines (as enrichWithExplicitRecipe would append)
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "LETTUCE", "quantityRequired": 1.0, "unit": "pcs"},
		{"componentType": "FIXED", "finishedGoodId": "BACON", "quantityRequired": 30.0, "unit": "g"},
	}
	stock := map[string]float64{"LETTUCE": 7, "BACON": 82.5}
	units := map[string]string{"LETTUCE": "pcs", "BACON": "g"}
	p, _ := ComputeMenuAvailability(lines, stock, units)
	if p != 2 {
		t.Fatalf("bacon burger expected 2, got %d", p)
	}
}

func TestP1_CheddarExhausted(t *testing.T) {
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
