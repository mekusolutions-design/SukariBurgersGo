package costing

import (
	"github.com/mekusolutions-design/SukariBurgersGo/pkg/money"
	"github.com/shopspring/decimal"
)

// WeightedAverageUnitCostDecimal implements RestFlow WAC at 6 decimal places.
// newAvg = (qtyOnHand * currentAvg + qtyReceived * receiptUnitCost) / (qtyOnHand + qtyReceived)
func WeightedAverageUnitCostDecimal(
	qtyOnHand, qtyReceived decimal.Decimal,
	currentAvg, receiptUnitCost money.Money,
) money.Money {
	if !qtyReceived.IsPositive() {
		return currentAvg.AsUnitCost()
	}
	if !qtyOnHand.IsPositive() {
		return receiptUnitCost.AsUnitCost()
	}
	den := qtyOnHand.Add(qtyReceived)
	if den.IsZero() {
		return receiptUnitCost.AsUnitCost()
	}
	left := qtyOnHand.Mul(currentAvg.Decimal())
	right := qtyReceived.Mul(receiptUnitCost.Decimal())
	avg := left.Add(right).Div(den)
	return money.FromDecimalUnitCost(avg)
}

// WeightedAverageUnitCost is a compatibility wrapper. Prefer WeightedAverageUnitCostDecimal.
func WeightedAverageUnitCost(qtyOnHand, currentAvg, qtyReceived, receiptUnitCost float64) float64 {
	avg := WeightedAverageUnitCostDecimal(
		decimal.NewFromFloat(qtyOnHand),
		decimal.NewFromFloat(qtyReceived),
		money.FromFloatUnitCost(currentAvg),
		money.FromFloatUnitCost(receiptUnitCost),
	)
	return avg.Float64()
}

// LineTotal = unitCost * qty (4dp total).
func LineTotal(unitCost money.Money, qty float64) money.Money {
	return unitCost.MulQty(qty).AsTotal()
}

// UnitFromTotal = total / qty (6dp unit cost).
func UnitFromTotal(total money.Money, qty float64) (money.Money, error) {
	return total.DivQty(qty)
}

// FoodCostPercent = cogs / revenue * 100 (1dp display typical; store 4dp).
func FoodCostPercent(cogs, revenue money.Money) *float64 {
	if !revenue.IsPositive() {
		return nil
	}
	pct, err := cogs.Mul(decimal.NewFromInt(100)).Div(revenue.Decimal())
	if err != nil {
		return nil
	}
	v := pct.AsTotal().Float64()
	return &v
}

// RoundMoney keeps legacy callers compiling; uses 4dp decimal.
func RoundMoney(f float64) float64 {
	return money.FromFloat(f).Float64()
}
