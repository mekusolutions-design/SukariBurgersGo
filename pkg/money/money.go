// Package money provides safe monetary arithmetic for RestFlow.
// Totals use 4 decimal places (PostgreSQL Decimal(18,4)).
// Unit costs / WAC use 6 decimal places (Decimal(18,6)).
// Never use float64 for intermediate financial math.
package money

import (
	"database/sql/driver"
	"fmt"
	"math/big"

	"github.com/shopspring/decimal"
)

const (
	Scale         int32 = 4 // line totals, revenue, waste value
	UnitCostScale int32 = 6 // avg_unit_cost, receipt unit cost
)

// Money is an immutable monetary amount (default scale 4).
type Money struct {
	d decimal.Decimal
}

func Zero() Money { return Money{d: decimal.Zero} }

func New(amount string) (Money, error) {
	d, err := decimal.NewFromString(amount)
	if err != nil {
		return Zero(), fmt.Errorf("invalid money amount %q: %w", amount, err)
	}
	return Money{d: d.Round(Scale)}, nil
}

func NewUnitCost(amount string) (Money, error) {
	d, err := decimal.NewFromString(amount)
	if err != nil {
		return Zero(), fmt.Errorf("invalid unit cost %q: %w", amount, err)
	}
	return Money{d: d.Round(UnitCostScale)}, nil
}

// FromStringPrefer parses without forcing scale until Round* is called.
func FromStringPrefer(amount string, scale int32) (Money, error) {
	d, err := decimal.NewFromString(amount)
	if err != nil {
		return Zero(), err
	}
	return Money{d: d.Round(scale)}, nil
}

// FromFloat is an edge adapter only (JSON numbers from clients). Prefer New().
func FromFloat(f float64) Money {
	return Money{d: decimal.NewFromFloat(f).Round(Scale)}
}

func FromFloatUnitCost(f float64) Money {
	return Money{d: decimal.NewFromFloat(f).Round(UnitCostScale)}
}

func FromDecimal(d decimal.Decimal) Money {
	return Money{d: d.Round(Scale)}
}

func FromDecimalUnitCost(d decimal.Decimal) Money {
	return Money{d: d.Round(UnitCostScale)}
}

func FromInt(units int64) Money {
	return Money{d: decimal.NewFromInt(units)}
}

// Qty returns a decimal quantity (not money) for multiply/divide.
func Qty(q float64) decimal.Decimal {
	return decimal.NewFromFloat(q)
}

func QtyString(s string) (decimal.Decimal, error) {
	return decimal.NewFromString(s)
}

func (m Money) Decimal() decimal.Decimal { return m.d }

func (m Money) String() string { return m.d.StringFixed(Scale) }

func (m Money) StringUnitCost() string { return m.d.StringFixed(UnitCostScale) }

// Float64 is edge-only (API numbers). Do not chain financial math on the result.
func (m Money) Float64() float64 {
	f, _ := m.d.Float64()
	return f
}

func (m Money) IsZero() bool     { return m.d.IsZero() }
func (m Money) IsNegative() bool { return m.d.IsNegative() }
func (m Money) IsPositive() bool { return m.d.IsPositive() }

func (m Money) Add(o Money) Money {
	return Money{d: m.d.Add(o.d).Round(Scale)}
}

func (m Money) Sub(o Money) Money {
	return Money{d: m.d.Sub(o.d).Round(Scale)}
}

func (m Money) Mul(factor decimal.Decimal) Money {
	return Money{d: m.d.Mul(factor).Round(Scale)}
}

func (m Money) MulQty(qty float64) Money {
	return Money{d: m.d.Mul(decimal.NewFromFloat(qty)).Round(Scale)}
}

func (m Money) MulMoney(o Money) Money {
	return Money{d: m.d.Mul(o.d).Round(Scale)}
}

func (m Money) Div(divisor decimal.Decimal) (Money, error) {
	if divisor.IsZero() {
		return Zero(), fmt.Errorf("division by zero")
	}
	return Money{d: m.d.Div(divisor).Round(Scale)}, nil
}

func (m Money) DivQty(qty float64) (Money, error) {
	if qty == 0 {
		return Zero(), fmt.Errorf("division by zero")
	}
	return Money{d: m.d.Div(decimal.NewFromFloat(qty)).Round(UnitCostScale)}, nil
}

func (m Money) Round(scale int32) Money {
	return Money{d: m.d.Round(scale)}
}

func (m Money) AsUnitCost() Money {
	return Money{d: m.d.Round(UnitCostScale)}
}

func (m Money) AsTotal() Money {
	return Money{d: m.d.Round(Scale)}
}

func (m Money) Cmp(o Money) int          { return m.d.Cmp(o.d) }
func (m Money) Equal(o Money) bool       { return m.d.Equal(o.d) }
func (m Money) GreaterThan(o Money) bool { return m.d.GreaterThan(o.d) }
func (m Money) LessThan(o Money) bool    { return m.d.LessThan(o.d) }
func (m Money) Abs() Money               { return Money{d: m.d.Abs()} }
func (m Money) Negate() Money            { return Money{d: m.d.Neg()} }

func (m *Money) Scan(src interface{}) error {
	if src == nil {
		m.d = decimal.Zero
		return nil
	}
	switch v := src.(type) {
	case string:
		d, err := decimal.NewFromString(v)
		if err != nil {
			return err
		}
		m.d = d
		return nil
	case []byte:
		d, err := decimal.NewFromString(string(v))
		if err != nil {
			return err
		}
		m.d = d
		return nil
	case float64:
		m.d = decimal.NewFromFloat(v)
		return nil
	case int64:
		m.d = decimal.NewFromInt(v)
		return nil
	case *big.Rat:
		m.d = decimal.NewFromBigRat(v, UnitCostScale)
		return nil
	default:
		return fmt.Errorf("cannot scan %T into Money", src)
	}
}

func (m Money) Value() (driver.Value, error) {
	return m.d.StringFixed(Scale), nil
}

func (m Money) ValueUnitCost() (driver.Value, error) {
	return m.d.StringFixed(UnitCostScale), nil
}

func (m Money) MarshalJSON() ([]byte, error) {
	return []byte(`"` + m.String() + `"`), nil
}

func (m *Money) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		m.d = decimal.Zero
		return nil
	}
	if len(data) >= 2 && data[0] == '"' {
		s := string(data[1 : len(data)-1])
		d, err := decimal.NewFromString(s)
		if err != nil {
			return err
		}
		m.d = d.Round(Scale)
		return nil
	}
	d, err := decimal.NewFromString(string(data))
	if err != nil {
		return err
	}
	m.d = d.Round(Scale)
	return nil
}

// ParseFlexible accepts string or numeric-looking values from handlers.
func ParseFlexible(v interface{}) Money {
	switch x := v.(type) {
	case nil:
		return Zero()
	case Money:
		return x
	case *Money:
		if x == nil {
			return Zero()
		}
		return *x
	case string:
		if x == "" {
			return Zero()
		}
		m, err := New(x)
		if err != nil {
			return FromFloat(0)
		}
		return m
	case float64:
		return FromFloat(x)
	case float32:
		return FromFloat(float64(x))
	case int:
		return FromInt(int64(x))
	case int64:
		return FromInt(x)
	default:
		return Zero()
	}
}

// ToAPINumber returns a float64 rounded to 4dp for Nest-compatible JSON numbers.
func ToAPINumber(m Money) float64 {
	return m.AsTotal().Float64()
}
