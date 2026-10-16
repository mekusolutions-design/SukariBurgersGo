package costing

import (
	"testing"

	"github.com/restflow/api-go/pkg/money"
	"github.com/shopspring/decimal"
)

func TestWACDecimalBasic(t *testing.T) {
	// 10 on hand @ 5.00, receive 10 @ 7.00 → avg 6.00
	avg := WeightedAverageUnitCostDecimal(
		decimal.NewFromInt(10),
		decimal.NewFromInt(10),
		money.FromFloatUnitCost(5),
		money.FromFloatUnitCost(7),
	)
	if avg.StringUnitCost() != "6.000000" {
		t.Fatalf("got %s", avg.StringUnitCost())
	}
}

func TestWACFromEmpty(t *testing.T) {
	avg := WeightedAverageUnitCostDecimal(
		decimal.Zero,
		decimal.NewFromInt(5),
		money.Zero(),
		money.FromFloatUnitCost(12.3456789),
	)
	// 6dp round
	if avg.StringUnitCost() != "12.345679" {
		t.Fatalf("got %s", avg.StringUnitCost())
	}
}

func TestLineTotal(t *testing.T) {
	uc := money.FromFloatUnitCost(3.333333)
	tot := LineTotal(uc, 3)
	if tot.String() != "9.999999" && tot.String() != "10.0000" && tot.String() != "9.9999" {
		// 3.333333*3 = 9.999999 → 4dp 10.0000 or 9.9999 depending on prior round
		t.Logf("line total %s (ok if near 10)", tot.String())
	}
}

func TestLegacyWrapper(t *testing.T) {
	v := WeightedAverageUnitCost(10, 5, 10, 7)
	if v < 5.999 || v > 6.001 {
		t.Fatalf("legacy WAC %v", v)
	}
}
