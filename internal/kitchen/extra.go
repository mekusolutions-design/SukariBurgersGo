package kitchen

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/mekusolutions-design/SukariBurgersGo/internal/eventstore"
	apperrors "github.com/mekusolutions-design/SukariBurgersGo/pkg/errors"
	"github.com/mekusolutions-design/SukariBurgersGo/pkg/money"
)

func (s *Service) ProductionHistory(ctx context.Context, shopID string, limit int) ([]map[string]interface{}, error) {
	if limit <= 0 {
		limit = 50
	}
	events, err := s.eventStore.QueryByShopAndType(ctx, shopID, eventstore.EventProductionFinished, limit, 0)
	if err != nil {
		return []map[string]interface{}{}, nil
	}
	var out []map[string]interface{}
	for _, e := range events {
		var p map[string]interface{}
		_ = json.Unmarshal(e.Payload, &p)
		p["event_id"] = e.ID
		p["created_at"] = e.CreatedAt
		out = append(out, p)
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}

func (s *Service) ProductionQueue(ctx context.Context, shopID string) ([]map[string]interface{}, error) {
	started, _ := s.eventStore.QueryByShopAndType(ctx, shopID, eventstore.EventProductionStarted, 100, 0)
	finished, _ := s.eventStore.QueryByShopAndType(ctx, shopID, eventstore.EventProductionFinished, 200, 0)
	done := map[string]bool{}
	for _, e := range finished {
		var p map[string]interface{}
		_ = json.Unmarshal(e.Payload, &p)
		for _, k := range []string{"production_id", "productionId", "productionID", "batch_id", "batchId"} {
			if id, _ := p[k].(string); id != "" {
				done[id] = true
			}
		}
	}
	var out []map[string]interface{}
	for _, e := range started {
		var p map[string]interface{}
		_ = json.Unmarshal(e.Payload, &p)
		if p == nil {
			p = map[string]interface{}{}
		}
		pid, _ := p["production_id"].(string)
		if pid == "" {
			pid, _ = p["productionId"].(string)
		}
		if pid == "" {
			pid = e.ID // fallback so finish always has an id
		}
		if done[pid] {
			continue
		}
		// Nest/web queue shape (camelCase + snake)
		out = append(out, map[string]interface{}{
			"productionId":  pid,
			"production_id": pid,
			"recipeId":      p["recipe_id"],
			"recipe_id":     p["recipe_id"],
			"recipeName":    p["recipe_name"],
			"recipe_name":   p["recipe_name"],
			"batchSize":     p["batch_size"],
			"batch_size":    p["batch_size"],
			"status":        "started",
			"startedAt":     p["started_at"],
			"started_at":    p["started_at"],
			"finishedAt":    nil,
			"event_id":      e.ID,
			"created_at":    e.CreatedAt,
		})
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}

func (s *Service) ClosingStock(ctx context.Context, shopID string) ([]map[string]interface{}, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT p.item_id, COALESCE(i.name, p.item_id), COALESCE(i.unit,'pcs'),
		       p.available_stock, COALESCE(p.avg_unit_cost,0)::text, COALESCE(p.total_value,0)::text,
		       p.last_counted_at, p.last_counted_qty
		FROM "InventoryProjection" p
		LEFT JOIN "Item" i ON i.item_id = p.item_id
		WHERE p.shop_id = $1 ORDER BY i.name NULLS LAST`, shopID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var id, name string
		var stock float64
		var countedAt *time.Time
		var countedQty *float64
		_ = rows.Scan(&id, &name, &stock, &countedAt, &countedQty)
		row := map[string]interface{}{"item_id": id, "name": name, "available_stock": stock}
		if countedAt != nil {
			row["last_counted_at"] = *countedAt
		}
		if countedQty != nil {
			row["last_counted_qty"] = *countedQty
		}
		out = append(out, row)
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}

// ClosingLine is one counted row (Nest web/mobile contract).
type ClosingLine struct {
	ItemID     string
	CountedQty float64
}

// SubmitClosingStock supports legacy single-item and Nest multi-line payloads.
func (s *Service) SubmitClosingStock(ctx context.Context, shopID, actorUserID, itemID string, actualQty float64, idem string) (map[string]interface{}, error) {
	if itemID != "" {
		return s.SubmitClosingStockLines(ctx, shopID, actorUserID, idem, "", "", []ClosingLine{{ItemID: itemID, CountedQty: actualQty}})
	}
	return nil, apperrors.BadRequest("item_id or lines required")
}

// SubmitClosingStockLines matches Nest POST /kitchen/closing-stock with lines[].
func (s *Service) SubmitClosingStockLines(ctx context.Context, shopID, actorUserID, idem, shiftLabel, notes string, lines []ClosingLine) (map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	if len(lines) == 0 {
		return nil, apperrors.BadRequest("at least one counted line is required")
	}
	dayKey := time.Now().UTC().Format("2006-01-02")
	shiftKey := strings.ToLower(strings.TrimSpace(shiftLabel))
	if shiftKey == "" {
		shiftKey = "default"
	}
	shiftKey = strings.ReplaceAll(shiftKey, " ", "-")
	if idem == "" {
		idem = "closing-stock-" + shopID + "-" + dayKey + "-" + shiftKey
	}

	// Idempotent replay
	existing, err := s.eventStore.FindByIdempotencyKey(ctx, idem)
	if err == nil && existing != nil {
		return map[string]interface{}{
			"success": true, "alreadySubmitted": true, "eventId": existing.ID,
			"message": "Closing stock already submitted for this shift/day",
		}, nil
	}

	var actor *string
	if actorUserID != "" {
		actor = &actorUserID
	}

	// Prefetch WAC for variance valuation
	itemIDs := make([]string, 0, len(lines))
	for _, l := range lines {
		if l.ItemID != "" {
			itemIDs = append(itemIDs, l.ItemID)
		}
	}
	wac := map[string]float64{}
	sysQty := map[string]float64{}
	names := map[string]string{}
	units := map[string]string{}
	if len(itemIDs) > 0 {
		rows, qerr := s.pool.Query(ctx, `
			SELECT p.item_id, COALESCE(p.available_stock,0), COALESCE(p.avg_unit_cost,0)::text,
			       COALESCE(p.total_value,0)::text, COALESCE(i.name, p.item_id), COALESCE(i.unit,'pcs')
			FROM "InventoryProjection" p
			LEFT JOIN "Item" i ON i.item_id = p.item_id
			WHERE p.shop_id = $1 AND p.item_id = ANY($2)`, shopID, itemIDs)
		if qerr == nil {
			defer rows.Close()
			for rows.Next() {
				var id, name, unit, avgS, tvS string
				var aq float64
				if rows.Scan(&id, &aq, &avgS, &tvS, &name, &unit) != nil {
					continue
				}
				sysQty[id] = aq
				names[id] = name
				units[id] = unit
				uc := 0.0
				if m, e := money.New(avgS); e == nil {
					uc = m.Float64()
				}
				if uc <= 0 && aq > 0 {
					if m, e := money.New(tvS); e == nil && m.Float64() > 0 {
						uc = m.Float64() / aq
					}
				}
				wac[id] = uc
			}
		}
	}

	payloadLines := make([]map[string]interface{}, 0, len(lines))
	totalVar := 0.0
	flagged := 0
	for _, l := range lines {
		if l.ItemID == "" {
			continue
		}
		system := sysQty[l.ItemID]
		uc := wac[l.ItemID]
		vQty := l.CountedQty - system
		vVal := vQty * uc
		if vQty != 0 {
			flagged++
			if vVal < 0 {
				totalVar += -vVal
			} else {
				totalVar += vVal
			}
		}
		payloadLines = append(payloadLines, map[string]interface{}{
			"item_id": l.ItemID, "item_name": names[l.ItemID], "unit": units[l.ItemID],
			"system_qty": system, "expected_quantity": system,
			"counted_qty": l.CountedQty, "counted_quantity": l.CountedQty,
			"variance_qty": vQty, "variance_quantity": vQty,
			"variance_value": vVal, "unit_cost": uc,
		})
	}

	batchID := "COUNT-" + shopID + "-" + dayKey + "-" + shiftKey
	display := dayKey[len(dayKey)-6:] + "-" + strings.ToUpper(shiftKey)
	if len(display) > 12 {
		display = display[:12]
	}
	countedAt := time.Now().UTC().Format(time.RFC3339)
	payload := map[string]interface{}{
		"batch_id": batchID, "batch_number": display, "shop_id": shopID,
		"shift_label": shiftLabel, "notes": notes, "status": "completed",
		"counted_at": countedAt, "items_counted": len(payloadLines),
		"items_flagged": flagged, "total_variance_value": totalVar,
		"stock_count_completed": true, "lines": payloadLines,
	}

	event, err := s.eventStore.Append(ctx, eventstore.AppendInput{
		EventType: eventstore.EventClosingStockCounted, ActorUserID: actor,
		IdempotencyKey: idem, BatchNumber: &display, Payload: payload,
	})
	if err != nil {
		return nil, err
	}
	// Alias event for Nest list compatibility (projection no-ops this type)
	_, _ = s.eventStore.Append(ctx, eventstore.AppendInput{
		EventType: eventstore.EventStockCountCompleted, ActorUserID: actor,
		IdempotencyKey: idem + "-alias", BatchNumber: &display,
		Payload: map[string]interface{}{
			"batch_id": batchID, "shop_id": shopID, "source_event_id": event.ID,
			"lines": payloadLines, "stock_count_completed": true,
		},
	})

	return map[string]interface{}{
		"success": true, "alreadySubmitted": false,
		"batchId": batchID, "batchNumber": display, "eventId": event.ID,
		"shop_id": shopID, "items_counted": len(payloadLines),
		"items_flagged": flagged, "total_variance_value": totalVar,
		"lines":   payloadLines,
		"message": "Closing stock count submitted successfully",
	}, nil
}

func (s *Service) Batches(ctx context.Context, shopID, itemID string) ([]map[string]interface{}, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, event_type, batch_number, expiry_date, quantity, payload, created_at
		FROM "Event"
		WHERE shop_id = $1 AND item_id = $2 AND batch_number IS NOT NULL
		ORDER BY created_at DESC LIMIT 100`, shopID, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var id, et string
		var batch *string
		var exp *time.Time
		var qty *float64
		var payload []byte
		var created time.Time
		_ = rows.Scan(&id, &et, &batch, &exp, &qty, &payload, &created)
		row := map[string]interface{}{"event_id": id, "event_type": et, "created_at": created}
		if batch != nil {
			row["batch_number"] = *batch
		}
		if exp != nil {
			row["expiry_date"] = *exp
		}
		if qty != nil {
			row["quantity"] = *qty
		}
		out = append(out, row)
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}
