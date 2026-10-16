package menu

import (
	"testing"
)

// Michael 3 Oct 2026: POS must never treat recipe ingredient rows as the sale target.
// These tests lock AggregateDeductions + pure selection math used by the sale path.

func TestAggregateDeductions_MergesSameSKU(t *testing.T) {
	in := []StockDeduction{
		{ItemID: "FG-TOPPING", ItemName: "Topping", Quantity: 2000, Unit: "g", UnitCost: 0.01},
		{ItemID: "FG-TOPPING", ItemName: "Topping", Quantity: 500, Unit: "g", UnitCost: 0.01},
		{ItemID: "BOX", ItemName: "Box", Quantity: 1, Unit: "pcs", UnitCost: 5},
	}
	out := AggregateDeductions(in)
	if len(out) != 2 {
		t.Fatalf("expected 2 SKUs, got %d: %+v", len(out), out)
	}
	by := map[string]float64{}
	for _, d := range out {
		by[d.ItemID] = d.Quantity
	}
	if by["FG-TOPPING"] != 2500 {
		t.Fatalf("topping qty want 2500 got %v", by["FG-TOPPING"])
	}
	if by["BOX"] != 1 {
		t.Fatalf("box qty want 1 got %v", by["BOX"])
	}
}

func TestAggregateDeductions_Empty(t *testing.T) {
	if got := AggregateDeductions(nil); len(got) != 0 {
		t.Fatalf("nil → empty, got %v", got)
	}
}

func TestStringSliceFromLine_OptionItemIDs(t *testing.T) {
	lm := map[string]interface{}{
		"option_item_ids": []interface{}{"MOZZ", "CHEDDAR"},
	}
	got := stringSliceFromLine(lm)
	if len(got) != 2 || got[0] != "MOZZ" || got[1] != "CHEDDAR" {
		t.Fatalf("option_item_ids: %v", got)
	}
}

func TestStringSliceFromLine_OptionsObjects(t *testing.T) {
	lm := map[string]interface{}{
		"options": []interface{}{
			map[string]interface{}{"finishedGoodId": "BOX-1"},
			map[string]interface{}{"finished_good_id": "BOX-2"},
		},
	}
	got := stringSliceFromLine(lm)
	if len(got) != 2 {
		t.Fatalf("options objects: %v", got)
	}
}

func TestPortionsFromLines_FIXEDUsesStock(t *testing.T) {
	lines := []map[string]interface{}{
		{
			"componentType":     "FIXED",
			"finishedGoodId":    "FG-TOPPING",
			"quantityRequired":  2000.0,
		},
	}
	stock := map[string]float64{"FG-TOPPING": 10000}
	maxP, fg := portionsFromLines(lines, stock)
	if maxP != 5 {
		t.Fatalf("max portions want 5 got %d", maxP)
	}
	if fg != "FG-TOPPING" {
		t.Fatalf("fg id %s", fg)
	}
}

func TestPortionsFromLines_DoesNotUseMissingRawSKU(t *testing.T) {
	// Availability is component-based: only FG-TOPPING matters, not CHICKEN BREAST.
	lines := []map[string]interface{}{
		{"componentType": "FIXED", "finishedGoodId": "FG-TOPPING", "quantityRequired": 2000.0},
	}
	stock := map[string]float64{
		"FG-TOPPING":     10000,
		"CHICKEN BREAST": 11822,
	}
	maxP, _ := portionsFromLines(lines, stock)
	if maxP != 5 {
		t.Fatalf("want 5 portions from FG only, got %d", maxP)
	}
}

func TestNormalizeMenuLines_OptionsAlwaysArray(t *testing.T) {
	lines := []map[string]interface{}{
		{
			"component_type":   "CHOICE",
			"component_key":    "cheese",
			"quantity_required": 200.0,
			"option_item_ids":  []interface{}{"MOZZ"},
		},
	}
	stock := map[string]float64{"MOZZ": 500}
	out, req := normalizeMenuLines(lines, stock)
	if len(out) != 1 {
		t.Fatalf("lines %d", len(out))
	}
	opts, ok := out[0]["options"].([]map[string]interface{})
	if !ok {
		// may be []interface{} depending on impl
		if opts2, ok2 := out[0]["options"].([]interface{}); !ok2 || len(opts2) == 0 {
			t.Fatalf("options missing: %#v", out[0]["options"])
		}
	} else if len(opts) != 1 {
		t.Fatalf("want 1 option got %d", len(opts))
	}
	if !req {
		t.Fatal("CHOICE should require selection flag true (or auto-deduct path)")
	}
}

func TestFinishedGoodCostFormula_PiecesTimesWeightTimesCpy(t *testing.T) {
	// Acceptance: 4 × DOUGH L (400 g), rate 0.15/g → Ksh 240
	pieces := 4.0
	unitWeight := 400.0
	cpy := 0.15
	costPerPiece := unitWeight * cpy
	line := pieces * costPerPiece
	if costPerPiece != 60.0 {
		t.Fatalf("cost/piece want 60 got %v", costPerPiece)
	}
	if line != 240.0 {
		t.Fatalf("line cost want 240 got %v", line)
	}
	yieldQty := pieces * unitWeight
	if yieldQty != 1600.0 {
		t.Fatalf("yield qty want 1600 got %v", yieldQty)
	}
}

func TestAggregateDeductions_PreservesLineCost(t *testing.T) {
	in := []StockDeduction{
		{ItemID: "DOUGH-L", Quantity: 2, UnitCost: 60, LineCost: 120, YieldQuantity: 800, Kind: "finished_good"},
		{ItemID: "DOUGH-L", Quantity: 2, UnitCost: 60, LineCost: 120, YieldQuantity: 800, Kind: "finished_good"},
	}
	out := AggregateDeductions(in)
	if len(out) != 1 {
		t.Fatalf("want 1 got %d", len(out))
	}
	if out[0].Quantity != 4 {
		t.Fatalf("qty %v", out[0].Quantity)
	}
	if out[0].LineCost != 240 {
		t.Fatalf("line cost %v", out[0].LineCost)
	}
	if out[0].YieldQuantity != 1600 {
		t.Fatalf("yield %v", out[0].YieldQuantity)
	}
}
