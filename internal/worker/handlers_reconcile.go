package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/eventstore"
)

type ReconcileHandler struct {
	pool  *pgxpool.Pool
	store *eventstore.Store
	log   *slog.Logger
}

func NewReconcileHandler(pool *pgxpool.Pool, es *eventstore.Store, log *slog.Logger) *ReconcileHandler {
	return &ReconcileHandler{pool: pool, store: es, log: log}
}

func (h *ReconcileHandler) Run(ctx context.Context, job *Job, jobs *Store) error {
	shopID := "1"
	if job.ShopID != nil && *job.ShopID != "" {
		shopID = *job.ShopID
	}
	shopID = job.PayloadString("shop_id", shopID)
	mode := job.PayloadString("mode", "both") // reconcile | backfill | both
	dateStr := job.PayloadString("date", time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02"))

	mismatchCount := 0
	backfilled := 0

	if mode == "reconcile" || mode == "both" {
		// SalesProjection vs sum of pos_sale totals for the day
		var projected float64
		_ = h.pool.QueryRow(ctx, `
			SELECT COALESCE(total_sales, 0) FROM "SalesProjection"
			WHERE shop_id = $1 AND period = $2
		`, shopID, dateStr).Scan(&projected)

		var eventSum float64
		_ = h.pool.QueryRow(ctx, `
			SELECT COALESCE(SUM( (payload->>'total_amount')::float ), 0)
			FROM "Event"
			WHERE shop_id = $1 AND event_type = $2
			  AND created_at::date = $3::date
		`, shopID, eventstore.EventPosSale, dateStr).Scan(&eventSum)

		diff := eventSum - projected
		if diff < 0 {
			diff = -diff
		}
		if diff > 0.5 {
			mismatchCount++
			_, _ = h.pool.Exec(ctx, `
				INSERT INTO "ReconcileMismatch" (id, shop_id, kind, ref_key, expected, actual, severity, detail, job_id, created_at)
				VALUES ($1, $2, 'sales_day', $3, $4, $5, 'warning', $6::jsonb, $7, NOW())
			`, uuid.New().String(), shopID, dateStr, eventSum, projected,
				fmt.Sprintf(`{"source":"pos_sale_sum","projected":%g}`, projected), job.ID)
		}
	}

	if mode == "backfill" || mode == "both" {
		// Ensure SalesProjection row exists for the day from events
		var eventSum float64
		var orderCount int
		_ = h.pool.QueryRow(ctx, `
			SELECT COALESCE(SUM( (payload->>'total_amount')::float ), 0), COUNT(*)
			FROM "Event"
			WHERE shop_id = $1 AND event_type = $2
			  AND created_at::date = $3::date
		`, shopID, eventstore.EventPosSale, dateStr).Scan(&eventSum, &orderCount)

		if orderCount > 0 {
			aov := 0.0
			if orderCount > 0 {
				aov = eventSum / float64(orderCount)
			}
			_, err := h.pool.Exec(ctx, `
				INSERT INTO "SalesProjection" (id, shop_id, period, total_sales, total_orders, avg_order_value, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, NOW())
				ON CONFLICT (shop_id, period) DO UPDATE SET
					total_sales = EXCLUDED.total_sales,
					total_orders = EXCLUDED.total_orders,
					avg_order_value = EXCLUDED.avg_order_value,
					updated_at = NOW()
			`, uuid.New().String(), shopID, dateStr, eventSum, orderCount, aov)
			if err != nil {
				h.log.Warn("sales backfill upsert", "error", err)
			} else {
				backfilled++
			}
		}
	}

	_ = jobs.UpdateProgress(ctx, job.ID, map[string]interface{}{
		"mode": mode, "date": dateStr, "mismatches": mismatchCount, "backfilled": backfilled,
	})
	return jobs.MarkSucceeded(ctx, job.ID, map[string]interface{}{
		"shop_id": shopID, "date": dateStr, "mode": mode,
		"mismatches": mismatchCount, "backfilled": backfilled,
	})
}
