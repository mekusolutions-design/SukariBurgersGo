package menu

import "testing"

func TestNormalizeStockKey_Soda(t *testing.T) {
	a := normalizeStockKey("300 ml SODA")
	b := normalizeStockKey("300ml soda")
	if a != b || a != "300mlsoda" {
		t.Fatalf("got %q %q", a, b)
	}
}

func TestStockByNameLookup_PassionJuice(t *testing.T) {
	by := map[string]float64{}
	for _, k := range stockNameKeys("Passion juice", "PASSION-JUICE") {
		by[k] = 5000
	}
	got := stockByNameLookup(by, "PASSION JUICE", "MENU-PASSION")
	if got < 5000 {
		t.Fatalf("expected 5000, got %v keys=%v", got, stockNameKeys("Passion juice", ""))
	}
}
