package sync

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/eventstore"
	apperrors "github.com/mekusolutions-design/SukariBurgersGo/pkg/errors"
)

type Service struct {
	pool       *pgxpool.Pool
	eventStore *eventstore.Store
}

func NewService(pool *pgxpool.Pool, es *eventstore.Store) *Service {
	return &Service{pool: pool, eventStore: es}
}

type BulkEvent struct {
	EventType      string                 `json:"event_type"`
	IdempotencyKey string                 `json:"idempotency_key"`
	Payload        map[string]interface{} `json:"payload"`
	ClientID       string                 `json:"client_id"`
}

type BulkResult struct {
	Accepted int                      `json:"accepted"`
	Replayed int                      `json:"replayed"`
	Failed   int                      `json:"failed"`
	Results  []map[string]interface{} `json:"results"`
}

func (s *Service) Bulk(ctx context.Context, shopID, actorUserID string, events []BulkEvent) (*BulkResult, error) {
	if len(events) == 0 {
		return nil, apperrors.BadRequest("events array required")
	}
	if len(events) > 100 {
		return nil, apperrors.BadRequest("max 100 events per bulk sync")
	}
	if shopID == "" {
		shopID = "1"
	}
	var actorPtr *string
	if actorUserID != "" {
		actorPtr = &actorUserID
	}
	result := &BulkResult{Results: []map[string]interface{}{}}
	for _, ev := range events {
		if ev.EventType == "" {
			result.Failed++
			result.Results = append(result.Results, map[string]interface{}{"error": "event_type required", "client_id": ev.ClientID})
			continue
		}
		key := ev.IdempotencyKey
		if key == "" {
			key = "sync-" + uuid.New().String()
		}
		payload := ev.Payload
		if payload == nil {
			payload = map[string]interface{}{}
		}
		payload["shop_id"] = shopID
		if ev.ClientID != "" {
			payload["client_id"] = ev.ClientID
		}
		existing, _ := s.eventStore.FindByIdempotencyKey(ctx, key)
		if existing != nil {
			result.Replayed++
			result.Results = append(result.Results, map[string]interface{}{
				"client_id": ev.ClientID, "event_id": existing.ID, "status": "replayed",
			})
			continue
		}
		event, err := s.eventStore.Append(ctx, eventstore.AppendInput{
			EventType: ev.EventType, ActorUserID: actorPtr, IdempotencyKey: key, Payload: payload,
		})
		if err != nil {
			result.Failed++
			result.Results = append(result.Results, map[string]interface{}{
				"client_id": ev.ClientID, "error": err.Error(), "status": "failed",
			})
			continue
		}
		result.Accepted++
		result.Results = append(result.Results, map[string]interface{}{
			"client_id": ev.ClientID, "event_id": event.ID, "status": "accepted",
		})
		// Ack event
		_, _ = s.eventStore.Append(ctx, eventstore.AppendInput{
			EventType: eventstore.EventClientSyncAck, ActorUserID: actorPtr,
			IdempotencyKey: fmt.Sprintf("sync-ack-%s", event.ID),
			Payload:        map[string]interface{}{"event_id": event.ID, "client_id": ev.ClientID, "shop_id": shopID},
		})
	}
	return result, nil
}
