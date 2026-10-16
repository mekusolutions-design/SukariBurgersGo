package pos

import "testing"

func TestValidateCreateOrderInputAcceptsPOSContract(t *testing.T) {
	in := CreateOrderInput{
		OrderType: "dine-in", PaymentMethod: "split", PaymentStatus: "paid",
		Items:         []OrderLineInput{{LineType: "menu_item", MenuItemID: "MENU-1", Quantity: 2}},
		PaymentSplits: []PaymentSplit{{Method: "cash", Amount: 10}, {Method: "mpesa", Amount: 10}},
	}
	if err := validateCreateOrderInput(in); err != nil {
		t.Fatalf("expected valid input, got %v", err)
	}
}

func TestValidateCreateOrderInputRejectsFractionalQuantity(t *testing.T) {
	in := CreateOrderInput{OrderType: "takeaway", PaymentMethod: "cash", Items: []OrderLineInput{{MenuItemID: "MENU-1", Quantity: 1.5}}}
	if err := validateCreateOrderInput(in); err == nil {
		t.Fatal("expected fractional quantity to be rejected")
	}
}

func TestValidateCreateOrderInputRejectsUnknownPaymentMethod(t *testing.T) {
	in := CreateOrderInput{OrderType: "takeaway", PaymentMethod: "crypto", Items: []OrderLineInput{{MenuItemID: "MENU-1", Quantity: 1}}}
	if err := validateCreateOrderInput(in); err == nil {
		t.Fatal("expected unknown payment method to be rejected")
	}
}

func TestValidateCreateOrderInputRejectsUnknownLineType(t *testing.T) {
	in := CreateOrderInput{OrderType: "takeaway", PaymentMethod: "cash", Items: []OrderLineInput{{LineType: "arbitrary", MenuItemID: "MENU-1", Quantity: 1}}}
	if err := validateCreateOrderInput(in); err == nil {
		t.Fatal("expected unknown line type to be rejected")
	}
}
