package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/mekusolutions-design/SukariBurgersGo/internal/auth"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/consumption"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/costing"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/finishedgoods"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/kitchen"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/refill"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/trace"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/waste"
	apperrors "github.com/mekusolutions-design/SukariBurgersGo/pkg/errors"
)

// --- Consumption ---
type ConsumptionHandler struct{ svc *consumption.Service }

func NewConsumptionHandler(svc *consumption.Service) *ConsumptionHandler {
	return &ConsumptionHandler{svc: svc}
}
func (h *ConsumptionHandler) Summary(w http.ResponseWriter, r *http.Request) {
	from, to := parseDateRange(r)
	out, err := h.svc.Summary(r.Context(), auth.ShopIDFromContext(r.Context()), *from, *to)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (h *ConsumptionHandler) Product(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Product(r.Context(), auth.ShopIDFromContext(r.Context()), r.PathValue("productId"))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (h *ConsumptionHandler) MenuItem(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.MenuItem(r.Context(), auth.ShopIDFromContext(r.Context()), r.PathValue("menuItemId"))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (h *ConsumptionHandler) Alerts(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Alerts(r.Context(), auth.ShopIDFromContext(r.Context()))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": out})
}

// --- Costing ---
type CostingHandler struct{ svc *costing.Service }

func NewCostingHandler(svc *costing.Service) *CostingHandler { return &CostingHandler{svc: svc} }
func (h *CostingHandler) SKU(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.SKU(r.Context(), auth.ShopIDFromContext(r.Context()), r.PathValue("itemId"))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// --- Finished goods ---
type FinishedGoodsHandler struct{ svc *finishedgoods.Service }

func NewFinishedGoodsHandler(svc *finishedgoods.Service) *FinishedGoodsHandler {
	return &FinishedGoodsHandler{svc: svc}
}
func (h *FinishedGoodsHandler) List(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.List(r.Context(), auth.ShopIDFromContext(r.Context()))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": out})
}
func (h *FinishedGoodsHandler) Add(w http.ResponseWriter, r *http.Request) {
	var req finishedgoods.AddInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid body"))
		return
	}
	req.ShopID = auth.ShopIDFromContext(r.Context())
	req.ActorUserID = auth.UserIDFromContext(r.Context())
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = r.Header.Get("Idempotency-Key")
	}
	out, err := h.svc.Add(r.Context(), req)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (h *FinishedGoodsHandler) Adjust(w http.ResponseWriter, r *http.Request) {
	var req finishedgoods.AdjustInput
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid body"))
		return
	}
	req.ShopID = auth.ShopIDFromContext(r.Context())
	req.ActorUserID = auth.UserIDFromContext(r.Context())
	if req.IdempotencyKey == "" {
		req.IdempotencyKey = r.Header.Get("Idempotency-Key")
	}
	out, err := h.svc.Adjust(r.Context(), req)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// --- Refill ---
type RefillHandler struct{ svc *refill.Service }

func NewRefillHandler(svc *refill.Service) *RefillHandler { return &RefillHandler{svc: svc} }
func (h *RefillHandler) Pending(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Pending(r.Context(), auth.ShopIDFromContext(r.Context()))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": out})
}
func (h *RefillHandler) Get(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Get(r.Context(), auth.ShopIDFromContext(r.Context()), r.PathValue("requestId"))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (h *RefillHandler) Request(w http.ResponseWriter, r *http.Request) {
	m, err := decodeMap(r.Body)
	if err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid body"))
		return
	}
	idem := r.Header.Get("Idempotency-Key")
	if idem == "" {
		idem = mapString(m, "idempotency_key", "idempotencyKey")
	}
	out, err := h.svc.Request(r.Context(), auth.ShopIDFromContext(r.Context()), auth.UserIDFromContext(r.Context()),
		mapString(m, "item_id", "itemId"), mapFloat(m, "quantity"), mapString(m, "note", "notes"), idem)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (h *RefillHandler) Issue(w http.ResponseWriter, r *http.Request) {
	m, err := decodeMap(r.Body)
	if err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid body"))
		return
	}
	idem := r.Header.Get("Idempotency-Key")
	if idem == "" {
		idem = mapString(m, "idempotency_key", "idempotencyKey")
	}
	out, err := h.svc.Issue(r.Context(), auth.ShopIDFromContext(r.Context()), auth.UserIDFromContext(r.Context()),
		mapString(m, "request_id", "requestId"),
		mapString(m, "item_id", "itemId"),
		mapFloat(m, "quantity"), idem)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// --- Trace ---
type TraceHandler struct{ svc *trace.Service }

func NewTraceHandler(svc *trace.Service) *TraceHandler { return &TraceHandler{svc: svc} }
func (h *TraceHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	// Prefer authenticated shop; never trust client shop override for tenant isolation.
	shopID := auth.ShopIDFromContext(r.Context())
	opts := trace.SearchOpts{
		Q:           q.Get("q"),
		ItemID:      firstNonEmpty(q.Get("itemId"), q.Get("item_id")),
		BatchNumber: firstNonEmpty(q.Get("batchNumber"), q.Get("batch_number")),
		EventType:   firstNonEmpty(q.Get("eventType"), q.Get("event_type")),
		Limit:       limit,
	}
	// If no search filters, return recent shop events (diagnostic list) in Nest-compatible envelope.
	if opts.Q == "" && opts.ItemID == "" && opts.BatchNumber == "" && opts.EventType == "" {
		out, err := h.svc.List(r.Context(), shopID, limit)
		if err != nil {
			apperrors.WriteJSON(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"query": "", "shopId": shopID, "eventType": nil,
			"items": []interface{}{}, "events": out, "totalEvents": len(out),
		})
		return
	}
	out, err := h.svc.Search(r.Context(), shopID, opts)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (h *TraceHandler) Diagnostics(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Diagnostics(r.Context(), auth.ShopIDFromContext(r.Context()), r.URL.Query().Get("itemId"))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (h *TraceHandler) ByItem(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	out, err := h.svc.ForItem(r.Context(), auth.ShopIDFromContext(r.Context()), r.PathValue("itemId"), limit)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// Inventory Alerts/Categories live in inventory.go

// Ensure inventory service has Alerts - uses inventory package
// --- Kitchen extras ---
func (h *KitchenHandler) ProductionHistory(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.ProductionHistory(r.Context(), auth.ShopIDFromContext(r.Context()), 50)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": out})
}
func (h *KitchenHandler) ProductionQueue(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.ProductionQueue(r.Context(), auth.ShopIDFromContext(r.Context()))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": out})
}
func (h *KitchenHandler) ClosingStock(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.ClosingStock(r.Context(), auth.ShopIDFromContext(r.Context()))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": out})
}
func (h *KitchenHandler) SubmitClosingStock(w http.ResponseWriter, r *http.Request) {
	m, err := decodeMap(r.Body)
	if err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid body"))
		return
	}
	idem := r.Header.Get("Idempotency-Key")
	if idem == "" {
		idem = mapString(m, "idempotency_key", "idempotencyKey")
	}
	shopID := auth.ShopIDFromContext(r.Context())
	if shopID == "" {
		shopID = mapString(m, "shop_id", "shopId")
	}
	actor := auth.UserIDFromContext(r.Context())
	shift := mapString(m, "shift_label", "shiftLabel")
	notes := mapString(m, "notes", "note")

	// Nest multi-line body
	var lines []kitchen.ClosingLine
	if raw, ok := m["lines"].([]interface{}); ok {
		for _, e := range raw {
			lm, ok := e.(map[string]interface{})
			if !ok {
				continue
			}
			id := mapString(lm, "item_id", "itemId")
			qty := mapFloat(lm, "counted_qty", "countedQty", "counted_quantity", "countedQuantity")
			if id != "" {
				lines = append(lines, kitchen.ClosingLine{ItemID: id, CountedQty: qty})
			}
		}
	}
	if len(lines) > 0 {
		out, err := h.svc.SubmitClosingStockLines(r.Context(), shopID, actor, idem, shift, notes, lines)
		if err != nil {
			apperrors.WriteJSON(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, out)
		return
	}

	// Legacy single-item
	out, err := h.svc.SubmitClosingStock(r.Context(), shopID, actor,
		mapString(m, "item_id", "itemId"),
		mapFloat(m, "actual_qty", "actualQty", "quantity", "counted_qty"), idem)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (h *KitchenHandler) Batches(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.Batches(r.Context(), auth.ShopIDFromContext(r.Context()), r.PathValue("itemId"))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": out})
}

// Ensure kitchen type
var _ = kitchen.Service{}

// --- Waste extras ---
func (h *WasteHandler) ExpiredCandidates(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.ExpiredCandidates(r.Context(), auth.ShopIDFromContext(r.Context()))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": out})
}
func (h *WasteHandler) WriteOffExpired(w http.ResponseWriter, r *http.Request) {
	m, err := decodeMap(r.Body)
	if err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid body"))
		return
	}
	idem := r.Header.Get("Idempotency-Key")
	if idem == "" {
		idem = mapString(m, "idempotency_key", "idempotencyKey")
	}
	out, err := h.svc.WriteOffExpired(r.Context(), auth.ShopIDFromContext(r.Context()), auth.UserIDFromContext(r.Context()),
		mapString(m, "item_id", "itemId"), mapFloat(m, "quantity"), idem)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (h *WasteHandler) Events(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("pageSize"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	from, to := parseDateRange(r)
	out, err := h.svc.Events(r.Context(), auth.ShopIDFromContext(r.Context()), limit, from, to)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"items": out, "total": len(out), "page": 1, "pageSize": limit,
	})
}
func (h *WasteHandler) EventByID(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.EventByID(r.Context(), auth.ShopIDFromContext(r.Context()), r.PathValue("eventId"))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

var _ = waste.Service{}

// --- Users admin ---
func (h *UsersHandler) List(w http.ResponseWriter, r *http.Request) {
	out, err := h.authSvc.ListUsers(r.Context(), auth.ShopIDFromContext(r.Context()))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"items": out})
}
func (h *UsersHandler) Get(w http.ResponseWriter, r *http.Request) {
	out, err := h.authSvc.GetUser(r.Context(), auth.ShopIDFromContext(r.Context()), r.PathValue("id"))
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
func (h *UsersHandler) UpdateRole(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Role string `json:"role"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	out, err := h.authSvc.UpdateRole(r.Context(), auth.ShopIDFromContext(r.Context()), r.PathValue("id"), req.Role)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
