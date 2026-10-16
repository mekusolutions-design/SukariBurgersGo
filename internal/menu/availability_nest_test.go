package menu

import "testing"

// Nest listMenus: floor(available / need), min across FIXED lines; skip empty SKUs.

func TestNestParity_FixedSoda(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "SODA-300", "quantityRequired": 1.0, "unit": "pcs"},
	}
	p, _ := computePortions(lines, map[string]float64{"SODA-300": 34}, nil)
	if p != 34 {
		t.Fatalf("got %d", p)
	}
}

func TestNestParity_SkipLineWithoutSKU(t *testing.T) {
	// One incomplete line must NOT 86 the whole menu (old Go fail-closed bug)
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "quantityRequired": 1.0},
		{"componentType": "FIXED", "finishedGoodId": "SODA-300", "quantityRequired": 1.0, "unit": "pcs"},
	}
	p, _ := computePortions(lines, map[string]float64{"SODA-300": 10}, nil)
	if p != 10 {
		t.Fatalf("got %d want 10", p)
	}
}

func TestNestParity_MinBacon(t *testing.T) {
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

func TestNestParity_Cheddar86(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "LETTUCE", "quantityRequired": 1.0, "unit": "pcs"},
		{"componentType": "FIXED", "finishedGoodId": "CHEDDAR", "quantityRequired": 43.48, "unit": "g"},
	}
	p, _ := computePortions(lines,
		map[string]float64{"LETTUCE": 7, "CHEDDAR": 6.5},
		map[string]string{"LETTUCE": "pcs", "CHEDDAR": "g"})
	if p != 0 {
		t.Fatalf("got %d want 0", p)
	}
}

func TestNestParity_ChoiceSum(t *testing.T) {
	lines := []map[string]interface{}{
		{
			"componentType": "CHOICE", "quantityRequired": 1.0,
			"option_item_ids": []interface{}{"A", "B"},
		},
	}
	p, _ := computePortions(lines, map[string]float64{"A": 3, "B": 2}, nil)
	if p != 5 {
		t.Fatalf("got %d want 5", p)
	}
}
