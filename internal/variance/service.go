package variance

import (
	"math"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/restflow/api-go/internal/eventstore"
	"github.com/restflow/api-go/pkg/money"
	apperrors "github.com/restflow/api-go/pkg/errors"
)

// isMaterialVariance implements Fix #5: round to 2dp; 0.00 is not a variance.
func isMaterialVariance(qty, value float64) bool {
	q := math.Round(qty*100) / 100
	if math.Abs(q) >= 0.01 {
		return true
	}
	v := math.Round(value*100) / 100
	return math.Abs(v) >= 0.01
}

type Service struct {
	pool       *pgxpool.Pool
	eventStore *eventstore.Store
}

func NewService(pool *pgxpool.Pool, es *eventstore.Store) *Service {
	return &Service{pool: pool, eventStore: es}
}

type Batch struct {
	ID            string          `json:"id"`
	BatchID       string          `json:"batch_id"`
	ShopID        string          `json:"shop_id"`
	ProductItemID *string         `json:"product_item_id"`
	ProductName   *string         `json:"product_name"`
	Source        string          `json:"source"`
	ExpectedQty   float64         `json:"expected_qty"`
	ActualQty     float64         `json:"actual_qty"`
	VarianceQty   float64         `json:"variance_qty"`
	VariancePct   float64         `json:"variance_pct"`
	ValueAffected float64         `json:"value_affected"`
	Flagged       bool            `json:"flagged"`
	ReasonCode    *string         `json:"reason_code"`
	ReasonNote    *string         `json:"reason_note"`
	Lines         json.RawMessage `json:"lines,omitempty"`
	ProductionID  *string         `json:"production_id"`
	CreatedAt     time.Time       `json:"created_at"`
}


// ListBatchesWeb returns Nest/web VarianceBatch shape from events (+ VarianceBatch table fallback).
func (s *Service) ListBatches(ctx context.Context, shopID string, flaggedOnly bool, limit, offset int) ([]map[string]interface{}, int, error) {
	if shopID == "" {
		shopID = "1"
	}
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}

	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.event_type, e.total_cost::text, e.created_at, e.payload,
		       u.name, u.email
		FROM "Event" e
		LEFT JOIN "users" u ON u.id = e.actor_user_id
		WHERE e.shop_id = $1
		  AND e.event_type IN (
		    'variance_batch', 'closing_stock_counted', 'stock_count_completed', 'VARIANCE_WRITEOFF'
		  )
		ORDER BY e.created_at DESC
		LIMIT 500`, shopID)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	seen := map[string]struct{}{}
	var all []map[string]interface{}
	for rows.Next() {
		var id, et string
		var totalCostStr *string
		var created time.Time
		var payload []byte
		var actorName, actorEmail *string
		if rows.Scan(&id, &et, &totalCostStr, &created, &payload, &actorName, &actorEmail) != nil {
			continue
		}
		var p map[string]interface{}
		_ = json.Unmarshal(payload, &p)
		if p == nil {
			p = map[string]interface{}{}
		}
		batchID, _ := p["batch_id"].(string)
		if batchID == "" {
			batchID, _ = p["batchId"].(string)
		}
		if batchID == "" {
			batchID = id
		}
		if _, ok := seen[batchID]; ok {
			continue
		}
		seen[batchID] = struct{}{}

		lines, _ := p["lines"].([]interface{})
		if lines == nil {
			lines = []interface{}{}
		}
		flagged := p["flagged"] == true
		itemsFlagged := 0
		for _, l := range lines {
			lm, _ := l.(map[string]interface{})
			if lm == nil {
				continue
			}
			vq := 0.0
			if v, ok := lm["varianceQuantity"].(float64); ok {
				vq = v
			} else if v, ok := lm["variance_quantity"].(float64); ok {
				vq = v
			} else if v, ok := lm["variance_qty"].(float64); ok {
				vq = v
			}
			if isMaterialVariance(vq, 0) {
				itemsFlagged++
				flagged = true
			}
		}
		if flaggedOnly && !flagged {
			continue
		}
		val := 0.0
		if totalCostStr != nil {
			if m, e := money.New(*totalCostStr); e == nil {
				val = m.Float64()
			}
		}
		if v, ok := p["value_affected"].(float64); ok && v != 0 {
			val = v
		}
		if v, ok := p["total_variance_value"].(float64); ok && v != 0 {
			val = v
		}
		countedBy := "system"
		if actorName != nil && *actorName != "" {
			countedBy = *actorName
		} else if actorEmail != nil && *actorEmail != "" {
			countedBy = *actorEmail
		}
		status := "open"
		if s, ok := p["status"].(string); ok && s != "" {
			status = s
		} else if flagged {
			status = "flagged"
		} else {
			status = "reviewed"
		}
		itemsCounted := len(lines)
		if v, ok := p["items_counted"].(float64); ok && int(v) > itemsCounted {
			itemsCounted = int(v)
		}
		// Fix #5: hide batches that round to 0.00 variance
		if !flaggedOnly && !isMaterialVariance(0, val) && itemsFlagged == 0 {
			continue
		}
		all = append(all, map[string]interface{}{
			"batchId": batchID, "batch_id": batchID,
			"countedBy": countedBy, "countedAt": created.UTC().Format(time.RFC3339),
			"status": status,
			"itemsCounted": itemsCounted, "itemsFlagged": itemsFlagged,
			"totalVarianceValue": val, "flagged": flagged,
			"source": et, "lines": lines,
		})
	}

	// Table fallback if no events
	if len(all) == 0 {
		batches, total, err := s.listFromTable(ctx, shopID, flaggedOnly, limit, offset)
		if err != nil {
			return nil, 0, err
		}
		return batches, total, nil
	}

	total := len(all)
	end := offset + limit
	if offset > total {
		offset = total
	}
	if end > total {
		end = total
	}
	return all[offset:end], total, nil
}

func (s *Service) listFromTable(ctx context.Context, shopID string, flaggedOnly bool, limit, offset int) ([]map[string]interface{}, int, error) {
	where := `shop_id = $1`
	args := []interface{}{shopID}
	if flaggedOnly {
		where += ` AND flagged = true`
	}
	var total int
	_ = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM "VarianceBatch" WHERE `+where, args...).Scan(&total)
	q := fmt.Sprintf(`SELECT id, batch_id, product_name, source, expected_qty, actual_qty, variance_qty, variance_pct,
		value_affected::text, flagged, reason_code, lines, created_at
		FROM "VarianceBatch" WHERE %s ORDER BY created_at DESC LIMIT %d OFFSET %d`, where, limit, offset)
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var id, batchID, source string
		var productName, reason *string
		var exp, act, vq, vp float64
		var valStr string
		var flagged bool
		var lines []byte
		var created time.Time
		if rows.Scan(&id, &batchID, &productName, &source, &exp, &act, &vq, &vp, &valStr, &flagged, &reason, &lines, &created) != nil {
			continue
		}
		val := 0.0
		if m, e := money.New(valStr); e == nil {
			val = m.Float64()
		}
		status := "open"
		if flagged {
			status = "flagged"
		}
		var lineArr interface{} = []interface{}{}
		_ = json.Unmarshal(lines, &lineArr)
		out = append(out, map[string]interface{}{
			"batchId": batchID, "countedBy": "system",
			"countedAt": created.UTC().Format(time.RFC3339),
			"status": status, "itemsCounted": 0, "itemsFlagged": 0,
			"totalVarianceValue": val, "flagged": flagged,
			"productName": productName, "source": source, "lines": lineArr,
		})
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, total, nil
}


func (s *Service) GetBatch(ctx context.Context, shopID, batchID string) (*Batch, error) {
	const q = `SELECT id, batch_id, shop_id, product_item_id, product_name, source,
		expected_qty, actual_qty, variance_qty, variance_pct, value_affected,
		flagged, reason_code, reason_note, lines, production_id, created_at
		FROM "VarianceBatch" WHERE shop_id = $1 AND (batch_id = $2 OR id = $2)`
	var b Batch
	var valStr string
	err := s.pool.QueryRow(ctx, q, shopID, batchID).Scan(&b.ID, &b.BatchID, &b.ShopID, &b.ProductItemID, &b.ProductName, &b.Source,
		&b.ExpectedQty, &b.ActualQty, &b.VarianceQty, &b.VariancePct, &valStr,
		&b.Flagged, &b.ReasonCode, &b.ReasonNote, &b.Lines, &b.ProductionID, &b.CreatedAt)
	if err != nil {
		return nil, apperrors.NotFound("Variance batch not found")
	}
	if m, e := money.New(valStr); e == nil {
		b.ValueAffected = m.Float64()
	}
	return &b, nil
}

func (s *Service) Flag(ctx context.Context, shopID, batchID, actorUserID string) error {
	const q = `UPDATE "VarianceBatch" SET flagged = true, updated_at = $1 WHERE shop_id = $2 AND (batch_id = $3 OR id = $3)`
	tag, err := s.pool.Exec(ctx, q, time.Now().UTC(), shopID, batchID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("Variance batch not found")
	}
	var actorPtr *string
	if actorUserID != "" {
		actorPtr = &actorUserID
	}
	_, _ = s.eventStore.Append(ctx, eventstore.AppendInput{
		EventType: eventstore.EventVarianceFlagged, ActorUserID: actorPtr,
		IdempotencyKey: "var-flag-" + batchID + "-" + uuid.New().String()[:8],
		Payload:        map[string]interface{}{"batch_id": batchID, "shop_id": shopID, "flagged": true},
	})
	return nil
}

func (s *Service) SubmitReason(ctx context.Context, shopID, batchID, reasonCode, reasonNote, actorUserID string) error {
	const q = `UPDATE "VarianceBatch" SET reason_code = $1, reason_note = $2, updated_at = $3 WHERE shop_id = $4 AND (batch_id = $5 OR id = $5)`
	tag, err := s.pool.Exec(ctx, q, reasonCode, reasonNote, time.Now().UTC(), shopID, batchID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return apperrors.NotFound("Variance batch not found")
	}
	var actorPtr *string
	if actorUserID != "" {
		actorPtr = &actorUserID
	}
	_, _ = s.eventStore.Append(ctx, eventstore.AppendInput{
		EventType: eventstore.EventVarianceReasonSubmitted, ActorUserID: actorPtr,
		IdempotencyKey: "var-reason-" + batchID + "-" + uuid.New().String()[:8],
		Payload: map[string]interface{}{
			"batch_id": batchID, "shop_id": shopID, "reason_code": reasonCode, "reason_note": reasonNote,
		},
	})
	return nil
}
