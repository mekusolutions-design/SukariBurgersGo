-- RestFlow background jobs (Go worker). Safe to run multiple times.
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

CREATE INDEX IF NOT EXISTS idx_background_job_type
  ON "BackgroundJob" (type);

-- Optional mismatch log for reconcile jobs
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

CREATE INDEX IF NOT EXISTS idx_reconcile_mismatch_shop
  ON "ReconcileMismatch" (shop_id, created_at DESC);
