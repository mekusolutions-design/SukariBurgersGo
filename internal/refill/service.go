package refill

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/eventstore"
	apperrors "github.com/mekusolutions-design/SukariBurgersGo/pkg/errors"
)

type Service struct {
	pool *pgxpool.Pool
	es   *eventstore.Store
}

func NewService(pool *pgxpool.Pool, es *eventstore.Store) *Service {
	return &Service{pool: pool, es: es}
}

func (s *Service) Pending(ctx context.Context, shopID string) ([]map[string]interface{}, error) {
	events, err := s.es.QueryByShopAndType(ctx, shopID, eventstore.EventRefillRequested, 100, 0)
	if err != nil {
		return []map[string]interface{}{}, nil
	}
	issued, _ := s.es.QueryByShopAndType(ctx, shopID, eventstore.EventRefillIssued, 200, 0)
	issuedIDs := map[string]bool{}
	for _, e := range issued {
		var p map[string]interface{}
		_ = json.Unmarshal(e.Payload, &p)
		if id, _ := p["request_id"].(string); id != "" {
			issuedIDs[id] = true
		}
	}
	var out []map[string]interface{}
	for _, e := range events {
		var p map[string]interface{}
		_ = json.Unmarshal(e.Payload, &p)
		rid, _ := p["request_id"].(string)
		if rid == "" {
			rid = e.ID
		}
		if issuedIDs[rid] {
			continue
		}
		p["event_id"] = e.ID
		p["created_at"] = e.CreatedAt
		out = append(out, p)
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}

func (s *Service) Get(ctx context.Context, shopID, requestID string) (map[string]interface{}, error) {
	pending, _ := s.Pending(ctx, shopID)
	for _, p := range pending {
		if p["request_id"] == requestID || p["event_id"] == requestID {
			return p, nil
		}
	}
	return nil, apperrors.NotFound("Refill request not found")
}

func (s *Service) Request(ctx context.Context, shopID, actorUserID, itemID string, qty float64, note, idem string) (map[string]interface{}, error) {
	if itemID == "" || qty <= 0 {
		return nil, apperrors.BadRequest("item_id and positive quantity required")
	}
	if shopID == "" {
		shopID = "1"
	}
	reqID := uuid.New().String()
	if idem == "" {
		idem = "refill-req-" + reqID
	}
	var actor *string
	if actorUserID != "" {
		actor = &actorUserID
	}
	payload := map[string]interface{}{
		"request_id": reqID, "shop_id": shopID, "item_id": itemID,
		"quantity": qty, "note": note, "status": "pending",
		"requested_at": time.Now().UTC().Format(time.RFC3339),
	}
	event, err := s.es.Append(ctx, eventstore.AppendInput{
		EventType: eventstore.EventRefillRequested, ActorUserID: actor,
		ItemID: &itemID, Quantity: &qty, IdempotencyKey: idem, Payload: payload,
	})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"success": true, "requestId": reqID, "eventId": event.ID}, nil
}

func (s *Service) Issue(ctx context.Context, shopID, actorUserID, requestID, itemID string, qty float64, idem string) (map[string]interface{}, error) {
	if itemID == "" || qty <= 0 {
		return nil, apperrors.BadRequest("item_id and positive quantity required")
	}
	if shopID == "" {
		shopID = "1"
	}
	if idem == "" {
		idem = "refill-issue-" + requestID + "-" + uuid.New().String()[:8]
	}
	var actor *string
	if actorUserID != "" {
		actor = &actorUserID
	}
	payload := map[string]interface{}{
		"request_id": requestID, "shop_id": shopID, "item_id": itemID,
		"quantity": qty, "status": "issued", "issued_at": time.Now().UTC().Format(time.RFC3339),
	}
	event, err := s.es.Append(ctx, eventstore.AppendInput{
		EventType: eventstore.EventRefillIssued, ActorUserID: actor,
		ItemID: &itemID, Quantity: &qty, IdempotencyKey: idem, Payload: payload,
	})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"success": true, "eventId": event.ID}, nil
}
