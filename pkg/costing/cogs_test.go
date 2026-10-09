package costing

import "testing"

func TestBatchUnitCost(t *testing.T) {
	// Ingredient A: 2 * 100 = 200; B: 3 * 50 = 150; batch = 350; yield 10 → 35
	batch := 2.0*100 + 3.0*50
	yield := 10.0
	unit := RoundMoney(batch / yield)
	if unit != 35.0 {
		t.Fatalf("got %v want 35", unit)
	}
}

func TestFoodCostPct(t *testing.T) {
	cogs, rev := 350.0, 1000.0
	pct := RoundMoney(cogs / rev * 100)
	if pct != 35.0 {
		t.Fatalf("got %v", pct)
	}
}
