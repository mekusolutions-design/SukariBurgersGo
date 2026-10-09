package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/mekusolutions-design/SukariBurgersGo/internal/auth"
	apperrors "github.com/mekusolutions-design/SukariBurgersGo/pkg/errors"
)

func (h *POSHandler) SendToKitchen(w http.ResponseWriter, r *http.Request) {
	orderID := r.PathValue("orderId")
	if orderID == "" {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("orderId required"))
		return
	}
	result, err := h.svc.SendToKitchen(r.Context(), auth.ShopIDFromContext(r.Context()), orderID, auth.UserIDFromContext(r.Context()))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *POSHandler) GetOrder(w http.ResponseWriter, r *http.Request) {
	orderID := r.PathValue("orderId")
	order, err := h.svc.GetOrder(r.Context(), auth.ShopIDFromContext(r.Context()), orderID)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, order)
}

func (h *POSHandler) UpdatePayment(w http.ResponseWriter, r *http.Request) {
	orderID := r.PathValue("orderId")
	var req struct {
		PaymentStatus string `json:"payment_status"`
		PaymentMethod string `json:"payment_method"`
		MpesaRef      string `json:"mpesa_reference"`
		Notes         string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid body"))
		return
	}
	result, err := h.svc.UpdatePayment(r.Context(), auth.ShopIDFromContext(r.Context()), orderID,
		auth.UserIDFromContext(r.Context()), req.PaymentStatus, req.PaymentMethod, req.MpesaRef, req.Notes)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
