package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/restflow/api-go/internal/eventstore"
	"github.com/restflow/api-go/pkg/money"
)

// ExpiryHandler mirrors Nest jobs/expiry-sweep.processor.ts (idempotent).
type ExpiryHandler struct {
	pool *pgxpool.Pool
	es   *eventstore.Store
	log  *slog.Logger
}

func NewExpiryHandler(pool *pgxpool.Pool, es *eventstore.Store, log *slog.Logger) *ExpiryHandler {
	return &ExpiryHandler{pool: pool, es: es, log: log}
}

func (h *ExpiryHandler) Run(ctx context.Context, job *Job, jobs *Store) error {
	shopID := "1"
	if job.ShopID != nil && *job.ShopID != "" {
		shopID = *job.ShopID
	}
	today := time.Now().UTC()
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	nearThreshold := today.AddDate(0, 0, 7)

	rows, err := h.pool.Query(ctx, `
		SELECT id, item_id, batch_number, expiry_date, quantity, unit_cost::text, total_cost::text, payload
		FROM "Event"
		WHERE shop_id = $1
		  AND event_type IN ('received','finished_good_added','production_finished')
		  AND expiry_date IS NOT NULL
		  AND expiry_date <= $2
		ORDER BY expiry_date ASC
		LIMIT 2000`, shopID, nearThreshold)
	if err != nil {
		return fmt.Errorf("expiry candidates: %w", err)
	}
	defer rows.Close()

	processed, expired, alerts := 0, 0, 0
	for rows.Next() {
		var id string
		var itemID, batchNum *string
		var expiry *time.Time
		var qty *float64
		var unitCostStr, totalCostStr *string
		var payload []byte
		if err := rows.Scan(&id, &itemID, &batchNum, &expiry, &qty, &unitCostStr, &totalCostStr, &payload); err != nil {
			continue
		}
		processed++
		if expiry == nil {
			continue
		}
		var p map[string]interface{}
		jsonUnmarshal(payload, &p)

		item := ""
		if itemID != nil {
			item = *itemID
		}
		if item == "" && p != nil {
			if v, ok := p["item_id"].(string); ok {
				item = v
			}
		}
		quantity := 0.0
		if qty != nil {
			quantity = *qty
		}
		if quantity <= 0 && p != nil {
			quantity = asFloatMap(p, "quantity_approved", "quantity", "actual_quantity_produced")
		}
		if item == "" || quantity <= 0 {
			continue
		}

		daysLeft := int(expiry.Sub(today).Hours() / 24)
		if daysLeft <= 0 {
			idem := "expiry-" + id
			sys := "system-expiry-sweep"
			bn := ""
			if batchNum != nil {
				bn = *batchNum
			}
			uc := 0.0
			if unitCostStr != nil {
				if m, e := money.New(*unitCostStr); e == nil {
					uc = m.Float64()
				}
			}
			tc := 0.0
			if totalCostStr != nil {
				if m, e := money.New(*totalCostStr); e == nil {
					tc = m.Float64()
				}
			}
			if tc <= 0 {
				tc = uc * quantity
			}
			unitM := money.FromFloatUnitCost(uc)
			totalM := money.FromFloat(tc).AsTotal()
			var itemPtr *string = &item
			var bnPtr *string
			if bn != "" {
				bnPtr = &bn
			}
			reason := "expired"
			_, err := h.es.Append(ctx, eventstore.AppendInput{
				EventType:      eventstore.EventBatchExpired,
				ActorUserID:    &sys,
				IdempotencyKey: idem,
				ItemID:         itemPtr,
				BatchNumber:    bnPtr,
				ExpiryDate:     expiry,
				Quantity:       &quantity,
				UnitCost:       &unitM,
				TotalCost:      &totalM,
				WasteReason:    &reason,
				Payload: map[string]interface{}{
					"item_id": item, "quantity": quantity,
					"unit_cost": uc, "total_cost": tc,
					"source_event_id": id, "batch_number": bn,
					"expiry_date": expiry.UTC().Format(time.RFC3339),
					"shop_id": shopID, "waste_reason": "expired",
				},
			})
			if err != nil {
				h.log.Debug("expiry skip/conflict", "source", id, "error", err)
				continue
			}
			expired++
			continue
		}

		if daysLeft <= 7 {
			alerts++
			_, _ = h.pool.Exec(ctx, `
				UPDATE "InventoryProjection"
				SET days_to_expiry_min = $1, next_expiry_date = $2, updated_at = $3
				WHERE shop_id = $4 AND item_id = $5`,
				daysLeft, expiry, time.Now().UTC(), shopID, item)
		}
	}

	result := map[string]interface{}{
		"processed": processed, "expired": expired, "alerts": alerts,
	}
	_ = jobs.UpdateProgress(ctx, job.ID, result)
	h.log.Info("expiry sweep done", "shop", shopID, "expired", expired, "alerts", alerts)
	_ = jobs.MarkSucceeded(ctx, job.ID, result)
	return nil
}

func jsonUnmarshal(b []byte, v *map[string]interface{}) {
	if len(b) == 0 {
		return
	}
	_ = json.Unmarshal(b, v)
}

func asFloatMap(p map[string]interface{}, keys ...string) float64 {
	for _, k := range keys {
		if v, ok := p[k].(float64); ok {
			return v
		}
	}
	return 0
}
