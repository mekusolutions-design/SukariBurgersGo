package receiving

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/restflow/api-go/internal/eventstore"
	"github.com/restflow/api-go/internal/locking"
	"github.com/restflow/api-go/pkg/money"
	apperrors "github.com/restflow/api-go/pkg/errors"
)

type Service struct {
	pool       *pgxpool.Pool
	eventStore *eventstore.Store
}

func NewService(pool *pgxpool.Pool, es *eventstore.Store) *Service {
	return &Service{pool: pool, eventStore: es}
}

type ReceiveInput struct {
	ItemID         string
	ItemName       string
	Units          string
	Category       string
	Quantity       float64
	QuantityApproved *float64 // if nil, use Quantity
	UnitCost       float64
	TotalCost      *float64
	BatchNumber    string
	ExpiryDate     *time.Time
	DateReceived   string
	SupplierName  string
	SupplierNumber string
	SupplierID     string
	ApprovedBy     string
	Reference      string
	WasteReason    string
	WastePhotoURL  string
	ShopID         string
	ActorUserID    string
	IdempotencyKey string
}

type ReceiveResult struct {
	Success bool   `json:"success"`
	EventID string `json:"eventId"`
	Message string `json:"message,omitempty"`
}

func (s *Service) Receive(ctx context.Context, in ReceiveInput) (*ReceiveResult, error) {
	if in.ItemID == "" && in.ItemName == "" {
		return nil, apperrors.BadRequest("item_id or item_name is required")
	}
	if in.Quantity <= 0 {
		return nil, apperrors.BadRequest("quantity must be positive")
	}
	if in.IdempotencyKey == "" {
		in.IdempotencyKey = uuid.New().String()
	}
	if in.ShopID == "" {
		in.ShopID = "1"
	}
	if in.Units == "" {
		in.Units = "unit"
	}

	itemID := in.ItemID
	if itemID == "" {
		itemID = in.ItemName // fallback; proper SKU generation later
	}

	approved := in.Quantity
	if in.QuantityApproved != nil {
		approved = *in.QuantityApproved
	}
	if approved < 0 {
		return nil, apperrors.BadRequest("quantity_approved cannot be negative")
	}
	rejected := in.Quantity - approved
	if rejected < 0 {
		rejected = 0
	}

	unitCost := money.FromFloatUnitCost(in.UnitCost)
	var totalCost money.Money
	if in.TotalCost != nil {
		totalCost = money.FromFloat(*in.TotalCost).AsTotal()
	} else {
		totalCost = unitCost.MulQty(approved).AsTotal()
	}

	// Use advisory lock for the shop during receive
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := locking.AcquireShopLock(ctx, tx, in.ShopID); err != nil {
		return nil, err
	}
	// Commit the lock tx early — event store opens its own tx.
	// For stronger atomicity we could pass the tx into event store later.
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	actor := in.ActorUserID
	var actorPtr *string
	if actor != "" {
		actorPtr = &actor
	}

	var batchPtr *string
	if in.BatchNumber != "" {
		batchPtr = &in.BatchNumber
	}
	var wasteReason, wastePhoto *string
	if in.WasteReason != "" {
		wasteReason = &in.WasteReason
	}
	if in.WastePhotoURL != "" {
		wastePhoto = &in.WastePhotoURL
	}

	payload := map[string]interface{}{
		"item_id":           itemID,
		"item_name":         in.ItemName,
		"units":             in.Units,
		"category":          in.Category,
		"quantity":          in.Quantity,
		"quantity_approved": approved,
		"quantity_rejected": rejected,
		"date_received":     in.DateReceived,
		"unit_cost":         unitCost.Float64(),
		"total_cost":        totalCost.Float64(),
		"supplier_name":     in.SupplierName,
		"supplier_number":   in.SupplierNumber,
		"supplier_id":       in.SupplierID,
		"approved_by":       in.ApprovedBy,
		"batch_number":      in.BatchNumber,
		"reference":         in.Reference,
		"shop_id":           in.ShopID,
	}
	if in.ExpiryDate != nil {
		payload["expiry_date"] = in.ExpiryDate.Format(time.RFC3339)
	}

	// Nest ensureItemExists — Event.item_id FK requires Item row
	if err := s.ensureItemExists(ctx, itemID, in.ItemName, in.Units, in.Category); err != nil {
		return nil, err
	}

	event, err := s.eventStore.Append(ctx, eventstore.AppendInput{
		EventType:      eventstore.EventReceived,
		ActorUserID:    actorPtr,
		IdempotencyKey: in.IdempotencyKey,
		ItemID:         &itemID,
		BatchNumber:    batchPtr,
		ExpiryDate:     in.ExpiryDate,
		Quantity:       &approved,
		UnitCost:       &unitCost,
		TotalCost:      &totalCost,
		WasteReason:    wasteReason,
		WastePhotoURL:  wastePhoto,
		Payload:        payload,
	})
	if err != nil {
		return nil, err
	}

	return &ReceiveResult{
		Success: true,
		EventID: event.ID,
	}, nil
}


func (s *Service) ListRecent(ctx context.Context, shopID string, limit int) ([]map[string]interface{}, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, event_type, item_id, quantity, total_cost::text, batch_number, created_at,
		       COALESCE(payload::text, '{}')
		FROM "Event"
		WHERE shop_id = $1 AND event_type IN ('goods_received','GOODS_RECEIVED','stock_received')
		ORDER BY created_at DESC
		LIMIT $2`, shopID, limit)
	if err != nil {
		return []map[string]interface{}{}, nil
	}
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var id, et, itemID, tc, bn, payload string
		var qty *float64
		var created interface{}
		if rows.Scan(&id, &et, &itemID, &qty, &tc, &bn, &created, &payload) != nil {
			continue
		}
		out = append(out, map[string]interface{}{
			"id": id, "event_type": et, "item_id": itemID, "quantity": qty,
			"total_cost": tc, "batch_number": bn, "created_at": created,
		})
	}
	if out == nil {
		out = []map[string]interface{}{}
	}
	return out, nil
}

// ensureItemExists mirrors Nest received.service ensureItemExists.
// ensureItemExists mirrors Nest received.service ensureItemExists.
// Item columns: id, item_id, name, description, unit, category, finished_good_category_id,
// min_stock, reorder_point, created_at, updated_at — NO is_active.
func (s *Service) ensureItemExists(ctx context.Context, itemID, itemName, units, category string) error {
	if itemID == "" {
		return apperrors.BadRequest("item_id is required")
	}
	var exists string
	err := s.pool.QueryRow(ctx, `SELECT item_id FROM "Item" WHERE item_id = $1`, itemID).Scan(&exists)
	if err == nil {
		// Refresh name/unit/category when provided (Nest update path)
		_, _ = s.pool.Exec(ctx, `
			UPDATE "Item" SET
				name = CASE WHEN $2 <> '' THEN $2 ELSE name END,
				unit = CASE WHEN $3 <> '' THEN $3 ELSE unit END,
				category = CASE WHEN $4 <> '' THEN $4 ELSE category END,
				updated_at = $5
			WHERE item_id = $1`,
			itemID, itemName, units, category, time.Now().UTC())
		return nil
	}
	if category == "" {
		return apperrors.BadRequest("Category is required when creating a new item on receive")
	}
	if itemName == "" {
		itemName = itemID
	}
	if units == "" {
		units = "units"
	}
	id := uuid.New().String()
	now := time.Now().UTC()
	// Default reorder_point ~20% of typical receive is applied later by projection/seed; Nest uses defaults 0.
	_, err = s.pool.Exec(ctx, `
		INSERT INTO "Item" (id, item_id, name, unit, category, min_stock, reorder_point, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, 0, 0, $6, $6)
		ON CONFLICT (item_id) DO UPDATE SET
			name = EXCLUDED.name,
			unit = COALESCE(EXCLUDED.unit, "Item".unit),
			category = COALESCE(EXCLUDED.category, "Item".category),
			updated_at = EXCLUDED.updated_at`,
		id, itemID, itemName, units, category, now)
	if err != nil {
		return fmt.Errorf("create item: %w", err)
	}
	return nil
}
