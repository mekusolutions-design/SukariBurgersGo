package unit_test

import (
	"testing"

	"github.com/restflow/api-go/internal/eventstore"
)

func TestPosSaleIsOnlyFinancialEvent(t *testing.T) {
	financial := eventstore.EventPosSale
	nonFinancial := []string{
		eventstore.EventOrderSentToKitchen,
		eventstore.EventOrderStatusUpdated,
		eventstore.EventOrderPaymentUpdated,
	}
	for _, et := range nonFinancial {
		if et == financial {
			t.Fatalf("%s must not equal pos_sale", et)
		}
	}
}
