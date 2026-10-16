package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/restflow/api-go/internal/auth"
	"github.com/restflow/api-go/internal/production"
	apperrors "github.com/restflow/api-go/pkg/errors"
)

type ProductionHandler struct {
	svc *production.Service
}

func NewProductionHandler(svc *production.Service) *ProductionHandler {
	return &ProductionHandler{svc: svc}
}

func (h *ProductionHandler) Start(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid body"))
		return
	}
	idem := r.Header.Get("Idempotency-Key")
	if idem == "" {
		if v, ok := req["idempotency_key"].(string); ok {
			idem = v
		}
	}
	recipeID, _ := req["recipe_id"].(string)
	if recipeID == "" {
		recipeID, _ = req["recipeId"].(string)
	}
	if recipeID == "" {
		recipeID, _ = req["item_id"].(string)
	}
	recipeName, _ := req["recipe_name"].(string)
	if recipeName == "" {
		recipeName, _ = req["item_name"].(string)
	}
	batchSize := asFloat(req["batch_size"], asFloat(req["planned_quantity"], asFloat(req["quantity"], 1)))

	result, err := h.svc.Start(r.Context(), production.StartInput{
		ShopID:         auth.ShopIDFromContext(r.Context()),
		ActorUserID:    auth.UserIDFromContext(r.Context()),
		RecipeID:       recipeID,
		RecipeName:     recipeName,
		BatchSize:      batchSize,
		IdempotencyKey: idem,
	})
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (h *ProductionHandler) Finish(w http.ResponseWriter, r *http.Request) {
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid body"))
		return
	}

	in := production.FinishInput{
		ShopID:      auth.ShopIDFromContext(r.Context()),
		ActorUserID: auth.UserIDFromContext(r.Context()),
		ProductionID: firstString(req, "production_id", "productionId"),
		RecipeID:     firstString(req, "recipe_id", "recipeId"),
		ItemID:       firstString(req, "item_id", "itemId"),
		ItemName:     firstString(req, "item_name", "itemName"),
		BatchNumber:  firstString(req, "batch_number", "batchNumber"),
		ExpiryDate:   firstString(req, "expiry_date", "expiryDate"),
		WasteReason:  firstString(req, "waste_reason", "wasteReason"),
		PlannedQty:   asFloat(req["planned_quantity"], asFloat(req["plannedQuantity"], 0)),
		ActualQty:    asFloat(req["actual_quantity_produced"], asFloat(req["actualQuantityProduced"], asFloat(req["actualYield"], 0))),
		WasteQty:     asFloat(req["waste_quantity"], asFloat(req["wasteQuantity"], 0)),
		UnitCost:     asFloat(req["unit_cost"], asFloat(req["unitCost"], 0)),
		TotalCost:    asFloat(req["total_cost"], asFloat(req["totalCost"], 0)),
		Raw:          req,
	}
	if in.ProductionID == "" {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("production_id required"))
		return
	}
	if in.BatchNumber == "" {
		in.BatchNumber = "AUTO-" + trimTail(in.ProductionID, 8)
	}
	if in.ExpiryDate == "" {
		// Nest requires expiry; default today UTC for web if omitted
		in.ExpiryDate = todayUTC()
	}

	// Parse inputs / outputs arrays (Nest ProductionLine shape)
	in.Inputs = parseLines(req["inputs"])
	in.Outputs = parseLines(req["outputs"])
	if in.ActualQty <= 0 && len(in.Outputs) > 0 {
		for _, o := range in.Outputs {
			in.ActualQty += o.ActualQuantity
		}
	}

	result, err := h.svc.Finish(r.Context(), in)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *ProductionHandler) History(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	out, err := h.svc.History(r.Context(), auth.ShopIDFromContext(r.Context()), limit)
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func firstString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

func asFloat(v interface{}, fallback float64) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case float32:
		return float64(t)
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	case string:
		f, err := strconv.ParseFloat(t, 64)
		if err == nil {
			return f
		}
	}
	return fallback
}

func parseLines(raw interface{}) []production.Line {
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	out := make([]production.Line, 0, len(arr))
	for _, e := range arr {
		m, ok := e.(map[string]interface{})
		if !ok {
			continue
		}
		out = append(out, production.Line{
			ItemID:            firstString(m, "item_id", "itemId"),
			ItemName:          firstString(m, "item_name", "itemName"),
			Unit:              firstString(m, "unit"),
			ActualQuantity:    asFloat(m["actual_quantity"], asFloat(m["actualQuantity"], 0)),
			StandardQuantity:  asFloat(m["standard_quantity"], asFloat(m["standardQuantity"], 0)),
			UnitCost:          asFloat(m["unit_cost"], asFloat(m["unitCost"], 0)),
			UnitWeight:        asFloat(m["unit_weight"], asFloat(m["unitWeight"], 0)),
			WeightUnit:        firstString(m, "weight_unit", "weightUnit"),
		})
	}
	return out
}

func trimTail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func todayUTC() string {
	return production.TodayUTC()
}

func (h *ProductionHandler) PrePrep(w http.ResponseWriter, r *http.Request) {
	m, err := decodeMap(r.Body)
	if err != nil {
		apperrors.WriteJSON(w, r, apperrors.BadRequest("Invalid body"))
		return
	}
	out, err := h.svc.PrePrep(r.Context(), production.PrePrepInput{
		ShopID:          auth.ShopIDFromContext(r.Context()),
		ActorUserID:     auth.UserIDFromContext(r.Context()),
		RawItemID:       mapString(m, "raw_item_id", "rawItemId"),
		PreppedItemID:   mapString(m, "prepped_item_id", "preppedItemId"),
		PreppedItemName: mapString(m, "prepped_item_name", "preppedItemName"),
		OriginalQty:     mapFloat(m, "original_qty", "originalQty"),
		YieldedQty:      mapFloat(m, "yielded_qty", "yieldedQty"),
		LossReason:      mapString(m, "loss_reason", "lossReason"),
		Note:            mapString(m, "note", "notes"),
		Unit:            mapString(m, "unit"),
		Method:          mapString(m, "method"),
	})
	if err != nil {
		apperrors.WriteJSON(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
