package fefo

import (
	"sort"
	"testing"
	"time"
)

func TestFEFOSortOrder(t *testing.T) {
	type lot struct {
		id  string
		exp *time.Time
	}
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	lots := []lot{
		{id: "null", exp: nil},
		{id: "late", exp: &t2},
		{id: "early", exp: &t1},
	}
	sort.SliceStable(lots, func(i, j int) bool {
		a, b := lots[i].exp, lots[j].exp
		if a == nil && b == nil {
			return false
		}
		if a == nil {
			return false
		}
		if b == nil {
			return true
		}
		return a.Before(*b)
	})
	if lots[0].id != "early" || lots[1].id != "late" || lots[2].id != "null" {
		t.Fatalf("order=%v %v %v", lots[0].id, lots[1].id, lots[2].id)
	}
}
