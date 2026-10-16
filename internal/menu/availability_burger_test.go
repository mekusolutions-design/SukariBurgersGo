package menu

import "testing"

// Mirrors the ops bug report: POS used to show lettuce (7) for every burger while
// Menu correctly took min(lettuce, bacon, cheddar).
func TestBurgerAvailabilityMinOverIngredients(t *testing.T) {
	// Single Patty Cheese: lettuce 7/1, cheddar 6.5/43.48 → 0
	linesCheese := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "LETTUCE", "quantityRequired": 1.0},
		{"componentType": "FIXED", "finishedGoodId": "CHEDDAR", "quantityRequired": 43.48},
	}
	stock := map[string]float64{"LETTUCE": 7, "BACON": 82.5, "CHEDDAR": 6.5}
	r := ComputeMenuAvailability(linesCheese, stock)
	if r.MaxPortions != 0 {
		t.Fatalf("cheese burger expected 0 (86'd), got %d", r.MaxPortions)
	}

	// Single Patty Bacon: lettuce 7, bacon 82.5/30 → 2
	linesBacon := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "LETTUCE", "quantityRequired": 1.0},
		{"componentType": "FIXED", "finishedGoodId": "BACON", "quantityRequired": 30.0},
	}
	r2 := ComputeMenuAvailability(linesBacon, stock)
	if r2.MaxPortions != 2 {
		t.Fatalf("bacon burger expected 2, got %d", r2.MaxPortions)
	}

	// Single Patty: lettuce only → 7
	linesPlain := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "LETTUCE", "quantityRequired": 1.0},
	}
	r3 := ComputeMenuAvailability(linesPlain, stock)
	if r3.MaxPortions != 7 {
		t.Fatalf("plain expected 7, got %d", r3.MaxPortions)
	}
}

func TestDoublePattyDoubleBaconPortions(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "LETTUCE", "quantityRequired": 1.0},
		{"componentType": "FIXED", "finishedGoodId": "BACON", "quantityRequired": 60.0},
	}
	stock := map[string]float64{"LETTUCE": 7, "BACON": 82.5}
	r := ComputeMenuAvailability(lines, stock)
	if r.MaxPortions != 1 {
		t.Fatalf("double bacon expected 1, got %d", r.MaxPortions)
	}
}
