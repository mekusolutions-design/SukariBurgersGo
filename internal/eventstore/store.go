package eventstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/restflow/api-go/internal/locking"
	"github.com/restflow/api-go/pkg/money"
	apperrors "github.com/restflow/api-go/pkg/errors"
)

var (
	ErrIdempotencyConflict = errors.New("idempotency key already used with different payload")
	ErrEventNotFound       = errors.New("event not found")
)

// Store is the Go event store. It preserves atomic append + projection semantics.
type Store struct {
	pool   *pgxpool.Pool
	log    *slog.Logger
	// ProjectionRunner is called inside the same transaction after the event is written.
	// It must be idempotent and transactional.
	ProjectionRunner ProjectionRunner
}

type ProjectionRunner interface {
	UpdateProjections(ctx context.Context, tx pgx.Tx, event *Event) error
}

func NewStore(pool *pgxpool.Pool, log *slog.Logger) *Store {
	return &Store{pool: pool, log: log}
}

func (s *Store) SetProjectionRunner(r ProjectionRunner) {
	s.ProjectionRunner = r
}

// Append writes an event with full idempotency and optional projection update
// inside a single transaction (mirrors NestJS EventStoreService.appendEvent).
func (s *Store) Append(ctx context.Context, in AppendInput) (*Event, error) {
	if in.IdempotencyKey == "" {
		return nil, apperrors.BadRequest("idempotency_key is required")
	}
	if in.EventType == "" {
		return nil, apperrors.BadRequest("event_type is required")
	}

	// Fast path: check outside the write lock
	existing, err := s.FindByIdempotencyKey(ctx, in.IdempotencyKey)
	if err != nil && !errors.Is(err, ErrEventNotFound) {
		return nil, err
	}
	if existing != nil {
		if payloadsEqual(existing.Payload, in.Payload) {
			s.log.Debug("idempotent replay", "key", in.IdempotencyKey)
			return existing, nil
		}
		return nil, apperrors.Conflict("Idempotency key already used with different payload")
	}

	payload := in.Payload
	if payload == nil {
		payload = map[string]interface{}{}
	}

	// Normalize shop_id into payload — never invent shop "1" for mutating events
	shopID := ""
	if v, ok := payload["shop_id"].(string); ok && v != "" {
		shopID = v
	} else if v, ok := payload["shopId"].(string); ok && v != "" {
		shopID = v
	}
	if shopID == "" {
		// Inventory/financial events must carry authoritative shop
		switch in.EventType {
		case EventPosSale, EventReceived, EventWasteRecorded, EventProductionFinished,
			EventFinishedGoodAdded, EventFinishedGoodAdjusted, EventRefillIssued,
			EventClosingStockCounted, EventMpesaStkInitiated, EventOrderPaymentUpdated:
			return nil, apperrors.BadRequest("shop_id required on event payload")
		default:
			shopID = "1" // read-model / non-mutating fallback only
		}
	}
	payload["shop_id"] = shopID

	// Normalize business item_id (SKU)
	var businessItemID *string
	if in.ItemID != nil && *in.ItemID != "" {
		id := normalizeSKU(*in.ItemID)
		businessItemID = &id
		payload["item_id"] = id
	} else if v, ok := payload["item_id"].(string); ok && v != "" {
		id := normalizeSKU(v)
		businessItemID = &id
		payload["item_id"] = id
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Shop-scoped advisory lock for inventory-affecting events (prevents concurrent oversell races)
	if in.EventType == EventPosSale || in.EventType == EventReceived ||
		in.EventType == EventWasteRecorded || in.EventType == EventProductionFinished {
		if err := locking.AcquireShopLock(ctx, tx, shopID); err != nil {
			return nil, err
		}
	}

	// Re-check inside transaction
	existing, err = s.findByIdempotencyKeyTx(ctx, tx, in.IdempotencyKey)
	if err != nil && !errors.Is(err, ErrEventNotFound) {
		return nil, err
	}
	if existing != nil {
		if payloadsEqual(existing.Payload, in.Payload) {
			return existing, nil
		}
		return nil, apperrors.Conflict("Idempotency key already used with different payload")
	}

	id := uuid.New().String()
	now := time.Now().UTC()

	var unitCost, totalCost *string
	if in.UnitCost != nil {
		s := in.UnitCost.String()
		unitCost = &s
	}
	if in.TotalCost != nil {
		s := in.TotalCost.String()
		totalCost = &s
	}

	const q = `
		INSERT INTO "Event" (
			id, event_type, shop_id, actor_user_id, idempotency_key,
			item_id, batch_number, expiry_date, quantity,
			unit_cost, total_cost, supplier_id, approved_by_id,
			waste_reason, waste_photo_url, payload, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5,
			$6, $7, $8, $9,
			$10, $11, $12, $13,
			$14, $15, $16, $17, $18
		)
		RETURNING id, event_type, shop_id, actor_user_id, idempotency_key,
			item_id, batch_number, expiry_date, quantity,
			unit_cost::text, total_cost::text, supplier_id, approved_by_id,
			waste_reason, waste_photo_url, payload, created_at, updated_at
	`

	// insertEvent runs INSERT under a SAVEPOINT so FK failures can be retried
	// without aborting the outer transaction (avoids SQLSTATE 25P02).
	insertEvent := func(actor *string, supplier *string, approved *string) (*Event, error) {
		if _, err := tx.Exec(ctx, `SAVEPOINT event_insert`); err != nil {
			return nil, err
		}
		row := tx.QueryRow(ctx, q,
			id, in.EventType, shopID, actor, in.IdempotencyKey,
			businessItemID, in.BatchNumber, in.ExpiryDate, in.Quantity,
			unitCost, totalCost, supplier, approved,
			in.WasteReason, in.WastePhotoURL, string(payloadBytes), now, now,
		)
		ev, err := scanEvent(row)
		if err != nil {
			_, _ = tx.Exec(ctx, `ROLLBACK TO SAVEPOINT event_insert`)
			return nil, err
		}
		_, _ = tx.Exec(ctx, `RELEASE SAVEPOINT event_insert`)
		return ev, nil
	}

	event, err := insertEvent(in.ActorUserID, in.SupplierID, in.ApprovedByID)
	if err != nil {
		if isUniqueViolation(err) {
			again, e2 := s.findByIdempotencyKeyTx(ctx, tx, in.IdempotencyKey)
			if e2 == nil && again != nil && payloadsEqual(again.Payload, in.Payload) {
				return again, nil
			}
			return nil, apperrors.Conflict("Idempotency key already used with different payload")
		}
		errStr := err.Error()
		// Retry without invalid actor FK
		if in.ActorUserID != nil && (contains(errStr, "actor_user_id") || contains(errStr, "ActorEvents") || contains(errStr, "foreign key")) {
			event, err = insertEvent(nil, in.SupplierID, in.ApprovedByID)
			errStr = ""
			if err != nil {
				errStr = err.Error()
			}
		}
		// Retry without invalid supplier / approver FK
		if err != nil && (contains(errStr, "supplier") || contains(errStr, "approved_by") || contains(errStr, "foreign key")) {
			event, err = insertEvent(nil, nil, nil)
			if err != nil {
				errStr = err.Error()
			}
		}
		if err != nil {
			if contains(err.Error(), "item_id") || contains(err.Error(), "Item_item_id") {
				return nil, apperrors.BadRequest(fmt.Sprintf(
					"item_id is not registered in Item table — receive/create the SKU first (%v)", err))
			}
			return nil, fmt.Errorf("insert event: %w", err)
		}
	}

	if s.ProjectionRunner != nil {
		if err := s.ProjectionRunner.UpdateProjections(ctx, tx, event); err != nil {
			if s.log != nil {
				s.log.Error("projection update failed", "event_id", event.ID, "type", event.EventType, "error", err)
			}
			// Fail the transaction so POS cannot "succeed" without stock/revenue updates
			return nil, fmt.Errorf("projection update: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return event, nil
}

func (s *Store) FindByIdempotencyKey(ctx context.Context, key string) (*Event, error) {
	const q = `
		SELECT id, event_type, shop_id, actor_user_id, idempotency_key,
			item_id, batch_number, expiry_date, quantity,
			unit_cost::text, total_cost::text, supplier_id, approved_by_id,
			waste_reason, waste_photo_url, payload, created_at, updated_at
		FROM "Event"
		WHERE idempotency_key = $1
	`
	row := s.pool.QueryRow(ctx, q, key)
	return scanEvent(row)
}

func (s *Store) findByIdempotencyKeyTx(ctx context.Context, tx pgx.Tx, key string) (*Event, error) {
	const q = `
		SELECT id, event_type, shop_id, actor_user_id, idempotency_key,
			item_id, batch_number, expiry_date, quantity,
			unit_cost::text, total_cost::text, supplier_id, approved_by_id,
			waste_reason, waste_photo_url, payload, created_at, updated_at
		FROM "Event"
		WHERE idempotency_key = $1
	`
	row := tx.QueryRow(ctx, q, key)
	return scanEvent(row)
}

func (s *Store) GetByID(ctx context.Context, id string) (*Event, error) {
	const q = `
		SELECT id, event_type, shop_id, actor_user_id, idempotency_key,
			item_id, batch_number, expiry_date, quantity,
			unit_cost::text, total_cost::text, supplier_id, approved_by_id,
			waste_reason, waste_photo_url, payload, created_at, updated_at
		FROM "Event"
		WHERE id = $1
	`
	row := s.pool.QueryRow(ctx, q, id)
	return scanEvent(row)
}

// QueryByShopAndType returns events for a shop filtered by event type, ordered by created_at.
func (s *Store) QueryByShopAndType(ctx context.Context, shopID, eventType string, limit, offset int) ([]*Event, error) {
	const q = `
		SELECT id, event_type, shop_id, actor_user_id, idempotency_key,
			item_id, batch_number, expiry_date, quantity,
			unit_cost::text, total_cost::text, supplier_id, approved_by_id,
			waste_reason, waste_photo_url, payload, created_at, updated_at
		FROM "Event"
		WHERE shop_id = $1 AND event_type = $2
		ORDER BY created_at DESC
		LIMIT $3 OFFSET $4
	`
	rows, err := s.pool.Query(ctx, q, shopID, eventType, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

type scannable interface {
	Scan(dest ...any) error
}

func scanEvent(row scannable) (*Event, error) {
	var e Event
	var unitCostStr, totalCostStr *string
	var payloadBytes []byte

	err := row.Scan(
		&e.ID, &e.EventType, &e.ShopID, &e.ActorUserID, &e.IdempotencyKey,
		&e.ItemID, &e.BatchNumber, &e.ExpiryDate, &e.Quantity,
		&unitCostStr, &totalCostStr, &e.SupplierID, &e.ApprovedByID,
		&e.WasteReason, &e.WastePhotoURL, &payloadBytes, &e.CreatedAt, &e.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrEventNotFound
		}
		return nil, err
	}

	e.Payload = payloadBytes
	if unitCostStr != nil {
		m, err := money.New(*unitCostStr)
		if err == nil {
			e.UnitCost = &m
		}
	}
	if totalCostStr != nil {
		m, err := money.New(*totalCostStr)
		if err == nil {
			e.TotalCost = &m
		}
	}
	return &e, nil
}

func payloadsEqual(existing json.RawMessage, current map[string]interface{}) bool {
	if current == nil {
		current = map[string]interface{}{}
	}
	curBytes, err := json.Marshal(current)
	if err != nil {
		return false
	}
	// Normalize both via map for stable comparison
	var a, b map[string]interface{}
	if err := json.Unmarshal(existing, &a); err != nil {
		return false
	}
	if err := json.Unmarshal(curBytes, &b); err != nil {
		return false
	}
	ab, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	return string(ab) == string(bb)
}

func normalizeSKU(s string) string {
	// Keep simple trim for now; TS uses normalizeSku helper
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}

func isUniqueViolation(err error) bool {
	// pgx wraps pgconn.PgError
	return err != nil && (contains(err.Error(), "23505") || contains(err.Error(), "unique constraint") || contains(err.Error(), "duplicate key"))
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

// QueryEventsASC streams events oldest-first for projection rebuild.
// Use afterCreatedAt + afterID as a stable cursor (exclusive).
func (s *Store) QueryEventsASC(ctx context.Context, shopID string, afterCreatedAt *time.Time, afterID string, limit int) ([]*Event, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	var rows pgx.Rows
	var err error
	if shopID == "" || shopID == "*" {
		if afterCreatedAt != nil && afterID != "" {
			rows, err = s.pool.Query(ctx, `
				SELECT id, event_type, shop_id, actor_user_id, idempotency_key,
					item_id, batch_number, expiry_date, quantity,
					unit_cost::text, total_cost::text, supplier_id, approved_by_id,
					waste_reason, waste_photo_url, payload, created_at, updated_at
				FROM "Event"
				WHERE (created_at > $1) OR (created_at = $1 AND id > $2)
				ORDER BY created_at ASC, id ASC
				LIMIT $3
			`, *afterCreatedAt, afterID, limit)
		} else {
			rows, err = s.pool.Query(ctx, `
				SELECT id, event_type, shop_id, actor_user_id, idempotency_key,
					item_id, batch_number, expiry_date, quantity,
					unit_cost::text, total_cost::text, supplier_id, approved_by_id,
					waste_reason, waste_photo_url, payload, created_at, updated_at
				FROM "Event"
				ORDER BY created_at ASC, id ASC
				LIMIT $1
			`, limit)
		}
	} else {
		if afterCreatedAt != nil && afterID != "" {
			rows, err = s.pool.Query(ctx, `
				SELECT id, event_type, shop_id, actor_user_id, idempotency_key,
					item_id, batch_number, expiry_date, quantity,
					unit_cost::text, total_cost::text, supplier_id, approved_by_id,
					waste_reason, waste_photo_url, payload, created_at, updated_at
				FROM "Event"
				WHERE shop_id = $1
				  AND ((created_at > $2) OR (created_at = $2 AND id > $3))
				ORDER BY created_at ASC, id ASC
				LIMIT $4
			`, shopID, *afterCreatedAt, afterID, limit)
		} else {
			rows, err = s.pool.Query(ctx, `
				SELECT id, event_type, shop_id, actor_user_id, idempotency_key,
					item_id, batch_number, expiry_date, quantity,
					unit_cost::text, total_cost::text, supplier_id, approved_by_id,
					waste_reason, waste_photo_url, payload, created_at, updated_at
				FROM "Event"
				WHERE shop_id = $1
				ORDER BY created_at ASC, id ASC
				LIMIT $2
			`, shopID, limit)
		}
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// CountEvents returns approximate event count for progress.
func (s *Store) CountEvents(ctx context.Context, shopID string) (int64, error) {
	var n int64
	var err error
	if shopID == "" || shopID == "*" {
		err = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM "Event"`).Scan(&n)
	} else {
		err = s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM "Event" WHERE shop_id = $1`, shopID).Scan(&n)
	}
	return n, err
}

