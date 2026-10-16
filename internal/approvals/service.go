package approvals

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/restflow/api-go/internal/eventstore"
	apperrors "github.com/restflow/api-go/pkg/errors"
)

// Nest PENDING_EVENT_TYPES / APPROVAL_EVENT_TYPES
var pendingEventTypes = []string{
	"WASTE_RECORDED", "waste_recorded", "waste",
	"STOCK_ADJUSTMENT", "finished_good_adjusted",
	"RECEIVING", "received",
	"VARIANCE_WRITEOFF", "REFUND", "refill_requested",
	"approval_requested",
}

type Service struct {
	pool       *pgxpool.Pool
	eventStore *eventstore.Store
}

func NewService(pool *pgxpool.Pool, es *eventstore.Store) *Service {
	return &Service{pool: pool, eventStore: es}
}

// Pending matches Nest ApprovalsService.getPending → web PendingApproval[].
func (s *Service) Pending(ctx context.Context, shopID string) ([]map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.event_type, e.item_id, e.quantity, e.total_cost::text,
		       e.created_at, e.payload, COALESCE(u.name, u.email, '')
		FROM "Event" e
		LEFT JOIN users u ON u.id = e.actor_user_id
		WHERE e.shop_id = $1
		  AND e.approved_by_id IS NULL
		  AND e.event_type = ANY($2)
		  AND COALESCE(e.payload->>'rejected','') <> 'true'
		ORDER BY e.created_at DESC
		LIMIT 50`, shopID, pendingEventTypes)
	if err != nil {
		return []map[string]interface{}{}, err
	}
	defer rows.Close()

	var out []map[string]interface{}
	for rows.Next() {
		var id, et string
		var itemID *string
		var qty *float64
		var totalStr *string
		var created time.Time
		var payload []byte
		var actorName string
		if err := rows.Scan(&id, &et, &itemID, &qty, &totalStr, &created, &payload, &actorName); err != nil {
			continue
		}
		var p map[string]interface{}
		_ = json.Unmarshal(payload, &p)
		if p == nil {
			p = map[string]interface{}{}
		}
		amount := 0.0
		if totalStr != nil && *totalStr != "" {
			fmt.Sscanf(*totalStr, "%f", &amount)
		}
		if amount == 0 {
			if v, ok := p["total_waste_value"].(float64); ok {
				amount = v
			} else if v, ok := p["total_cost"].(float64); ok {
				amount = v
			}
		}
		item := ""
		if itemID != nil {
			item = *itemID
		}
		if item == "" {
			if v, ok := p["item_id"].(string); ok {
				item = v
			}
		}
		name := ""
		if v, ok := p["item_name"].(string); ok {
			name = v
		}
		q := 0.0
		if qty != nil {
			q = *qty
		}
		typ := mapApprovalType(et)
		summary := humanSummary(et, name, item, q)
		if actorName == "" {
			actorName = "System"
		}
		out = append(out, map[string]interface{}{
			"id":          id,
			"type":        typ,
			"summary":     summary,
			"requestedBy": actorName,
			"requestedAt": created.UTC().Format(time.RFC3339),
			"amount":      amount,
			"event_type":  et,
			"item_id":     item,
			"status":      "pending",
		})
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}

func mapApprovalType(et string) string {
	e := strings.ToLower(et)
	switch {
	case strings.Contains(e, "waste"):
		return "waste"
	case strings.Contains(e, "receiv"):
		return "receiving"
	case strings.Contains(e, "refill"):
		return "refill"
	case strings.Contains(e, "variance"):
		return "variance"
	case strings.Contains(e, "refund"):
		return "refund"
	case strings.Contains(e, "adjust"):
		return "adjustment"
	default:
		return e
	}
}

func humanSummary(et, name, item string, qty float64) string {
	label := name
	if label == "" {
		label = item
	}
	if label == "" {
		label = strings.ReplaceAll(strings.ToLower(et), "_", " ")
	}
	if qty > 0 {
		return fmt.Sprintf("%s (%.2f)", label, qty)
	}
	return label
}

func (s *Service) History(ctx context.Context, shopID string, limit int) ([]map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.event_type, e.item_id, e.quantity, e.total_cost::text,
		       e.created_at, e.updated_at, e.approved_by_id, e.payload,
		       COALESCE(u.name, u.email, '')
		FROM "Event" e
		LEFT JOIN users u ON u.id = e.actor_user_id
		WHERE e.shop_id = $1
		  AND e.event_type = ANY($2)
		  AND (e.approved_by_id IS NOT NULL OR COALESCE(e.payload->>'rejected','') = 'true')
		ORDER BY e.updated_at DESC
		LIMIT $3`, shopID, pendingEventTypes, limit)
	if err != nil {
		return []map[string]interface{}{}, err
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var id, et string
		var itemID, approvedBy *string
		var qty *float64
		var totalStr *string
		var created, updated time.Time
		var payload []byte
		var actorName string
		if err := rows.Scan(&id, &et, &itemID, &qty, &totalStr, &created, &updated, &approvedBy, &payload, &actorName); err != nil {
			continue
		}
		var p map[string]interface{}
		_ = json.Unmarshal(payload, &p)
		status := "approved"
		if p != nil {
			if r, _ := p["rejected"].(bool); r {
				status = "rejected"
			}
			if s, ok := p["rejected"].(string); ok && s == "true" {
				status = "rejected"
			}
		}
		out = append(out, map[string]interface{}{
			"id": id, "event_type": et, "status": status,
			"item_id": itemID, "created_at": created, "updated_at": updated,
			"requestedBy": actorName, "payload": p,
		})
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}

func (s *Service) Decide(ctx context.Context, shopID, approvalID, actorUserID, decision, notes string) (map[string]interface{}, error) {
	if decision != "approve" && decision != "reject" {
		return nil, apperrors.BadRequest("decision must be approve or reject")
	}
	if actorUserID == "" {
		return nil, apperrors.Forbidden("actor required")
	}

	var approvedBy *string
	var payload []byte
	err := s.pool.QueryRow(ctx, `
		SELECT approved_by_id, payload FROM "Event" WHERE id = $1 AND shop_id = $2`,
		approvalID, shopID).Scan(&approvedBy, &payload)
	if err != nil {
		return nil, apperrors.NotFound("approval not found")
	}
	var p map[string]interface{}
	_ = json.Unmarshal(payload, &p)
	if p == nil {
		p = map[string]interface{}{}
	}
	if approvedBy != nil && *approvedBy != "" {
		if r, _ := p["rejected"].(bool); !r {
			return map[string]interface{}{"success": true, "message": "Already approved"}, nil
		}
	}

	if decision == "reject" {
		p["rejected"] = true
		p["approval_status"] = "rejected"
		p["rejection_reason"] = notes
		p["rejected_at"] = time.Now().UTC().Format(time.RFC3339)
		p["rejected_by"] = actorUserID
	} else {
		p["rejected"] = false
		p["approval_status"] = "approved"
		p["approved_at"] = time.Now().UTC().Format(time.RFC3339)
		p["approved_by"] = actorUserID
		delete(p, "rejection_reason")
	}
	b, _ := json.Marshal(p)
	_, err = s.pool.Exec(ctx, `
		UPDATE "Event" SET approved_by_id = $1, payload = $2::jsonb, updated_at = $3
		WHERE id = $4 AND shop_id = $5`,
		actorUserID, string(b), time.Now().UTC(), approvalID, shopID)
	if err != nil {
		return nil, err
	}

	evtType := eventstore.EventApprovalGranted
	if decision == "reject" {
		evtType = eventstore.EventApprovalRejected
	}
	actorPtr := &actorUserID
	_, _ = s.eventStore.Append(ctx, eventstore.AppendInput{
		EventType:      evtType,
		ActorUserID:    actorPtr,
		IdempotencyKey: fmt.Sprintf("approval-%s-%s", decision, approvalID),
		Payload: map[string]interface{}{
			"approval_id": approvalID, "decision": decision, "notes": notes,
			"shop_id": shopID,
		},
	})
	return map[string]interface{}{
		"success": true, "approvalId": approvalID, "decision": decision,
	}, nil
}

