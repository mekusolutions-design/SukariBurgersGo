package inventory

import (
	"testing"
	"time"
)

func TestPastExpiryPrefersNextExpiryDate(t *testing.T) {
	past := time.Now().UTC().Add(-48 * time.Hour)
	stalePositive := 8 // stale DB column saying 8 days left
	row := StockRow{
		AvailableStock:  0,
		DaysToExpiryMin: &stalePositive,
		NextExpiryDate:  &past,
	}
	if !isPastExpiry(row) {
		t.Fatal("expected past expiry from next_expiry_date")
	}
	if !isWrittenOffExpired(row) {
		t.Fatal("zero stock + past expiry must be written-off")
	}
	if matchesStatus(row, "out-of-stock") {
		t.Fatal("written-off must not match out-of-stock")
	}
}

func TestOutOfStockNotPastExpiry(t *testing.T) {
	future := time.Now().UTC().Add(8 * 24 * time.Hour)
	row := StockRow{AvailableStock: 0, NextExpiryDate: &future}
	if isWrittenOffExpired(row) {
		t.Fatal("future expiry zero stock is out-of-stock not written-off")
	}
	if !matchesStatus(row, "out-of-stock") {
		t.Fatal("expected out-of-stock")
	}
}

func TestInStockWithReorder(t *testing.T) {
	row := StockRow{AvailableStock: 100, ReorderPoint: 10}
	if !matchesStatus(row, "in-stock") {
		t.Fatal("expected in-stock")
	}
	if matchesStatus(row, "low-stock") {
		t.Fatal("not low")
	}
}
