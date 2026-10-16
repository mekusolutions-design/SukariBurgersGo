package projections

import (
	"testing"

	"github.com/restflow/api-go/internal/eventstore"
)

func TestAppliesSaleSideEffects(t *testing.T) {
	if !AppliesSaleSideEffects(eventstore.EventPosSale) {
		t.Fatal("pos_sale must apply")
	}
	for _, et := range []string{
		eventstore.EventOrderSentToKitchen,
		eventstore.EventOrderStatusUpdated,
		eventstore.EventOrderPaymentUpdated,
		"kitchen_status",
		"",
	} {
		if AppliesSaleSideEffects(et) {
			t.Fatalf("%q must not apply sale side effects", et)
		}
	}
}
