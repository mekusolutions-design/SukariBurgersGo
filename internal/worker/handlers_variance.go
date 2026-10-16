package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type VarianceConsumptionHandler struct {
	pool *pgxpool.Pool
	log  *slog.Logger
}

func NewVarianceConsumptionHandler(pool *pgxpool.Pool, log *slog.Logger) *VarianceConsumptionHandler {
	return &VarianceConsumptionHandler{pool: pool, log: log}
}

func tolerancePct() float64 {
	v := os.Getenv("VARIANCE_TOLERANCE_PCT")
	if v == "" {
		return 5.0
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f <= 0 {
		return 5.0
	}
	return f
}

func (h *VarianceConsumptionHandler) Run(ctx context.Context, job *Job, jobs *Store) error {
	shopID := "1"
	if job.ShopID != nil && *job.ShopID != "" {
		shopID = *job.ShopID
	}
	shopID = job.PayloadString("shop_id", shopID)
	from := job.PayloadString("from", time.Now().UTC().AddDate(0, 0, -30).Format("2006-01-02"))
	to := job.PayloadString("to", time.Now().UTC().Format("2006-01-02"))
	scope := job.PayloadString("scope", "both") // variance | consumption | both

	tol := tolerancePct()
	flagged := 0
	updated := 0

	if scope == "variance" || scope == "both" {
		rows, err := h.pool.Query(ctx, `
			SELECT id, batch_id, expected_qty, actual_qty, COALESCE(value_affected,0)
			FROM "VarianceBatch"
			WHERE shop_id = $1
			  AND created_at::date >= $2::date AND created_at::date <= $3::date
		`, shopID, from, to)
		if err != nil {
			return fmt.Errorf("list variance batches: %w", err)
		}
		type row struct {
			id, batchID string
			exp, act, val float64
		}
		var list []row
		for rows.Next() {
			var r row
			if rows.Scan(&r.id, &r.batchID, &r.exp, &r.act, &r.val) == nil {
				list = append(list, r)
			}
		}
		rows.Close()

		for i, r := range list {
			vq := r.act - r.exp
			vp := 0.0
			if r.exp != 0 {
				vp = (vq / r.exp) * 100
			}
			isFlagged := math.Abs(vp) > tol
			_, err := h.pool.Exec(ctx, `
				UPDATE "VarianceBatch"
				SET variance_qty = $1, variance_pct = $2, flagged = $3, updated_at = NOW()
				WHERE id = $4
			`, vq, vp, isFlagged, r.id)
			if err != nil {
				h.log.Warn("variance update", "batch", r.batchID, "error", err)
				continue
			}
			updated++
			if isFlagged {
				flagged++
			}
			if i%25 == 0 {
				_ = jobs.UpdateProgress(ctx, job.ID, map[string]interface{}{
					"scope": "variance", "updated": updated, "flagged": flagged, "index": i,
				})
			}
		}
	}

	consumptionRows := 0
	if scope == "consumption" || scope == "both" {
		// Aggregate stock_deductions from pos_sale payloads in range (true usage, not revenue proxy)
		rows, err := h.pool.Query(ctx, `
			SELECT payload::text FROM "Event"
			WHERE shop_id = $1 AND event_type = 'pos_sale'
			  AND created_at::date >= $2::date AND created_at::date <= $3::date
		`, shopID, from, to)
		if err != nil {
			return err
		}
		usage := map[string]float64{}
		for rows.Next() {
			var payloadStr string
			if rows.Scan(&payloadStr) != nil {
				continue
			}
			var p map[string]interface{}
			if json.Unmarshal([]byte(payloadStr), &p) != nil {
				continue
			}
			raw, ok := p["stock_deductions"].([]interface{})
			if !ok {
				continue
			}
			for _, entry := range raw {
				m, ok := entry.(map[string]interface{})
				if !ok {
					continue
				}
				id, _ := m["item_id"].(string)
				if id == "" {
					id, _ = m["itemId"].(string)
				}
				qty := 0.0
				switch v := m["quantity"].(type) {
				case float64:
					qty = v
				}
				if id != "" && qty > 0 {
					usage[id] += qty
					consumptionRows++
				}
			}
		}
		rows.Close()

		// Store summary in job result (v1 — no dedicated ConsumptionProjection table required)
		_ = jobs.UpdateProgress(ctx, job.ID, map[string]interface{}{
			"scope": "consumption", "sku_count": len(usage), "deduction_lines": consumptionRows,
		})
		return jobs.MarkSucceeded(ctx, job.ID, map[string]interface{}{
			"shop_id": shopID, "from": from, "to": to, "scope": scope,
			"variance_updated": updated, "variance_flagged": flagged,
			"tolerance_pct": tol, "consumption_skus": len(usage),
			"consumption_by_sku": usage,
		})
	}

	return jobs.MarkSucceeded(ctx, job.ID, map[string]interface{}{
		"shop_id": shopID, "from": from, "to": to, "scope": scope,
		"variance_updated": updated, "variance_flagged": flagged, "tolerance_pct": tol,
	})
}
