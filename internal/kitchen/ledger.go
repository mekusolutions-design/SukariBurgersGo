package kitchen

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DailyStockLedger — Nest KitchenService.getDailyStockLedger approximate parity.
func (s *Service) DailyStockLedger(ctx context.Context, shopID, dateIso string) (map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	if dateIso == "" {
		dateIso = time.Now().UTC().Format("2006-01-02")
	}
	if len(dateIso) > 10 {
		dateIso = dateIso[:10]
	}
	dayStart, _ := time.Parse(time.RFC3339, dateIso+"T00:00:00.000Z")
	dayEnd, _ := time.Parse(time.RFC3339, dateIso+"T23:59:59.999Z")

	type proj struct {
		itemID, name, unit string
		stock              float64
	}
	prows, err := s.pool.Query(ctx, `
		SELECT p.item_id, COALESCE(i.name, p.item_id), COALESCE(i.unit, ''), p.available_stock
		FROM "InventoryProjection" p
		LEFT JOIN "Item" i ON i.item_id = p.item_id
		WHERE p.shop_id = $1
		ORDER BY p.item_id ASC
		LIMIT 200`, shopID)
	if err != nil {
		return nil, err
	}
	defer prows.Close()
	var projs []proj
	for prows.Next() {
		var p proj
		if prows.Scan(&p.itemID, &p.name, &p.unit, &p.stock) == nil {
			projs = append(projs, p)
		}
	}

	type movAgg struct {
		added, used, waste float64
	}
	byItem := map[string]*movAgg{}
	mrows, err := s.pool.Query(ctx, `
		SELECT item_id, movement_type, quantity
		FROM "InventoryMovement"
		WHERE shop_id = $1 AND created_at >= $2 AND created_at <= $3
		LIMIT 5000`, shopID, dayStart, dayEnd)
	if err == nil {
		defer mrows.Close()
		for mrows.Next() {
			var itemID, mtype string
			var qty float64
			if mrows.Scan(&itemID, &mtype, &qty) != nil {
				continue
			}
			a := byItem[itemID]
			if a == nil {
				a = &movAgg{}
				byItem[itemID] = a
			}
			mt := mtype
			switch {
			case mt == "RECEIPT" || mt == "PRODUCTION_OUTPUT" || mt == "finished_good_added":
				if qty > 0 {
					a.added += qty
				} else {
					a.added += -qty
				}
			case mt == "WASTE" || mt == "RECEIVE_VOID" || mt == "EXPIRED":
				if qty < 0 {
					a.waste += -qty
				} else {
					a.waste += qty
				}
			default:
				// sales / consume / count
				if qty < 0 {
					a.used += -qty
				} else if mt == "POS_SALE" || mt == "SALE" || mt == "CONSUMPTION" {
					a.used += qty
				}
			}
		}
	}

	prevLines := map[string]float64{}
	var prevPayload []byte
	_ = s.pool.QueryRow(ctx, `
		SELECT payload FROM "Event"
		WHERE shop_id = $1 AND event_type = 'closing_stock_counted' AND created_at < $2
		ORDER BY created_at DESC LIMIT 1`, shopID, dayStart).Scan(&prevPayload)
	if len(prevPayload) > 0 {
		var p map[string]interface{}
		_ = json.Unmarshal(prevPayload, &p)
		if lines, ok := p["lines"].([]interface{}); ok {
			for _, raw := range lines {
				row, _ := raw.(map[string]interface{})
				if row == nil {
					continue
				}
				id, _ := row["item_id"].(string)
				if id == "" {
					id, _ = row["itemId"].(string)
				}
				cq := 0.0
				if v, ok := row["counted_qty"].(float64); ok {
					cq = v
				} else if v, ok := row["counted_quantity"].(float64); ok {
					cq = v
				}
				if id != "" {
					prevLines[id] = cq
				}
			}
		}
	}

	todayLines := map[string]map[string]interface{}{}
	var sessionMeta map[string]interface{}
	var todayPayload []byte
	var todayAt time.Time
	_ = s.pool.QueryRow(ctx, `
		SELECT payload, created_at FROM "Event"
		WHERE shop_id = $1 AND event_type = 'closing_stock_counted'
		  AND created_at >= $2 AND created_at <= $3
		ORDER BY created_at DESC LIMIT 1`, shopID, dayStart, dayEnd).Scan(&todayPayload, &todayAt)
	if len(todayPayload) > 0 {
		var p map[string]interface{}
		_ = json.Unmarshal(todayPayload, &p)
		sessionMeta = map[string]interface{}{
			"batchId": p["batch_id"], "batchNumber": p["batch_number"],
			"status": p["status"], "countedAt": todayAt.UTC().Format(time.RFC3339),
			"totalVarianceValue": p["total_variance_value"],
		}
		if lines, ok := p["lines"].([]interface{}); ok {
			for _, raw := range lines {
				row, _ := raw.(map[string]interface{})
				if row == nil {
					continue
				}
				id, _ := row["item_id"].(string)
				if id == "" {
					id, _ = row["itemId"].(string)
				}
				if id != "" {
					todayLines[id] = row
				}
			}
		}
	}

	items := []map[string]interface{}{}
	for _, pr := range projs {
		opening := prevLines[pr.itemID]
		if _, ok := prevLines[pr.itemID]; !ok {
			// no prior close: opening ≈ current - net day (best effort)
			agg := byItem[pr.itemID]
			net := 0.0
			if agg != nil {
				net = agg.added - agg.used - agg.waste
			}
			opening = pr.stock - net
			if opening < 0 {
				opening = 0
			}
		}
		added, used, waste := 0.0, 0.0, 0.0
		if a := byItem[pr.itemID]; a != nil {
			added, used, waste = a.added, a.used, a.waste
		}
		expected := opening + added - used - waste
		closing := pr.stock
		counted := interface{}(nil)
		variance := interface{}(nil)
		if tl, ok := todayLines[pr.itemID]; ok {
			if v, ok := tl["counted_qty"].(float64); ok {
				counted = v
				variance = v - expected
				closing = v
			} else if v, ok := tl["counted_quantity"].(float64); ok {
				counted = v
				variance = v - expected
				closing = v
			}
		}
		items = append(items, map[string]interface{}{
			"itemId": pr.itemID, "item_id": pr.itemID,
			"name": pr.name, "unit": pr.unit,
			"opening": opening, "added": added, "used": used, "waste": waste,
			"expected": expected, "closing": closing,
			"counted_qty": counted, "variance": variance,
			"system_qty": pr.stock,
		})
	}
	if items == nil {
		items = []map[string]interface{}{}
	}
	return map[string]interface{}{
		"success": true,
		"shopId":  shopID,
		"date":    dateIso,
		"items":   items,
		"rows":    items,
		"session": sessionMeta,
		"source":  "go-nest-parity",
		"count":   len(items),
	}, nil
}

func (s *Service) ClosingStockHistory(ctx context.Context, shopID string, limit int) (map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.payload, e.created_at, COALESCE(u.name, u.email, '')
		FROM "Event" e
		LEFT JOIN users u ON u.id = e.actor_user_id
		WHERE e.shop_id = $1 AND e.event_type IN ('closing_stock_counted', 'stock_count_completed')
		ORDER BY e.created_at DESC
		LIMIT $2`, shopID, limit)
	if err != nil {
		return map[string]interface{}{"success": true, "items": []interface{}{}, "count": 0}, nil
	}
	defer rows.Close()
	var items []map[string]interface{}
	for rows.Next() {
		var id string
		var payload []byte
		var created time.Time
		var actor string
		if rows.Scan(&id, &payload, &created, &actor) != nil {
			continue
		}
		var p map[string]interface{}
		_ = json.Unmarshal(payload, &p)
		items = append(items, map[string]interface{}{
			"id": id, "createdAt": created.UTC().Format(time.RFC3339),
			"actor": actor, "payload": p,
			"batchId": p["batch_id"], "status": p["status"],
		})
	}
	if items == nil {
		items = []map[string]interface{}{}
	}
	return map[string]interface{}{"success": true, "items": items, "count": len(items)}, nil
}

func (s *Service) CountAdjustments(ctx context.Context, shopID, countRef string) (map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT m.id, m.item_id, m.quantity, m.unit_cost::text, m.reference_id, m.idempotency_key, m.created_at,
		       COALESCE(i.name, m.item_id)
		FROM "InventoryMovement" m
		LEFT JOIN "Item" i ON i.item_id = m.item_id
		WHERE m.shop_id = $1 AND m.movement_type = 'COUNT_ADJUSTMENT'
		  AND (m.reference_id = $2 OR m.idempotency_key LIKE $3)
		ORDER BY m.created_at ASC
		LIMIT 500`, shopID, countRef, "count-adj:"+countRef+"%")
	if err != nil {
		return map[string]interface{}{"success": true, "items": []interface{}{}, "count": 0}, nil
	}
	defer rows.Close()
	var items []map[string]interface{}
	for rows.Next() {
		var id, itemID, name string
		var qty float64
		var uc, ref, idem *string
		var created time.Time
		if rows.Scan(&id, &itemID, &qty, &uc, &ref, &idem, &created, &name) != nil {
			continue
		}
		row := map[string]interface{}{
			"id": id, "itemId": itemID, "item_id": itemID, "name": name,
			"quantity": qty, "createdAt": created.UTC().Format(time.RFC3339),
		}
		items = append(items, row)
	}
	if items == nil {
		items = []map[string]interface{}{}
	}
	return map[string]interface{}{"success": true, "items": items, "count": len(items), "countRef": countRef}, nil
}

// ensure Service has pool — same package as existing Service
var _ *pgxpool.Pool
