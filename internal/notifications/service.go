package notifications

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/eventstore"
	apperrors "github.com/mekusolutions-design/SukariBurgersGo/pkg/errors"
)

type PushProvider interface {
	Send(ctx context.Context, userID, title, body string, data map[string]interface{}) error
}

type noopPush struct{}

func (noopPush) Send(ctx context.Context, userID, title, body string, data map[string]interface{}) error {
	return nil
}

type Service struct {
	pool       *pgxpool.Pool
	eventStore *eventstore.Store
	push       PushProvider
}

func NewService(pool *pgxpool.Pool, es *eventstore.Store) *Service {
	return &Service{pool: pool, eventStore: es, push: noopPush{}}
}

func (s *Service) SetPushProvider(p PushProvider) { s.push = p }

type CreateInput struct {
	ShopID, ActorUserID, UserID, Title, Body, IdempotencyKey string
	Data                                                     map[string]interface{}
}

func (s *Service) Create(ctx context.Context, in CreateInput) (map[string]interface{}, error) {
	if in.Title == "" {
		return nil, apperrors.BadRequest("title required")
	}
	if in.ShopID == "" {
		in.ShopID = "1"
	}
	if in.IdempotencyKey == "" {
		in.IdempotencyKey = "notif-" + uuid.New().String()
	}
	var actorPtr *string
	if in.ActorUserID != "" {
		actorPtr = &in.ActorUserID
	}
	payload := map[string]interface{}{
		"user_id": in.UserID, "title": in.Title, "body": in.Body,
		"data": in.Data, "shop_id": in.ShopID, "read": false,
		"created_at": time.Now().UTC().Format(time.RFC3339),
	}
	event, err := s.eventStore.Append(ctx, eventstore.AppendInput{
		EventType: eventstore.EventNotificationCreated, ActorUserID: actorPtr,
		IdempotencyKey: in.IdempotencyKey, Payload: payload,
	})
	if err != nil {
		return nil, err
	}
	if in.UserID != "" {
		_ = s.push.Send(ctx, in.UserID, in.Title, in.Body, in.Data)
	}
	return map[string]interface{}{"success": true, "eventId": event.ID}, nil
}

func (s *Service) ListForUser(ctx context.Context, shopID, userID string, limit int) ([]map[string]interface{}, error) {
	if limit <= 0 {
		limit = 50
	}
	events, err := s.eventStore.QueryByShopAndType(ctx, shopID, eventstore.EventNotificationCreated, limit*2, 0)
	if err != nil {
		return nil, err
	}
	var out []map[string]interface{}
	for _, e := range events {
		var p map[string]interface{}
		_ = json.Unmarshal(e.Payload, &p)
		if uid, _ := p["user_id"].(string); userID != "" && uid != "" && uid != userID {
			continue
		}
		p["event_id"] = e.ID
		p["created_at"] = e.CreatedAt
		out = append(out, p)
		if len(out) >= limit {
			break
		}
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}
