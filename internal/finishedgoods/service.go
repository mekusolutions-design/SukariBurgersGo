package finishedgoods

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/restflow/api-go/internal/eventstore"
	"github.com/restflow/api-go/pkg/money"
	apperrors "github.com/restflow/api-go/pkg/errors"
)

type Service struct {
	pool *pgxpool.Pool
	es   *eventstore.Store
}

func NewService(pool *pgxpool.Pool, es *eventstore.Store) *Service {
	return &Service{pool: pool, es: es}
}

func (s *Service) List(ctx context.Context, shopID string) ([]map[string]interface{}, error) {
	if shopID == "" {
		shopID = "1"
	}
	rows, err := s.pool.Query(ctx, `
		SELECT p.item_id, COALESCE(i.name, p.item_id), p.available_stock, p.total_value::text
		FROM "InventoryProjection" p
		LEFT JOIN "Item" i ON i.item_id = p.item_id
		WHERE p.shop_id = $1
		ORDER BY i.name NULLS LAST`, shopID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var id, name, val string
		var stock float64
		_ = rows.Scan(&id, &name, &stock, &val)
		v := 0.0
		if m, e := money.New(val); e == nil {
			v = m.Float64()
		}
		out = append(out, map[string]interface{}{
			"item_id": id, "name": name, "available_stock": stock, "total_value": v,
		})
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}

type AddInput struct {
	ShopID, ItemID, ItemName, Units, ActorUserID, IdempotencyKey string
	Quantity, UnitCost                                           float64
}

func (s *Service) Add(ctx context.Context, in AddInput) (map[string]interface{}, error) {
	if in.ItemID == "" || in.Quantity <= 0 {
		return nil, apperrors.BadRequest("item_id and positive quantity required")
	}
	if in.ShopID == "" {
		in.ShopID = "1"
	}
	if in.IdempotencyKey == "" {
		in.IdempotencyKey = "fg-add-" + uuid.New().String()
	}
	var actor *string
	if in.ActorUserID != "" {
		actor = &in.ActorUserID
	}
	uc := money.FromFloatUnitCost(in.UnitCost)
	tc := uc.MulQty(in.Quantity).AsTotal()
	payload := map[string]interface{}{
		"shop_id": in.ShopID, "item_id": in.ItemID, "item_name": in.ItemName,
		"quantity": in.Quantity, "unit_cost": uc.Float64(), "total_cost": tc.Float64(),
		"units": in.Units,
	}
	itemID := in.ItemID
	qty := in.Quantity
	event, err := s.es.Append(ctx, eventstore.AppendInput{
		EventType: eventstore.EventFinishedGoodAdded, ActorUserID: actor,
		ItemID: &itemID, Quantity: &qty, UnitCost: &uc, TotalCost: &tc,
		IdempotencyKey: in.IdempotencyKey, Payload: payload,
	})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"success": true, "eventId": event.ID}, nil
}

type AdjustInput struct {
	ShopID, ItemID, Reason, ActorUserID, IdempotencyKey string
	QuantityDelta                                       float64
}

func (s *Service) Adjust(ctx context.Context, in AdjustInput) (map[string]interface{}, error) {
	if in.ItemID == "" || in.QuantityDelta == 0 {
		return nil, apperrors.BadRequest("item_id and non-zero quantity_delta required")
	}
	if in.ShopID == "" {
		in.ShopID = "1"
	}
	if in.IdempotencyKey == "" {
		in.IdempotencyKey = "fg-adj-" + uuid.New().String()
	}
	var actor *string
	if in.ActorUserID != "" {
		actor = &in.ActorUserID
	}
	itemID := in.ItemID
	qty := in.QuantityDelta
	payload := map[string]interface{}{
		"shop_id": in.ShopID, "item_id": in.ItemID, "quantity_delta": in.QuantityDelta,
		"quantity": in.QuantityDelta, "reason": in.Reason, "adjusted_at": time.Now().UTC().Format(time.RFC3339),
	}
	event, err := s.es.Append(ctx, eventstore.AppendInput{
		EventType: eventstore.EventFinishedGoodAdjusted, ActorUserID: actor,
		ItemID: &itemID, Quantity: &qty, IdempotencyKey: in.IdempotencyKey, Payload: payload,
	})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"success": true, "eventId": event.ID}, nil
}
