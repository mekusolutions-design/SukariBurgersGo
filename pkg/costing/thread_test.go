package costing

import "testing"

func TestFinishCostMath_Experimental(t *testing.T) {
	tm, uc, stock, wv, un, ok := FinishCostMath(18, 2, 96.6, 20)
	if tm != 20 {
		t.Fatalf("totalMade %v", tm)
	}
	if uc < 4.82 || uc > 4.84 {
		t.Fatalf("unitCost %v want ~4.83", uc)
	}
	if stock < 86.9 || stock > 87.0 {
		t.Fatalf("toStock %v", stock)
	}
	if wv < 9.65 || wv > 9.67 {
		t.Fatalf("wasteValue %v", wv)
	}
	if un != 0 || !ok {
		t.Fatalf("un=%v ok=%v", un, ok)
	}
}

func TestFinishCostMath_NoDoubleSubtract(t *testing.T) {
	// good is already net to stock — never good-waste again
	good := 18.0
	usable := good // never good-waste again
	if usable != 18 {
		t.Fatal(usable)
	}
	_, _, _, _, un, _ := FinishCostMath(18, 0, 96.6, 20)
	if un != 2 {
		t.Fatalf("unaccounted %v", un)
	}
}

func TestResolveProductionCost_MislabeledBatchTotal(t *testing.T) {
	c := ResolveProductionCost(96.6, 0, 96.6, 20)
	if c < 96.5 || c > 96.7 {
		t.Fatalf("got %v", c)
	}
}
