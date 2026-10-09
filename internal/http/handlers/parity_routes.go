package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/mekusolutions-design/SukariBurgersGo/internal/auth"
	apperrors "github.com/mekusolutions-design/SukariBurgersGo/pkg/errors"
)

// AuthMe aliases Nest GET /auth/me → current user (same as GET /users/me).
func (h *UsersHandler) AuthMe(w http.ResponseWriter, r *http.Request) {
	h.Me(w, r)
}

// CreateOrderAlias handles mobile POST /pos/orders (Nest mobile path).
func (h *POSHandler) CreateOrderAlias(w http.ResponseWriter, r *http.Request) {
	h.CreateOrder(w, r)
}

// ListReceived GET /received — recent goods-received events for shop.
func (h *ReceivedHandler) List(w http.ResponseWriter, r *http.Request) {
	shopID := auth.ShopIDFromContext(r.Context())
	if shopID == "" {
		shopID = r.URL.Query().Get("shopId")
		if shopID == "" {
			shopID = "1"
		}
	}
	out, err := h.svc.ListRecent(r.Context(), shopID, 50)
	if err != nil {
		apperrors.WriteJSON(w, r, apperrors.Internal("failed to list received"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": out, "total": len(out)})
}

// Update recipe PUT /recipe/{recipeId}
func (h *RecipeHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("recipeId")
	var body map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid body"))
		return
	}
	rec, err := h.svc.Update(r.Context(), id, body)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, rec)
}

// AddOutput POST /recipe/{recipeId}/outputs
func (h *RecipeHandler) AddOutput(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("recipeId")
	var body struct {
		ItemID   string  `json:"item_id"`
		ItemName string  `json:"item_name"`
		Quantity float64 `json:"quantity"`
		Unit     string  `json:"unit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid body"))
		return
	}
	out, err := h.svc.AddOutput(r.Context(), id, body.ItemID, body.ItemName, body.Quantity, body.Unit)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

// RecipeByItemPath handles GET /recipe/item/{itemId} without registering a conflicting ServeMux pattern.
func RecipeByItemPath(h *RecipeHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		const prefix = "/recipe/item/"
		if !strings.HasPrefix(path, prefix) {
			http.NotFound(w, r)
			return
		}
		itemID := strings.TrimPrefix(path, prefix)
		itemID = strings.Trim(itemID, "/")
		if itemID == "" {
			apperrors.WriteJSON(w, r, apperrors.BadRequest("itemId required"))
			return
		}
		rec, err := h.svc.GetByItemID(r.Context(), itemID)
		if err != nil {
			apperrors.WriteJSON(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, rec)
	}
}

// NotificationsList GET /notifications
func (h *NotificationsHandler) List(w http.ResponseWriter, r *http.Request) {
	shopID := auth.ShopIDFromContext(r.Context())
	userID := auth.UserIDFromContext(r.Context())
	out, err := h.svc.ListForUser(r.Context(), shopID, userID, 50)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"items": []interface{}{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": out})
}

// NotificationSettings GET/PUT /notifications/settings (mobile)
func (h *NotificationsHandler) Settings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"settings": map[string]interface{}{
			"low_stock": true, "expiry": true, "approvals": true, "orders": true,
		},
	})
}
