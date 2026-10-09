# Database Compatibility

## Source of truth

Prisma schema + migrations under `apps/api/prisma/`.

## Strategy

1. First Go release **reads and writes the existing schema** without destructive changes.
2. Table names follow Prisma conventions (`"Event"`, `users`, `"InventoryProjection"`, etc.).
3. Money columns remain `numeric(18,4)`.
4. Event `idempotency_key` stays unique.
5. Optional later migrations only after cutover and with rollback plans.

## Key models used by current Go code

- `users` — auth / RBAC / shop_id
- `"Event"` — event store (append, idempotency, query)

## Connection

- Driver: `jackc/pgx/v5` + `pgxpool`
- Config: `DATABASE_URL` (and optional `DIRECT_URL`)
- Pool: max 25, health checks, connection lifetime limits

## Advisory locks

`pg_advisory_xact_lock(123456789, hash(shop_id))` — same key scheme as NestJS `AdvisoryLockService`.
