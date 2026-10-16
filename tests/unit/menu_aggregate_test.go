package unit_test

import (
	"testing"

	"github.com/restflow/api-go/internal/menu"
)

func TestAggregateDeductions(t *testing.T) {
	rows := []menu.StockDeduction{
		{ItemID: "A", ItemName: "A", Quantity: 1, UnitCost: 10},
		{ItemID: "A", ItemName: "A", Quantity: 2, UnitCost: 0},
		{ItemID: "B", ItemName: "B", Quantity: 1, UnitCost: 5},
	}
	out := menu.AggregateDeductions(rows)
	if len(out) != 2 {
		t.Fatalf("expected 2, got %d", len(out))
	}
	for _, d := range out {
		if d.ItemID == "A" && d.Quantity != 3 {
			t.Fatalf("A qty=%v", d.Quantity)
		}
		if d.ItemID == "A" && d.UnitCost != 10 {
			t.Fatalf("A cost=%v", d.UnitCost)
		}
	}
}
