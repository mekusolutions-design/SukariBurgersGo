package unit_test

import (
	"testing"

	"github.com/mekusolutions-design/SukariBurgersGo/pkg/money"
)

func TestMoneyRoundTrip(t *testing.T) {
	m, err := money.New("19.9999")
	if err != nil {
		t.Fatal(err)
	}
	if m.String() != "19.9999" {
		t.Fatalf("got %s", m.String())
	}
	sum := m.Add(money.FromFloat(0.0001))
	if sum.String() != "20.0000" {
		t.Fatalf("sum %s", sum.String())
	}
}
