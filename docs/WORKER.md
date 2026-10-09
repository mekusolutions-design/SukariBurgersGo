# RestFlow Go Background Worker

Runs **heavy, offline** jobs so POS and the Nest/Go HTTP APIs stay fast.

## Job types

| Type | Purpose |
|------|---------|
| `full_projection_rebuild` | Replay events ASC → refresh Inventory/Sales/Waste projections (strategy **C**: idempotent upsert; optional `confirm_clear=true` for delete+replay) |
| `report_export` | CSV aggregates (sales_summary, waste_summary, variance_batches) → file path in job result |
| `nightly_reconcile_backfill` | Compare sales events vs SalesProjection; backfill missing day rows |
| `bulk_recompute_variance_consumption` | Recompute variance %/flags; aggregate true consumption from `pos_sale` stock_deductions |

Everyday POS charges, single waste posts, and short dashboard reads are **not** worker jobs.

## Database

Apply migration (or rely on worker `EnsureSchema` on start):

```bash
psql "$DATABASE_URL" -f db/migrations/20260922_background_job.sql
```

Tables: `BackgroundJob`, `ReconcileMismatch`.

## Environment

| Variable | Default | Notes |
|----------|---------|--------|
| `DATABASE_URL` | required | Prefer Supabase **pooler** URL; simple protocol is forced |
| `WORKER_MAX_CONNS` | `3` | Keep low so Nest + Go POS keep headroom |
| `WORKER_REPORT_DIR` | OS temp | Where CSV exports are written |
| `VARIANCE_TOLERANCE_PCT` | `5` | Flag variance when \|pct\| exceeds this |
| `JWT_SECRET` | optional for worker | Auto-filled with placeholder if missing |

## CLI

```bash
# Build
go build -o restflow-worker ./cmd/worker

# Health
./restflow-worker health

# Enqueue
./restflow-worker enqueue full_projection_rebuild --shop-id=1
./restflow-worker enqueue report_export --shop-id=1 --report_type=sales_summary --from=2026-09-01 --to=2026-09-22

# Process queue (long-running)
./restflow-worker run

# One-shot
./restflow-worker run-job <job-id>
./restflow-worker status <job-id>
./restflow-worker retry <job-id>
```

### Enqueue via SQL

```sql
INSERT INTO "BackgroundJob" (id, type, shop_id, payload, status, created_at)
VALUES (
  gen_random_uuid()::text,
  'full_projection_rebuild',
  '1',
  '{"shop_id":"1"}'::jsonb,
  'queued',
  NOW()
);
```

## Render Background Worker

1. Dockerfile: `Dockerfile.worker` (builds `./cmd/worker`)
2. Start command: `./restflow-worker run` (override image CMD if needed)
3. Env: same `DATABASE_URL` as Go API (pooler)
4. Plan: starter is enough for low-frequency jobs

`render.yaml` already defines a `restflow-worker` service; set:

```yaml
dockerCommand: /app/restflow-worker run
```

or change Dockerfile.worker:

```dockerfile
CMD ["run"]
```

## Safety

- Live `Append` + incremental projection on POS is **unchanged**
- Rebuild uses strategy **C** by default (no truncate)
- Truncate only with `--confirm_clear=true`
- Worker uses a **small** connection pool
- Failed jobs keep `error_message` and can be `retry`’d without rewriting the event log
