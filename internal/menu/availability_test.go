package menu

import "testing"

func TestFixedSoda(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "SODA-300", "quantityRequired": 1.0, "unit": "pcs"},
	}
	p, _ := computePortions(lines, map[string]float64{"SODA-300": 34}, map[string]string{"SODA-300": "pcs"})
	if p != 34 {
		t.Fatalf("got %d", p)
	}
}

func TestBaconRecipe(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "LETTUCE", "quantityRequired": 1.0, "unit": "pcs"},
		{"componentType": "FIXED", "finishedGoodId": "BACON", "quantityRequired": 30.0, "unit": "g"},
	}
	p, _ := computePortions(lines,
		map[string]float64{"LETTUCE": 7, "BACON": 82.5},
		map[string]string{"LETTUCE": "pcs", "BACON": "g"})
	if p != 2 {
		t.Fatalf("got %d want 2", p)
	}
}

func TestCheeseRecipe86(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "CHEDDAR", "quantityRequired": 43.48, "unit": "g"},
	}
	p, _ := computePortions(lines,
		map[string]float64{"CHEDDAR": 6.5},
		map[string]string{"CHEDDAR": "g"})
	if p != 0 {
		t.Fatalf("got %d want 0", p)
	}
}
