package trace

import "testing"

func TestEventTypeAliases(t *testing.T) {
	a := eventTypeAliases("waste")
	if len(a) == 0 {
		t.Fatal("expected aliases")
	}
	found := false
	for _, x := range a {
		if x == "waste_recorded" {
			found = true
		}
	}
	if !found {
		t.Fatalf("waste should map to waste_recorded: %v", a)
	}
}

func TestEventTypeMatches(t *testing.T) {
	if !eventTypeMatches("waste_recorded", "waste") {
		t.Fatal("waste_recorded should match waste")
	}
	if eventTypeMatches("pos_sale", "waste") {
		t.Fatal("pos_sale must not match waste")
	}
}

func TestBuildSummary(t *testing.T) {
	q := 2.0
	s := buildSummary("received", "Butter", &q, map[string]interface{}{})
	if s == "" || s[:8] != "Received" {
		t.Fatalf("got %q", s)
	}
}
