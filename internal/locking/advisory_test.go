package locking

import "testing"

func TestHashStringToIntStable(t *testing.T) {
	a := hashStringToInt("shop-1")
	b := hashStringToInt("shop-1")
	if a != b {
		t.Fatal("hash must be stable")
	}
	if a == hashStringToInt("shop-2") {
		t.Fatal("different shops should differ (usually)")
	}
}
