package money

import "testing"

func TestNewAndArithmetic(t *testing.T) {
	a, err := New("10.12345")
	if err != nil {
		t.Fatal(err)
	}
	if a.String() != "10.1235" {
		t.Fatalf("got %s", a.String())
	}
	b := FromFloat(2.5)
	sum := a.Add(b)
	if sum.String() != "12.6235" {
		t.Fatalf("sum %s", sum.String())
	}
	prod := a.MulQty(2)
	if prod.String() != "20.2470" {
		t.Fatalf("prod %s", prod.String())
	}
}

func TestUnitCostScale(t *testing.T) {
	u, err := NewUnitCost("1.1234567")
	if err != nil {
		t.Fatal(err)
	}
	if u.StringUnitCost() != "1.123457" {
		t.Fatalf("got %s", u.StringUnitCost())
	}
}

func TestZeroAndCompare(t *testing.T) {
	z := Zero()
	if !z.IsZero() {
		t.Fatal("expected zero")
	}
	a := FromFloat(1)
	if !a.GreaterThan(z) {
		t.Fatal("1 should be > 0")
	}
}

func TestParseFlexible(t *testing.T) {
	m := ParseFlexible("12.50")
	if m.String() != "12.5000" {
		t.Fatalf("got %s", m.String())
	}
	m2 := ParseFlexible(3.25)
	if m2.String() != "3.2500" {
		t.Fatalf("got %s", m2.String())
	}
}
