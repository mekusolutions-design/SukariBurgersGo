package handlers

import (
	"net/http"
	"strconv"

	"github.com/mekusolutions-design/SukariBurgersGo/internal/auth"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/inventory"
	apperrors "github.com/mekusolutions-design/SukariBurgersGo/pkg/errors"
)

type InventoryHandler struct {
	svc *inventory.Service
}

func NewInventoryHandler(svc *inventory.Service) *InventoryHandler {
	return &InventoryHandler{svc: svc}
}

func (h *InventoryHandler) List(w http.ResponseWriter, r *http.Request) {
	shopID := auth.ShopIDFromContext(r.Context())
	if shopID == "" {
		apperrors.WriteJSON(w, r, apperrors.Forbidden("shop identity required"))
		return
	}
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit == 0 {
		limit, _ = strconv.Atoi(q.Get("pageSize"))
	}

	opts := inventory.ListOptions{
		ShopID:            shopID,
		Page:              page,
		Limit:             limit,
		Search:            q.Get("search"),
		Category:          q.Get("category"),
		Status:            q.Get("status"),
		LowStockOnly:      q.Get("lowStockOnly") == "true",
		IncludeFinished:   q.Get("includeFinished") == "true",
		IncludeWrittenOff: q.Get("includeWrittenOff") == "true",
	}

	result, err := h.svc.List(r.Context(), opts)
	if err != nil {
		apperrors.WriteJSON(w, r, apperrors.Internal("failed to list inventory"))
		return
	}

	items := make([]map[string]interface{}, 0, len(result.Items))
	for _, row := range result.Items {
		items = append(items, inventory.RowToAPI(row))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": items, "total": result.Total, "page": result.Page,
		"limit": result.Limit, "pageSize": result.PageSize, "totalPages": result.TotalPages,
	})
}

func (h *InventoryHandler) Summary(w http.ResponseWriter, r *http.Request) {
	shopID := auth.ShopIDFromContext(r.Context())
	if shopID == "" {
		apperrors.WriteJSON(w, r, apperrors.Forbidden("shop identity required"))
		return
	}
	sum, err := h.svc.Summary(r.Context(), shopID)
	if err != nil {
		apperrors.WriteJSON(w, r, apperrors.Internal("failed to get inventory summary"))
		return
	}
	writeJSON(w, http.StatusOK, sum)
}

func (h *InventoryHandler) GetByItemID(w http.ResponseWriter, r *http.Request) {
	shopID := auth.ShopIDFromContext(r.Context())
	itemID := r.PathValue("itemId")
	if itemID == "" {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("itemId required"))
		return
	}
	row, err := h.svc.GetByItemID(r.Context(), shopID, itemID)
	if err != nil {
		apperrors.WriteJSON(w, r, apperrors.NotFound("Item not found in inventory"))
		return
	}
	writeJSON(w, http.StatusOK, inventory.RowToAPI(*row))
}

func (h *InventoryHandler) Alerts(w http.ResponseWriter, r *http.Request) {
	shopID := auth.ShopIDFromContext(r.Context())
	if shopID == "" {
		apperrors.WriteJSON(w, r, apperrors.Forbidden("shop identity required"))
		return
	}
	out, err := h.svc.Alerts(r.Context(), shopID)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	// Nest returns a bare array
	writeJSON(w, http.StatusOK, out)
}

func (h *InventoryHandler) Categories(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Categories(r.Context(), auth.ShopIDFromContext(r.Context()))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *InventoryHandler) Batches(w http.ResponseWriter, r *http.Request) {
	shopID := auth.ShopIDFromContext(r.Context())
	if shopID == "" {
		apperrors.WriteJSON(w, r, apperrors.Forbidden("shop identity required"))
		return
	}
	itemID := r.PathValue("itemId")
	out, err := h.svc.Batches(r.Context(), shopID, itemID)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
