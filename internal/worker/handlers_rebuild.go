package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/restflow/api-go/internal/eventstore"
	"github.com/restflow/api-go/internal/projections"
)

// RebuildHandler implements strategy C: idempotent upsert replay (no truncate by default).
type RebuildHandler struct {
	pool   *pgxpool.Pool
	store  *eventstore.Store
	engine *projections.Engine
	log    *slog.Logger
	batch  int
}

func NewRebuildHandler(pool *pgxpool.Pool, es *eventstore.Store, engine *projections.Engine, log *slog.Logger, batch int) *RebuildHandler {
	if batch <= 0 {
		batch = 200
	}
	return &RebuildHandler{pool: pool, store: es, engine: engine, log: log, batch: batch}
}

func (h *RebuildHandler) Run(ctx context.Context, job *Job, jobs *Store) error {
	shopID := ""
	if job.ShopID != nil {
		shopID = *job.ShopID
	}
	if s := job.PayloadString("shop_id", ""); s != "" {
		shopID = s
	}

	confirmClear := job.PayloadBool("confirm_clear", false)
	filter := job.PayloadString("projection_types", "all")

	if confirmClear {
		if err := h.clearProjections(ctx, shopID, filter); err != nil {
			return fmt.Errorf("clear projections: %w", err)
		}
		h.log.Warn("cleared projections before rebuild", "shop_id", shopID, "filter", filter, "job_id", job.ID)
	}

	total, _ := h.store.CountEvents(ctx, shopID)
	var processed int64
	var afterTime *time.Time
	afterID := ""

	// Resume from checkpoint
	if job.Progress != nil {
		if v, ok := job.Progress["last_event_id"].(string); ok {
			afterID = v
		}
		if v, ok := job.Progress["last_created_at"].(string); ok {
			if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
				afterTime = &t
			} else if t, err := time.Parse(time.RFC3339, v); err == nil {
				afterTime = &t
			}
		}
		if v, ok := job.Progress["processed"].(float64); ok {
			processed = int64(v)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		events, err := h.store.QueryEventsASC(ctx, shopID, afterTime, afterID, h.batch)
		if err != nil {
			return fmt.Errorf("query events: %w", err)
		}
		if len(events) == 0 {
			break
		}

		for _, ev := range events {
			tx, err := h.pool.Begin(ctx)
			if err != nil {
				return err
			}
			if err := h.engine.UpdateProjections(ctx, tx, ev); err != nil {
				_ = tx.Rollback(ctx)
				h.log.Error("projection failed", "event_id", ev.ID, "type", ev.EventType, "error", err)
				// Strategy C: skip bad event, continue stream
			} else {
				if err := tx.Commit(ctx); err != nil {
					return err
				}
			}
			processed++
			afterTime = &ev.CreatedAt
			afterID = ev.ID
		}

		progress := map[string]interface{}{
			"processed":       processed,
			"total":           total,
			"last_event_id":   afterID,
			"last_created_at": afterTime.UTC().Format(time.RFC3339Nano),
			"shop_id":         shopID,
			"strategy":        "C_idempotent_upsert",
		}
		_ = jobs.UpdateProgress(ctx, job.ID, progress)
		h.log.Info("rebuild progress", "job_id", job.ID, "processed", processed, "total", total)
	}

	_ = jobs.MarkSucceeded(ctx, job.ID, map[string]interface{}{
		"processed": processed,
		"total":     total,
		"shop_id":   shopID,
		"strategy":  "C_idempotent_upsert",
	})
	return nil
}

func (h *RebuildHandler) clearProjections(ctx context.Context, shopID, filter string) error {
	// Only when confirm_clear=true
	tables := []string{}
	switch filter {
	case "inventory":
		tables = []string{`"InventoryProjection"`}
	case "sales":
		tables = []string{`"SalesProjection"`}
	case "waste":
		tables = []string{`"WasteProjection"`}
	default:
		tables = []string{`"InventoryProjection"`, `"SalesProjection"`, `"WasteProjection"`}
	}
	for _, tbl := range tables {
		var err error
		if shopID == "" || shopID == "*" {
			_, err = h.pool.Exec(ctx, fmt.Sprintf(`DELETE FROM %s`, tbl))
		} else {
			_, err = h.pool.Exec(ctx, fmt.Sprintf(`DELETE FROM %s WHERE shop_id = $1`, tbl), shopID)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
