package unit_test

import (
	"math"
	"testing"

	"github.com/restflow/api-go/internal/units"
)

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

func TestToCanonicalStockQty_KG(t *testing.T) {
	n := units.ToCanonicalStockQty(0.5, "kg", 400)
	if n.Quantity != 500 || n.Unit != "g" || !almostEqual(n.UnitCost, 0.4) {
		t.Fatalf("got %+v", n)
	}
	if !almostEqual(n.Quantity*n.UnitCost, 200) {
		t.Fatalf("line value drift: %v", n.Quantity*n.UnitCost)
	}
	if !n.Converted {
		t.Fatal("expected Converted")
	}
}

func TestToCanonicalStockQty_L(t *testing.T) {
	n := units.ToCanonicalStockQty(2, "L", 100)
	if n.Quantity != 2000 || n.Unit != "ml" || !almostEqual(n.UnitCost, 0.1) {
		t.Fatalf("got %+v", n)
	}
	if !almostEqual(n.Quantity*n.UnitCost, 200) {
		t.Fatalf("line value drift")
	}
}

func TestToCanonicalStockQty_PCS(t *testing.T) {
	n := units.ToCanonicalStockQty(3, "pcs", 10)
	if n.Quantity != 3 || n.Unit != "pcs" || n.UnitCost != 10 || n.Converted {
		t.Fatalf("got %+v", n)
	}
}

func TestQuantityInItemUnit(t *testing.T) {
	if !almostEqual(units.QuantityInItemUnit(1, "kg", "g"), 1000) {
		t.Fatal("1 kg → g")
	}
	if !almostEqual(units.QuantityInItemUnit(500, "g", "kg"), 0.5) {
		t.Fatal("500 g → kg")
	}
	if !almostEqual(units.QuantityInItemUnit(30, "g", "g"), 30) {
		t.Fatal("same unit")
	}
}

func TestScaleFactorForUnitChange(t *testing.T) {
	if units.ScaleFactorForUnitChange("kg", "g") != 1000 {
		t.Fatal("kg→g")
	}
	if units.ScaleFactorForUnitChange("g", "kg") != 0.001 {
		t.Fatal("g→kg")
	}
	if units.ScaleFactorForUnitChange("l", "ml") != 1000 {
		t.Fatal("l→ml")
	}
	if units.ScaleFactorForUnitChange("pcs", "pcs") != 1 {
		t.Fatal("pcs")
	}
}

func TestDedupeMenuLines(t *testing.T) {
	lines := []units.MenuLine{
		{ComponentKey: "c1", FinishedGoodID: "WATER", QuantityRequired: 1},
		{ComponentKey: "c2", FinishedGoodID: "COLA", QuantityRequired: 1},
		{ComponentKey: "c3", FinishedGoodID: "WATER", QuantityRequired: 1},
		{ComponentKey: "c4", FinishedGoodID: "COLA", QuantityRequired: 1},
	}
	out := units.DedupeMenuLines(lines)
	if len(out) != 2 {
		t.Fatalf("len %d", len(out))
	}
	if out[0].FinishedGoodID != "WATER" || out[1].FinishedGoodID != "COLA" {
		t.Fatalf("%+v", out)
	}
}

func TestCanonicalStorageUnit(t *testing.T) {
	if units.CanonicalStorageUnit("kilograms") != "g" {
		t.Fatal()
	}
	if units.CanonicalStorageUnit("litre") != "ml" {
		t.Fatal()
	}
	if units.CanonicalStorageUnit("pc") != "pcs" {
		t.Fatal()
	}
}
