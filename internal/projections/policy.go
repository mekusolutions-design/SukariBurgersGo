package projections

import "github.com/restflow/api-go/internal/eventstore"

// AppliesSaleSideEffects reports whether an event type may mutate inventory or sales.
// Kitchen/status/payment events must never re-apply POS financial effects.
func AppliesSaleSideEffects(eventType string) bool {
	return eventType == eventstore.EventPosSale
}
