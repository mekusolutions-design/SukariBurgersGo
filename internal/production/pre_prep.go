package production

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/restflow/api-go/internal/eventstore"
	"github.com/restflow/api-go/pkg/money"
	apperrors "github.com/restflow/api-go/pkg/errors"
)

// PrePrepInput matches Nest POST /production/pre-prep.
type PrePrepInput struct {
	ShopID          string
	ActorUserID     string
	RawItemID       string
	PreppedItemID   string
	PreppedItemName string
	OriginalQty     float64
	YieldedQty      float64
	LossReason      string
	Note            string
	Unit            string
	Method          string
}

// PrePrep: raw → prepped FG + optional waste.
// Original = Yielded + Lost (lost system-calculated).
// prepped_unit_cost = total_raw_cost / yielded (loss cost absorbed).
func (s *Service) PrePrep(ctx context.Context, in PrePrepInput) (map[string]interface{}, error) {
	shopID := strings.TrimSpace(in.ShopID)
	if shopID == "" {
		shopID = "1"
	}
	rawID := strings.TrimSpace(in.RawItemID)
	preppedID := strings.TrimSpace(in.PreppedItemID)
	if rawID == "" || preppedID == "" {
		return nil, apperrors.BadRequest("raw_item_id and prepped_item_id are required")
	}
	original := in.OriginalQty
	yielded := in.YieldedQty
	if !(original > 0) || !(yielded > 0) {
		return nil, apperrors.BadRequest("original_qty and yielded_qty must be > 0")
	}
	if yielded > original {
		return nil, apperrors.BadRequest("yielded_qty cannot exceed original_qty")
	}

	lost := math.Round((original-yielded)*1000) / 1000
	lossPct := 0.0
	if original > 0 {
		lossPct = math.Round((lost/original)*1000) / 10
	}
	if lost > 0 && strings.TrimSpace(in.LossReason) == "" {
		return nil, apperrors.BadRequest("loss_reason is required when lost weight > 0")
	}

	var onHand float64
	var avgStr, tvStr *string
	err := s.pool.QueryRow(ctx, `
		SELECT COALESCE(available_stock,0), avg_unit_cost::text, total_value::text
		FROM "InventoryProjection" WHERE shop_id=$1 AND item_id=$2`, shopID, rawID).
		Scan(&onHand, &avgStr, &tvStr)
	if err != nil {
		return nil, apperrors.BadRequest(fmt.Sprintf("raw item not in inventory: %s", rawID))
	}
	if onHand+1e-9 < original {
		return nil, apperrors.BadRequest(fmt.Sprintf(
			"insufficient raw stock for %s: need %v, have %v", rawID, original, onHand))
	}

	rawUC := 0.0
	if avgStr != nil {
		if m, e := money.New(*avgStr); e == nil {
			rawUC = m.Float64()
		}
	}
	if rawUC <= 0 && onHand > 0 && tvStr != nil {
		if m, e := money.New(*tvStr); e == nil && m.Float64() > 0 {
			rawUC = m.Float64() / onHand
		}
	}
	totalRaw := rawUC * original
	preppedUC := 0.0
	if yielded > 0 {
		preppedUC = math.Round((totalRaw/yielded)*10000) / 10000
	}

	unit := in.Unit
	if unit == "" {
		unit = "kg"
	}
	batchID := fmt.Sprintf("PREP-%s", strings.ToUpper(uuid.New().String()[:10]))
	idem := fmt.Sprintf("pre-prep-%s-%s-%s-%.4f-%.4f-%s", shopID, rawID, preppedID, original, yielded, in.ActorUserID)

	name := in.PreppedItemName
	if name == "" {
		name = preppedID
	}

	payload := map[string]interface{}{
		"batch_id": batchID, "batch_type": "pre_prep", "shop_id": shopID,
		"raw_item_id": rawID, "prepped_item_id": preppedID, "prepped_item_name": name,
		"original_qty": original, "yielded_qty": yielded, "lost_qty": lost,
		"loss_pct": lossPct, "loss_reason": prePrepNullStr(in.LossReason),
		"note": prePrepNullStr(in.Note), "method": prePrepNullStr(in.Method),
		"unit": unit, "raw_unit_cost": rawUC, "total_raw_cost": totalRaw,
		"prepped_unit_cost": preppedUC,
		"stock_deductions": []map[string]interface{}{
			{"item_id": rawID, "quantity": original, "unit": unit, "unit_cost": rawUC},
		},
		"inputs": []map[string]interface{}{
			{"item_id": rawID, "actual_quantity": original, "quantity": original, "unit_cost": rawUC},
		},
		"outputs": []map[string]interface{}{
			{
				"item_id": preppedID, "item_name": name, "quantity": yielded,
				"usable_quantity": yielded, "unit": unit, "unit_cost": preppedUC,
				"total_cost": totalRaw, "category": "Finished Goods", "batch_type": "pre_prep",
			},
		},
		"status": "completed", "completed_at": time.Now().UTC().Format(time.RFC3339),
	}
	if lost > 0 {
		payload["waste"] = map[string]interface{}{
			"item_id": rawID, "quantity": lost, "unit": unit,
			"reason": in.LossReason, "cause": "pre_prep", "value": 0,
			"original_qty": original, "yielded_qty": yielded, "batch_id": batchID,
		}
	}

	var actorPtr *string
	if in.ActorUserID != "" {
		actorPtr = &in.ActorUserID
	}
	qty := original
	tc := money.FromFloat(totalRaw)
	uc := money.FromFloat(rawUC)
	bn := batchID
	var wasteReason *string
	if strings.TrimSpace(in.LossReason) != "" {
		wr := strings.TrimSpace(in.LossReason)
		wasteReason = &wr
	}

	event, err := s.eventStore.Append(ctx, eventstore.AppendInput{
		EventType:      "pre_prep_completed",
		ActorUserID:    actorPtr,
		IdempotencyKey: idem,
		ItemID:         &rawID,
		Quantity:       &qty,
		UnitCost:       &uc,
		TotalCost:      &tc,
		BatchNumber:    &bn,
		WasteReason:    wasteReason,
		Payload:        payload,
	})
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"success": true, "batchId": batchID, "eventId": event.ID,
		"original_qty": original, "yielded_qty": yielded, "lost_qty": lost,
		"loss_pct": lossPct, "prepped_unit_cost": preppedUC, "total_raw_cost": totalRaw,
		"message": "Pre-prep completed",
	}, nil
}

func prePrepNullStr(s string) interface{} {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}
