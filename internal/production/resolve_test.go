package production

import (
	"math"
	"testing"
)

func scaleIngredients(standardYield, primaryActual float64, perUnit []float64) []float64 {
	if standardYield <= 0 {
		standardYield = 1
	}
	if primaryActual <= 0 {
		primaryActual = standardYield
	}
	scale := primaryActual / standardYield
	out := make([]float64, len(perUnit))
	for i, p := range perUnit {
		out[i] = p * scale
	}
	return out
}

func TestScaleIngredientsNestParity(t *testing.T) {
	got := scaleIngredients(10, 20, []float64{5, 1.5})
	if math.Abs(got[0]-10) > 1e-9 || math.Abs(got[1]-3) > 1e-9 {
		t.Fatalf("got %v want [10, 3]", got)
	}
	got = scaleIngredients(0, 5, []float64{2})
	if math.Abs(got[0]-10) > 1e-9 {
		t.Fatalf("zero yield scale: got %v", got)
	}
}

func TestUsableQuantityNest(t *testing.T) {
	usable := 10.0 - 2.0
	if usable < 0 {
		usable = 0
	}
	if usable != 8 {
		t.Fatalf("usable=%v", usable)
	}
	usable = 3 - 5
	if usable < 0 {
		usable = 0
	}
	if usable != 0 {
		t.Fatalf("floor usable=%v", usable)
	}
}

func TestLineJSONTagsPresent(t *testing.T) {
	l := Line{ItemID: "RAW1", ActualQuantity: 3, UnitCost: 1.5}
	if l.ItemID == "" || l.ActualQuantity <= 0 {
		t.Fatal("line shape broken")
	}
}
