package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/restflow/api-go/internal/approvals"
	"github.com/restflow/api-go/internal/auth"
	"github.com/restflow/api-go/internal/dashboard"
	"github.com/restflow/api-go/internal/menu"
	"github.com/restflow/api-go/internal/notifications"
	"github.com/restflow/api-go/internal/sync"
	"github.com/restflow/api-go/internal/variance"
	apperrors "github.com/restflow/api-go/pkg/errors"
)

// --- Variance ---
type VarianceHandler struct{ svc *variance.Service }

func NewVarianceHandler(svc *variance.Service) *VarianceHandler { return &VarianceHandler{svc: svc} }

func (h *VarianceHandler) ListBatches(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit == 0 {
		limit, _ = strconv.Atoi(q.Get("pageSize"))
	}
	page, _ := strconv.Atoi(q.Get("page"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	if page > 0 && offset == 0 {
		offset = (page - 1) * limit
	}
	if page <= 0 {
		page = 1
	}
	flagged := q.Get("flagged") == "true"
	items, total, err := h.svc.ListBatches(r.Context(), auth.ShopIDFromContext(r.Context()), flagged, limit, offset)
	if err != nil {
		apperrors.WriteJSON(w, r, apperrors.Internal("failed to list variance batches"))
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": items, "total": total, "page": page, "pageSize": limit,
	})
}

func (h *VarianceHandler) GetBatch(w http.ResponseWriter, r *http.Request) {
	b, err := h.svc.GetBatch(r.Context(), auth.ShopIDFromContext(r.Context()), r.PathValue("batchId"))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, b)
}

func (h *VarianceHandler) Flag(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Flag(r.Context(), auth.ShopIDFromContext(r.Context()), r.PathValue("batchId"), auth.UserIDFromContext(r.Context())); err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *VarianceHandler) Reason(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ReasonCode string `json:"reason_code"`
		ReasonNote string `json:"reason_note"`
		Note       string `json:"note"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	note := req.Note
	if note == "" {
		note = req.ReasonNote
	}
	if err := h.svc.SubmitReason(r.Context(), auth.ShopIDFromContext(r.Context()), r.PathValue("batchId"), req.ReasonCode, note, auth.UserIDFromContext(r.Context())); err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// --- Approvals ---
type ApprovalsHandler struct{ svc *approvals.Service }

func NewApprovalsHandler(svc *approvals.Service) *ApprovalsHandler { return &ApprovalsHandler{svc: svc} }

func (h *ApprovalsHandler) Pending(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.Pending(r.Context(), auth.ShopIDFromContext(r.Context()))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": items})
}

func (h *ApprovalsHandler) History(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.History(r.Context(), auth.ShopIDFromContext(r.Context()), 50)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": items})
}

func (h *ApprovalsHandler) Approve(w http.ResponseWriter, r *http.Request) {
	var req struct{ Notes string `json:"notes"` }
	_ = json.NewDecoder(r.Body).Decode(&req)
	result, err := h.svc.Decide(r.Context(), auth.ShopIDFromContext(r.Context()), r.PathValue("approvalId"), auth.UserIDFromContext(r.Context()), "approve", req.Notes)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *ApprovalsHandler) Reject(w http.ResponseWriter, r *http.Request) {
	var req struct{ Notes string `json:"notes"` }
	_ = json.NewDecoder(r.Body).Decode(&req)
	result, err := h.svc.Decide(r.Context(), auth.ShopIDFromContext(r.Context()), r.PathValue("approvalId"), auth.UserIDFromContext(r.Context()), "reject", req.Notes)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// --- Sync ---
type SyncHandler struct{ svc *sync.Service }

func NewSyncHandler(svc *sync.Service) *SyncHandler { return &SyncHandler{svc: svc} }

func (h *SyncHandler) Bulk(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Events []sync.BulkEvent `json:"events"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid body"))
		return
	}
	result, err := h.svc.Bulk(r.Context(), auth.ShopIDFromContext(r.Context()), auth.UserIDFromContext(r.Context()), req.Events)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// --- Dashboard ---
type DashboardHandler struct{ svc *dashboard.Service }

func NewDashboardHandler(svc *dashboard.Service) *DashboardHandler { return &DashboardHandler{svc: svc} }

func (h *DashboardHandler) Summary(w http.ResponseWriter, r *http.Request) {
	from, to := parseDateRange(r)
	result, err := h.svc.Summary(r.Context(), auth.ShopIDFromContext(r.Context()), *from, *to)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *DashboardHandler) KPIs(w http.ResponseWriter, r *http.Request) {
	from, to := parseDateRange(r)
	result, err := h.svc.KPIs(r.Context(), auth.ShopIDFromContext(r.Context()), *from, *to)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *DashboardHandler) Alerts(w http.ResponseWriter, r *http.Request) {
	result, err := h.svc.Alerts(r.Context(), auth.ShopIDFromContext(r.Context()))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	// Nest returns a bare array; web dashboardApi.getAlerts expects DashboardAlert[].
	if result == nil {
		result = []map[string]interface{}{}
	}
	writeJSON(w, http.StatusOK, result)
}

// --- Notifications ---
type NotificationsHandler struct{ svc *notifications.Service }

func NewNotificationsHandler(svc *notifications.Service) *NotificationsHandler {
	return &NotificationsHandler{svc: svc}
}

func (h *NotificationsHandler) Create(w http.ResponseWriter, r *http.Request) {
	m, err := decodeMap(r.Body)
	if err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid body"))
		return
	}
	idem := r.Header.Get("Idempotency-Key")
	if idem == "" {
		idem = mapString(m, "idempotency_key", "idempotencyKey")
	}
	var data map[string]interface{}
	if d, ok := m["data"].(map[string]interface{}); ok {
		data = d
	}
	result, err := h.svc.Create(r.Context(), notifications.CreateInput{
		ShopID: auth.ShopIDFromContext(r.Context()), ActorUserID: auth.UserIDFromContext(r.Context()),
		UserID: mapString(m, "user_id", "userId"),
		Title: mapString(m, "title"),
		Body: mapString(m, "body", "message"),
		Data: data, IdempotencyKey: idem,
	})
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// --- Menu ---
type MenuHandler struct{ svc *menu.ResolutionService }

func NewMenuHandler(svc *menu.ResolutionService) *MenuHandler { return &MenuHandler{svc: svc} }

func (h *MenuHandler) ListCatalog(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListCatalog(r.Context(), auth.ShopIDFromContext(r.Context()))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": items})
}

func (h *MenuHandler) CreateCatalog(w http.ResponseWriter, r *http.Request) {
	var req menu.CreateCatalogInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid body"))
		return
	}
	req.ShopID = auth.ShopIDFromContext(r.Context())
	item, err := h.svc.CreateCatalogItem(r.Context(), req)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *MenuHandler) ListCombos(w http.ResponseWriter, r *http.Request) {
	items, err := h.svc.ListCombos(r.Context(), auth.ShopIDFromContext(r.Context()))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": items})
}

func (h *MenuHandler) CreateCombo(w http.ResponseWriter, r *http.Request) {
	var req menu.CreateComboInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid body"))
		return
	}
	req.ShopID = auth.ShopIDFromContext(r.Context())
	item, err := h.svc.CreateCombo(r.Context(), req)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h *MenuHandler) PreviewCombo(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ComboID    string                   `json:"combo_id"`
		Quantity   float64                  `json:"quantity"`
		Selections []menu.ComboSelectionInput `json:"combo_selections"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid body"))
		return
	}
	if req.Quantity <= 0 {
		req.Quantity = 1
	}
	resolved, err := h.svc.ResolveComboLine(r.Context(), auth.ShopIDFromContext(r.Context()), req.ComboID, req.Quantity, req.Selections)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, resolved)
}


func (h *DashboardHandler) RebuildSales(w http.ResponseWriter, r *http.Request) {
	shopID := auth.ShopIDFromContext(r.Context())
	n, err := h.svc.RebuildSales(r.Context(), shopID)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "rebuilt": n, "shopId": shopID})
}
