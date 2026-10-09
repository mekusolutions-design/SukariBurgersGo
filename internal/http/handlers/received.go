package handlers

import (
	"net/http"
	"time"

	"github.com/mekusolutions-design/SukariBurgersGo/internal/auth"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/receiving"
	apperrors "github.com/mekusolutions-design/SukariBurgersGo/pkg/errors"
)

type ReceivedHandler struct {
	svc *receiving.Service
}

func NewReceivedHandler(svc *receiving.Service) *ReceivedHandler {
	return &ReceivedHandler{svc: svc}
}

type receiveRequest struct {
	ItemID           string   `json:"item_id"`
	ItemName         string   `json:"item_name"`
	Units            string   `json:"units"`
	Category         string   `json:"category"`
	Quantity         float64  `json:"quantity"`
	QuantityApproved *float64 `json:"quantity_approved"`
	UnitCost         float64  `json:"unit_cost"`
	TotalCost        *float64 `json:"total_cost"`
	BatchNumber      string   `json:"batch_number"`
	ExpiryDate       string   `json:"expiry_date"`
	DateReceived     string   `json:"date_received"`
	SupplierName     string   `json:"supplier_name"`
	SupplierNumber   string   `json:"supplier_number"`
	SupplierID       string   `json:"supplier_id"`
	ApprovedBy       string   `json:"approved_by"`
	Reference        string   `json:"reference"`
	WasteReason      string   `json:"waste_reason"`
	WastePhotoURL    string   `json:"waste_photo_url"`
	IdempotencyKey   string   `json:"idempotency_key"`
}

func (h *ReceivedHandler) Receive(w http.ResponseWriter, r *http.Request) {
	m, err := decodeMap(r.Body)
	if err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid request body"))
		return
	}

	idem := r.Header.Get("Idempotency-Key")
	if idem == "" {
		idem = mapString(m, "idempotency_key", "idempotencyKey")
	}

	expiryStr := mapString(m, "expiry_date", "expiryDate")
	var expiry *time.Time
	if expiryStr != "" {
		if tt, e := time.Parse(time.RFC3339, expiryStr); e == nil {
			expiry = &tt
		} else if tt, e := time.Parse("2006-01-02", expiryStr); e == nil {
			expiry = &tt
		}
	}

	qty := mapFloat(m, "quantity")
	unitCost := mapFloat(m, "unit_cost", "unitCost")
	var totalCost *float64
	if v := mapFloat(m, "total_cost", "totalCost"); v > 0 {
		totalCost = &v
	}
	var qtyApproved *float64
	if v := mapFloat(m, "quantity_approved", "quantityApproved"); v > 0 {
		qtyApproved = &v
	} else if rejected := mapFloat(m, "quantity_rejected", "quantityRejected"); rejected > 0 && qty > rejected {
		// Web form often sends received + rejected; Nest approved = received - rejected
		a := qty - rejected
		qtyApproved = &a
	}

	in := receiving.ReceiveInput{
		ItemID:           mapString(m, "item_id", "itemId"),
		ItemName:         mapString(m, "item_name", "itemName"),
		Units:            mapString(m, "units", "unit"),
		Category:         mapString(m, "category"),
		Quantity:         qty,
		QuantityApproved: qtyApproved,
		UnitCost:         unitCost,
		TotalCost:        totalCost,
		BatchNumber:      mapString(m, "batch_number", "batchNumber"),
		ExpiryDate:       expiry,
		DateReceived:     mapString(m, "date_received", "dateReceived"),
		SupplierName:     mapString(m, "supplier_name", "supplierName"),
		SupplierNumber:   mapString(m, "supplier_number", "supplierNumber"),
		SupplierID:       mapString(m, "supplier_id", "supplierId"),
		ApprovedBy:       mapString(m, "approved_by", "approvedBy"),
		Reference:        mapString(m, "reference"),
		WasteReason:      mapString(m, "waste_reason", "wasteReason"),
		WastePhotoURL:    mapString(m, "waste_photo_url", "wastePhotoUrl"),
		ShopID:           auth.ShopIDFromContext(r.Context()),
		ActorUserID:      auth.UserIDFromContext(r.Context()),
		IdempotencyKey:   idem,
	}

	result, err := h.svc.Receive(r.Context(), in)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
