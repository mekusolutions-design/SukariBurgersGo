package production

import "testing"

func TestActualYieldMassGrams(t *testing.T) {
	// 3 pieces × 400 g = 1200 g (not 3)
	outs := []Line{
		{ItemID: "BALL", ActualQuantity: 3, UnitWeight: 400, WeightUnit: "g"},
	}
	g := actualYieldMassGrams(outs)
	if g != 1200 {
		t.Fatalf("mass grams: got %v want 1200", g)
	}
	header, _, fromMass := normalizeHeaderYield(FinishInput{
		PlannedQty: 1200,
		Outputs:    outs,
		ActualQty:  3, // wrong client piece sum
	})
	if !fromMass {
		t.Fatal("expected fromMass")
	}
	if header != 1200 {
		t.Fatalf("header yield: got %v want 1200 (not piece count 3)", header)
	}
}

func TestPieceOnlyNoMass(t *testing.T) {
	outs := []Line{{ItemID: "BOX", ActualQuantity: 5}}
	g := actualYieldMassGrams(outs)
	if g != 0 {
		t.Fatalf("expected 0 mass, got %v", g)
	}
	header, _, fromMass := normalizeHeaderYield(FinishInput{
		ActualQty: 5,
		Outputs:   outs,
	})
	if fromMass {
		t.Fatal("should not use mass")
	}
	if header != 5 {
		t.Fatalf("got %v", header)
	}
}
