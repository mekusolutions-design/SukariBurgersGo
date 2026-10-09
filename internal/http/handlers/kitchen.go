package handlers

import (
	"net/http"

	"github.com/mekusolutions-design/SukariBurgersGo/internal/auth"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/kitchen"
	apperrors "github.com/mekusolutions-design/SukariBurgersGo/pkg/errors"
)

type KitchenHandler struct {
	svc *kitchen.Service
}

func NewKitchenHandler(svc *kitchen.Service) *KitchenHandler {
	return &KitchenHandler{svc: svc}
}

func (h *KitchenHandler) ActiveOrders(w http.ResponseWriter, r *http.Request) {
	orders, err := h.svc.ActiveOrders(r.Context(), auth.ShopIDFromContext(r.Context()))
	if err != nil {
		apperrors.WriteJSON(w, r, apperrors.Internal("failed to list kitchen orders"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "orders": orders, "items": orders, "count": len(orders), "total": len(orders)})
}

func (h *KitchenHandler) UpdateStatus(w http.ResponseWriter, r *http.Request) {
	orderID := r.PathValue("orderId")
	m, err := decodeMap(r.Body)
	if err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid body"))
		return
	}
	status := mapString(m, "status", "kitchen_status", "kitchenStatus")
	result, err := h.svc.UpdateOrderStatus(r.Context(), auth.ShopIDFromContext(r.Context()), orderID, status, auth.UserIDFromContext(r.Context()))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
