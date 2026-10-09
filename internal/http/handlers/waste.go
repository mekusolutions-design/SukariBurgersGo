package handlers

import (
	"net/http"

	"github.com/mekusolutions-design/SukariBurgersGo/internal/auth"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/waste"
	apperrors "github.com/mekusolutions-design/SukariBurgersGo/pkg/errors"
)

type WasteHandler struct {
	svc *waste.Service
}

func NewWasteHandler(svc *waste.Service) *WasteHandler {
	return &WasteHandler{svc: svc}
}

func (h *WasteHandler) Record(w http.ResponseWriter, r *http.Request) {
	m, err := decodeMap(r.Body)
	if err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid body"))
		return
	}
	idem := r.Header.Get("Idempotency-Key")
	if idem == "" {
		idem = mapString(m, "idempotency_key", "idempotencyKey")
	}
	result, err := h.svc.Record(r.Context(), waste.RecordInput{
		ShopID: auth.ShopIDFromContext(r.Context()), ActorUserID: auth.UserIDFromContext(r.Context()),
		ItemID:         mapString(m, "item_id", "itemId"),
		ItemName:       mapString(m, "item_name", "itemName"),
		BatchNumber:    mapString(m, "batch_number", "batchNumber"),
		Unit:           mapString(m, "unit", "units"),
		Reason:         mapString(m, "reason", "waste_reason", "wasteReason"),
		PhotoURL:       mapString(m, "photo_url", "photoUrl", "waste_photo_url", "wastePhotoUrl"),
		IdempotencyKey: idem,
		Quantity:       mapFloat(m, "quantity"),
		UnitCost:       mapFloat(m, "unit_cost", "unitCost"),
	})
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *WasteHandler) Analytics(w http.ResponseWriter, r *http.Request) {
	from, to := parseDateRange(r)
	result, err := h.svc.Analytics(r.Context(), auth.ShopIDFromContext(r.Context()), from, to)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
