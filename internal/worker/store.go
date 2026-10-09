package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store persists BackgroundJob rows.
type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// EnsureSchema creates tables if missing (dev / first deploy).
func (s *Store) EnsureSchema(ctx context.Context) error {
	_, err := s.pool.Exec(ctx, `
CREATE TABLE IF NOT EXISTS "BackgroundJob" (
  id            TEXT PRIMARY KEY,
  type          TEXT NOT NULL,
  shop_id       TEXT,
  payload       JSONB NOT NULL DEFAULT '{}',
  status        TEXT NOT NULL DEFAULT 'queued',
  progress      JSONB,
  result        JSONB,
  error_message TEXT,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  started_at    TIMESTAMPTZ,
  finished_at   TIMESTAMPTZ,
  created_by    TEXT,
  attempts      INT NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_background_job_status_created
  ON "BackgroundJob" (status, created_at ASC);
CREATE TABLE IF NOT EXISTS "ReconcileMismatch" (
  id          TEXT PRIMARY KEY,
  shop_id     TEXT NOT NULL,
  kind        TEXT NOT NULL,
  ref_key     TEXT,
  expected    DOUBLE PRECISION,
  actual      DOUBLE PRECISION,
  severity    TEXT NOT NULL DEFAULT 'warning',
  detail      JSONB,
  job_id      TEXT,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
`)
	return err
}

// Enqueue inserts a queued job.
func (s *Store) Enqueue(ctx context.Context, jobType string, shopID *string, payload map[string]interface{}, createdBy *string) (*Job, error) {
	if payload == nil {
		payload = map[string]interface{}{}
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	id := uuid.New().String()
	now := time.Now().UTC()
	_, err = s.pool.Exec(ctx, `
		INSERT INTO "BackgroundJob" (id, type, shop_id, payload, status, created_at, created_by)
		VALUES ($1, $2, $3, $4::jsonb, $5, $6, $7)
	`, id, jobType, shopID, string(b), StatusQueued, now, createdBy)
	if err != nil {
		return nil, fmt.Errorf("enqueue: %w", err)
	}
	return &Job{
		ID: id, Type: jobType, ShopID: shopID, Payload: payload,
		Status: StatusQueued, CreatedAt: now, CreatedBy: createdBy,
	}, nil
}

// ClaimNext takes the oldest queued job (SKIP LOCKED).
func (s *Store) ClaimNext(ctx context.Context) (*Job, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	row := tx.QueryRow(ctx, `
		SELECT id, type, shop_id, payload::text, status, progress::text, result::text,
		       error_message, created_at, started_at, finished_at, created_by, attempts
		FROM "BackgroundJob"
		WHERE status = $1
		ORDER BY created_at ASC
		FOR UPDATE SKIP LOCKED
		LIMIT 1
	`, StatusQueued)

	j, err := scanJob(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	_, err = tx.Exec(ctx, `
		UPDATE "BackgroundJob"
		SET status = $1, started_at = $2, attempts = attempts + 1
		WHERE id = $3
	`, StatusRunning, now, j.ID)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	j.Status = StatusRunning
	j.StartedAt = &now
	j.Attempts++
	return j, nil
}

func (s *Store) Get(ctx context.Context, id string) (*Job, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, type, shop_id, payload::text, status, progress::text, result::text,
		       error_message, created_at, started_at, finished_at, created_by, attempts
		FROM "BackgroundJob" WHERE id = $1
	`, id)
	return scanJob(row)
}

func (s *Store) MarkRunning(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE "BackgroundJob"
		SET status = $1, started_at = NOW(), attempts = attempts + 1
		WHERE id = $2
	`, StatusRunning, id)
	return err
}

func (s *Store) UpdateProgress(ctx context.Context, id string, progress map[string]interface{}) error {
	b, _ := json.Marshal(progress)
	_, err := s.pool.Exec(ctx, `
		UPDATE "BackgroundJob" SET progress = $1::jsonb WHERE id = $2
	`, string(b), id)
	return err
}

func (s *Store) MarkSucceeded(ctx context.Context, id string, result map[string]interface{}) error {
	b, _ := json.Marshal(result)
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		UPDATE "BackgroundJob"
		SET status = $1, result = $2::jsonb, finished_at = $3, error_message = NULL
		WHERE id = $4
	`, StatusSucceeded, string(b), now, id)
	return err
}

func (s *Store) MarkFailed(ctx context.Context, id string, errMsg string) error {
	now := time.Now().UTC()
	_, err := s.pool.Exec(ctx, `
		UPDATE "BackgroundJob"
		SET status = $1, error_message = $2, finished_at = $3
		WHERE id = $4
	`, StatusFailed, errMsg, now, id)
	return err
}

func (s *Store) Requeue(ctx context.Context, id string) error {
	_, err := s.pool.Exec(ctx, `
		UPDATE "BackgroundJob"
		SET status = $1, started_at = NULL, finished_at = NULL, error_message = NULL
		WHERE id = $2
	`, StatusQueued, id)
	return err
}

type scannable interface {
	Scan(dest ...any) error
}

func scanJob(row scannable) (*Job, error) {
	var j Job
	var shopID, errMsg, createdBy *string
	var payloadStr, progressStr, resultStr *string
	var started, finished *time.Time
	err := row.Scan(
		&j.ID, &j.Type, &shopID, &payloadStr, &j.Status, &progressStr, &resultStr,
		&errMsg, &j.CreatedAt, &started, &finished, &createdBy, &j.Attempts,
	)
	if err != nil {
		return nil, err
	}
	j.ShopID = shopID
	j.ErrorMessage = errMsg
	j.CreatedBy = createdBy
	j.StartedAt = started
	j.FinishedAt = finished
	j.Payload = map[string]interface{}{}
	if payloadStr != nil && *payloadStr != "" {
		_ = json.Unmarshal([]byte(*payloadStr), &j.Payload)
	}
	if progressStr != nil && *progressStr != "" {
		_ = json.Unmarshal([]byte(*progressStr), &j.Progress)
	}
	if resultStr != nil && *resultStr != "" {
		_ = json.Unmarshal([]byte(*resultStr), &j.Result)
	}
	return &j, nil
}
