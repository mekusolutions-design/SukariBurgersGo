package contract_test

import (
	"testing"
)

// Contract checklist — extend with httptest against running API.
// Critical paths the frontend depends on:
var requiredEndpoints = []struct {
	Method, Path string
}{
	{"POST", "/auth/login"},
	{"POST", "/auth/register"},
	{"GET", "/users/me"},
	{"GET", "/inventory"},
	{"GET", "/inventory/summary"},
	{"POST", "/received"},
	{"POST", "/pos/order"},
	{"GET", "/pos/orders"},
	{"POST", "/pos/orders/{orderId}/send-to-kitchen"},
	{"GET", "/kitchen/active-orders"},
	{"GET", "/recipe"},
	{"POST", "/waste/record"},
	{"POST", "/production/start"},
	{"POST", "/payment/mpesa"},
	{"GET", "/variance/batches"},
	{"GET", "/approvals/pending"},
	{"POST", "/sync/bulk"},
	{"GET", "/dashboard/summary"},
	{"GET", "/menu/catalog-items"},
	{"GET", "/menu/combos"},
	{"GET", "/health/ready"},
}

func TestRequiredEndpointsDocumented(t *testing.T) {
	if len(requiredEndpoints) < 20 {
		t.Fatal("expected comprehensive endpoint list")
	}
	t.Logf("%d contract endpoints tracked", len(requiredEndpoints))
}
