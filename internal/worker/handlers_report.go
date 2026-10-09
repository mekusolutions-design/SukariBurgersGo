package worker

import (
	"context"
	"encoding/csv"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ReportHandler struct {
	pool *pgxpool.Pool
	log  *slog.Logger
	dir  string
}

func NewReportHandler(pool *pgxpool.Pool, log *slog.Logger) *ReportHandler {
	dir := os.Getenv("WORKER_REPORT_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return &ReportHandler{pool: pool, log: log, dir: dir}
}

func (h *ReportHandler) Run(ctx context.Context, job *Job, jobs *Store) error {
	shopID := "1"
	if job.ShopID != nil && *job.ShopID != "" {
		shopID = *job.ShopID
	}
	shopID = job.PayloadString("shop_id", shopID)
	reportType := job.PayloadString("report_type", "sales_summary")
	from := job.PayloadString("from", time.Now().UTC().AddDate(0, 0, -30).Format("2006-01-02"))
	to := job.PayloadString("to", time.Now().UTC().Format("2006-01-02"))

	_ = jobs.UpdateProgress(ctx, job.ID, map[string]interface{}{
		"phase": "aggregating", "report_type": reportType, "shop_id": shopID,
	})

	path := filepath.Join(h.dir, fmt.Sprintf("restflow-%s-%s-%s.csv", reportType, shopID, job.ID[:8]))
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)

	var rowCount int
	switch reportType {
	case "waste_summary":
		rows, err := h.pool.Query(ctx, `
			SELECT item_id, COALESCE(total_wasted,0), COALESCE(total_value,0)
			FROM "WasteProjection" WHERE shop_id = $1 ORDER BY total_value DESC NULLS LAST
		`, shopID)
		if err != nil {
			return err
		}
		defer rows.Close()
		_ = w.Write([]string{"item_id", "total_wasted", "total_value"})
		for rows.Next() {
			var id string
			var qty, val float64
			if rows.Scan(&id, &qty, &val) == nil {
				_ = w.Write([]string{id, fmt.Sprintf("%g", qty), fmt.Sprintf("%g", val)})
				rowCount++
			}
		}
	case "variance_batches":
		rows, err := h.pool.Query(ctx, `
			SELECT batch_id, COALESCE(product_name,''), expected_qty, actual_qty, variance_qty, variance_pct, value_affected, flagged
			FROM "VarianceBatch"
			WHERE shop_id = $1 AND created_at::date >= $2::date AND created_at::date <= $3::date
			ORDER BY created_at DESC
		`, shopID, from, to)
		if err != nil {
			return err
		}
		defer rows.Close()
		_ = w.Write([]string{"batch_id", "product", "expected", "actual", "variance_qty", "variance_pct", "value", "flagged"})
		for rows.Next() {
			var bid, name string
			var exp, act, vq, vp, val float64
			var flagged bool
			if rows.Scan(&bid, &name, &exp, &act, &vq, &vp, &val, &flagged) == nil {
				_ = w.Write([]string{bid, name, fmt.Sprintf("%g", exp), fmt.Sprintf("%g", act),
					fmt.Sprintf("%g", vq), fmt.Sprintf("%g", vp), fmt.Sprintf("%g", val), fmt.Sprintf("%v", flagged)})
				rowCount++
			}
		}
	case "food_cost", "sales_summary":
		fallthrough
	default:
		rows, err := h.pool.Query(ctx, `
			SELECT period, COALESCE(total_sales,0), COALESCE(total_orders,0), COALESCE(avg_order_value,0)
			FROM "SalesProjection"
			WHERE shop_id = $1 AND period >= $2 AND period <= $3
			ORDER BY period ASC
		`, shopID, from, to)
		if err != nil {
			return err
		}
		defer rows.Close()
		_ = w.Write([]string{"period", "total_sales", "total_orders", "avg_order_value"})
		for rows.Next() {
			var period string
			var sales, aov float64
			var orders int
			if rows.Scan(&period, &sales, &orders, &aov) == nil {
				_ = w.Write([]string{period, fmt.Sprintf("%g", sales), fmt.Sprintf("%d", orders), fmt.Sprintf("%g", aov)})
				rowCount++
			}
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}

	result := map[string]interface{}{
		"path":        path,
		"format":      "csv",
		"report_type": reportType,
		"shop_id":     shopID,
		"from":        from,
		"to":          to,
		"row_count":   rowCount,
	}
	return jobs.MarkSucceeded(ctx, job.ID, result)
}
