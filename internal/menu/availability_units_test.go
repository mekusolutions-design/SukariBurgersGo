package menu

import "testing"

func TestMassStockNotCountedAsPortionsWithNeedOnePcs(t *testing.T) {
	// Bug: SINGLE PATTY CHEESE showed 49953 left — cheddar grams / need 1 pcs
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "CHEDDAR", "quantityRequired": 1.0, "unit": "pcs"},
	}
	stock := map[string]float64{"CHEDDAR": 49953}
	units := map[string]string{"CHEDDAR": "g"}
	r := ComputeMenuAvailability(lines, stock, units)
	if r.MaxPortions != 0 {
		t.Fatalf("expected 0 portions for mass stock with need=1 pcs, got %d", r.MaxPortions)
	}
}

func TestRecipeGramsPortions(t *testing.T) {
	// Menu-correct: 82.5 g bacon / 30 g per burger = 2
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "LETTUCE", "quantityRequired": 1.0, "unit": "pcs"},
		{"componentType": "FIXED", "finishedGoodId": "BACON", "quantityRequired": 30.0, "unit": "g"},
	}
	stock := map[string]float64{"LETTUCE": 7, "BACON": 82.5}
	units := map[string]string{"LETTUCE": "pcs", "BACON": "g"}
	r := ComputeMenuAvailability(lines, stock, units)
	if r.MaxPortions != 2 {
		t.Fatalf("expected 2, got %d", r.MaxPortions)
	}
}

func TestCheddarRecipePortions(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "LETTUCE", "quantityRequired": 1.0, "unit": "pcs"},
		{"componentType": "FIXED", "finishedGoodId": "CHEDDAR", "quantityRequired": 43.48, "unit": "g"},
	}
	stock := map[string]float64{"LETTUCE": 7, "CHEDDAR": 6.5}
	units := map[string]string{"LETTUCE": "pcs", "CHEDDAR": "g"}
	r := ComputeMenuAvailability(lines, stock, units)
	if r.MaxPortions != 0 {
		t.Fatalf("expected 0 (86'd), got %d", r.MaxPortions)
	}
}

func TestSodaPcsPortions(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "SODA-300", "quantityRequired": 1.0, "unit": "pcs"},
	}
	stock := map[string]float64{"SODA-300": 34}
	units := map[string]string{"SODA-300": "pcs"}
	r := ComputeMenuAvailability(lines, stock, units)
	if r.MaxPortions != 34 {
		t.Fatalf("expected 34, got %d", r.MaxPortions)
	}
}
