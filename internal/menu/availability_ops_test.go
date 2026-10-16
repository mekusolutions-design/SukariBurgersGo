package menu

import "testing"

// Ops brief 16 Oct 2026 — POS must match Menu page min-across-ingredients.

func TestOps_SinglePatty_LettuceOnly(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "LETTUCE", "quantityRequired": 1.0},
	}
	stock := map[string]float64{"LETTUCE": 7}
	r := ComputeMenuAvailability(lines, stock, nil)
	if r.MaxPortions != 7 {
		t.Fatalf("want 7, got %d", r.MaxPortions)
	}
}

func TestOps_SinglePattyBacon(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "LETTUCE", "quantityRequired": 1.0},
		{"componentType": "FIXED", "finishedGoodId": "BACON", "quantityRequired": 30.0},
	}
	stock := map[string]float64{"LETTUCE": 7, "BACON": 82.5}
	r := ComputeMenuAvailability(lines, stock, nil)
	// bacon 82.5/30 = 2
	if r.MaxPortions != 2 {
		t.Fatalf("want 2, got %d breakdown=%+v", r.MaxPortions, r.Lines)
	}
}

func TestOps_SinglePattyCheese_CheddarExhausted(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "LETTUCE", "quantityRequired": 1.0},
		{"componentType": "FIXED", "finishedGoodId": "CHEDDAR", "quantityRequired": 43.48},
	}
	stock := map[string]float64{"LETTUCE": 7, "CHEDDAR": 6.5}
	r := ComputeMenuAvailability(lines, stock, nil)
	if r.MaxPortions != 0 {
		t.Fatalf("want 0, got %d", r.MaxPortions)
	}
	if r.IsAvailable {
		t.Fatal("should not be available")
	}
}

func TestOps_DoublePattyDoubleBacon(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "LETTUCE", "quantityRequired": 1.0},
		{"componentType": "FIXED", "finishedGoodId": "BACON", "quantityRequired": 60.0},
	}
	stock := map[string]float64{"LETTUCE": 7, "BACON": 82.5}
	r := ComputeMenuAvailability(lines, stock, nil)
	// 82.5/60 = 1
	if r.MaxPortions != 1 {
		t.Fatalf("want 1, got %d", r.MaxPortions)
	}
}

func TestOps_DoublePattyDoubleCheese(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "LETTUCE", "quantityRequired": 1.0},
		{"componentType": "FIXED", "finishedGoodId": "CHEDDAR", "quantityRequired": 86.96},
	}
	stock := map[string]float64{"LETTUCE": 7, "CHEDDAR": 6.5}
	r := ComputeMenuAvailability(lines, stock, nil)
	if r.MaxPortions != 0 {
		t.Fatalf("want 0, got %d", r.MaxPortions)
	}
}

func TestOps_StandaloneBaconDoesNotInflateBurger(t *testing.T) {
	// Burger uses BACON-G (grams). Standalone sellable BACON-PCS has huge stock — must not affect burger.
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "BACON-G", "quantityRequired": 30.0},
		{"componentType": "FIXED", "finishedGoodId": "LETTUCE", "quantityRequired": 1.0},
	}
	stock := map[string]float64{
		"BACON-G":   82.5,
		"LETTUCE":   7,
		"BACON-PCS": 1425, // standalone menu chip — wrong SKU
	}
	r := ComputeMenuAvailability(lines, stock, nil)
	if r.MaxPortions != 2 {
		t.Fatalf("want 2 (from BACON-G), got %d", r.MaxPortions)
	}
}

func TestOps_SodaFixedByStockItemID(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "SODA-300", "quantityRequired": 1.0},
	}
	stock := map[string]float64{"SODA-300": 34}
	r := ComputeMenuAvailability(lines, stock, nil)
	if r.MaxPortions != 34 {
		t.Fatalf("want 34, got %d", r.MaxPortions)
	}
}

func TestHasStockConstraints_BlocksNameFallback(t *testing.T) {
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "LETTUCE", "quantityRequired": 1.0},
	}
	if !hasStockConstraints(lines) {
		t.Fatal("expected constraints")
	}
	if hasStockConstraints(nil) {
		t.Fatal("empty should be false")
	}
}

func TestOps_RecipeConstraints_MinAcrossIngredients(t *testing.T) {
	// Simulates enrich output: base lines + recipe ingredients
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "LETTUCE", "quantityRequired": 1.0},
		{"componentType": "FIXED", "finishedGoodId": "BACON", "quantityRequired": 30.0},
		{"componentType": "FIXED", "finishedGoodId": "CHEDDAR", "quantityRequired": 43.48},
	}
	stock := map[string]float64{"LETTUCE": 7, "BACON": 82.5, "CHEDDAR": 6.5}
	r := ComputeMenuAvailability(lines, stock, nil)
	if r.MaxPortions != 0 {
		t.Fatalf("cheese-limited want 0, got %d", r.MaxPortions)
	}
}
