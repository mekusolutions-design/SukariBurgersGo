package handlers

import (
	"net/http"

	"github.com/restflow/api-go/internal/auth"
	apperrors "github.com/restflow/api-go/pkg/errors"
)

type UsersHandler struct {
	authSvc *auth.Service
}

func NewUsersHandler(authSvc *auth.Service) *UsersHandler {
	return &UsersHandler{authSvc: authSvc}
}

func (h *UsersHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID := auth.UserIDFromContext(r.Context())
	if userID == "" {
		apperrors.WriteJSON(w, r, apperrors.Unauthorized("Not authenticated"))
		return
	}
	user, err := h.authSvc.GetUserByID(r.Context(), userID)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	shopID := user.ShopID
	if shopID == "" {
		shopID = "1"
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"id":       user.ID,
		"email":    user.Email,
		"name":     user.Name,
		"role":     string(user.Role),
		"shopId":   shopID,
		"shop_id":  shopID,
		"isActive": user.IsActive,
	})
}
