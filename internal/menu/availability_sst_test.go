package menu

import "testing"

// Acceptance from ops screenshots — Nest Menu Available ≡ POS chips formula.

func TestSST_SinglePatty(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "BUNS", "quantityRequired": 1.0},
		{"componentType": "FIXED", "finishedGoodId": "LETTUCE", "quantityRequired": 1.0},
		{"componentType": "FIXED", "finishedGoodId": "TOMATOES", "quantityRequired": 0.25},
		{"componentType": "FIXED", "finishedGoodId": "PATTIES", "quantityRequired": 1.0},
		{"componentType": "FIXED", "finishedGoodId": "SAUCE", "quantityRequired": 45.0},
		{"componentType": "FIXED", "finishedGoodId": "MARINATION", "quantityRequired": 10.0},
		{"componentType": "FIXED", "finishedGoodId": "OIL", "quantityRequired": 2.0},
	}
	stock := map[string]float64{
		"BUNS": 220, "LETTUCE": 7, "TOMATOES": 31, "PATTIES": 22500,
		"SAUCE": 1500, "MARINATION": 2000, "OIL": 9907.5,
	}
	r := ComputeMenuAvailability(lines, stock)
	if r.MaxPortions != 7 {
		t.Fatalf("SINGLE PATTY expected 7 (lettuce), got %d", r.MaxPortions)
	}
}

func TestSST_SinglePattyBacon(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "BUNS", "quantityRequired": 1.0},
		{"componentType": "FIXED", "finishedGoodId": "LETTUCE", "quantityRequired": 1.0},
		{"componentType": "FIXED", "finishedGoodId": "TOMATOES", "quantityRequired": 0.25},
		{"componentType": "FIXED", "finishedGoodId": "PATTIES", "quantityRequired": 1.0},
		{"componentType": "FIXED", "finishedGoodId": "SAUCE", "quantityRequired": 45.0},
		{"componentType": "FIXED", "finishedGoodId": "MARINATION", "quantityRequired": 10.0},
		{"componentType": "FIXED", "finishedGoodId": "BACON", "quantityRequired": 30.0},
		{"componentType": "FIXED", "finishedGoodId": "OIL", "quantityRequired": 2.0},
	}
	stock := map[string]float64{
		"BUNS": 220, "LETTUCE": 7, "TOMATOES": 31, "PATTIES": 22500,
		"SAUCE": 1500, "MARINATION": 2000, "BACON": 82.5, "OIL": 9907.5,
	}
	r := ComputeMenuAvailability(lines, stock)
	if r.MaxPortions != 2 {
		t.Fatalf("SINGLE PATTY BACON expected 2, got %d", r.MaxPortions)
	}
}

func TestSST_SinglePattyCheese(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "LETTUCE", "quantityRequired": 1.0},
		{"componentType": "FIXED", "finishedGoodId": "CHEDDAR", "quantityRequired": 43.48},
	}
	stock := map[string]float64{"LETTUCE": 7, "CHEDDAR": 6.5}
	r := ComputeMenuAvailability(lines, stock)
	if r.MaxPortions != 0 {
		t.Fatalf("SINGLE PATTY CHEESE expected 0, got %d", r.MaxPortions)
	}
}

func TestSST_SkipLineWithoutSKU(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "quantityRequired": 1.0}, // no SKU — Nest skips
		{"componentType": "FIXED", "finishedGoodId": "LETTUCE", "quantityRequired": 1.0},
	}
	r := ComputeMenuAvailability(lines, map[string]float64{"LETTUCE": 7})
	if r.MaxPortions != 7 {
		t.Fatalf("expected 7, got %d", r.MaxPortions)
	}
}

func TestSST_Fries(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "POTATOES", "quantityRequired": 430.0},
		{"componentType": "FIXED", "finishedGoodId": "SALT", "quantityRequired": 5.0},
	}
	stock := map[string]float64{"POTATOES": 180180, "SALT": 1454.67}
	r := ComputeMenuAvailability(lines, stock)
	// floor(180180/430)=419; floor(1454.67/5)=290 → min 290
	if r.MaxPortions != 290 {
		t.Fatalf("SEASONED FRIES expected 290, got %d", r.MaxPortions)
	}
}
