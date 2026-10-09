# RestFlow Go Migration — Architecture

## Status

**Phase 1 (archaeology):** complete  
**Phase 2 (design):** complete  
**Phase 3–5 (DB + core infra + master data start):** in progress  

Working Go binary builds: `cmd/api` compiles and links.

## Before (NestJS)

```
HTTP → Nest Controllers → ValidationPipe/Zod → Guards (JWT, Roles, Shop)
  → Services → Prisma transactions → EventStore.appendEvent
  → ProjectionEngine.updateProjections (same tx)
  → WebSocket gateways / Bull jobs
```

Event store is the source of truth. Projections are derived read models.

## After (Go)

```
HTTP → Chi/net-http handlers → middleware (RequestID, Auth, Shop, CORS, RateLimit)
  → Application services → Domain rules
  → EventStore.Append (pgx tx + optional ProjectionRunner)
  → PostgreSQL (existing schema)
  → WebSocket hub (later) / Asynq workers (later)
```

### Package map

| NestJS                    | Go                                      |
|---------------------------|-----------------------------------------|
| Controller                | `internal/http/handlers`                |
| Service                   | `internal/{domain}` application service |
| Prisma                    | `pgx` + `sqlc` repositories             |
| DTO / Zod                 | Request structs + validation            |
| Guard                     | `internal/http/middleware`              |
| Gateway                   | `internal/realtime` (pending)           |
| Bull/BullMQ               | Asynq / worker (pending)                |
| ConfigService             | `internal/config`                       |
| EventStoreService         | `internal/eventstore`                   |
| ProjectionEngineService   | `internal/projections` (pending)        |
| IdempotencyService        | eventstore unique key + helpers         |
| AdvisoryLockService       | `internal/locking`                      |

## Money

All monetary values use `pkg/money.Money` (shopspring/decimal, scale 4) matching `Decimal(18,4)`.

## Shop isolation

Every authenticated request carries `shop_id` from JWT claims. Repository queries for shop-scoped data must always filter by this value. Never trust client-supplied shop IDs for authorization.

## Compatibility

Routes, status codes, and response shapes for implemented endpoints match the NestJS contract so existing web/mobile clients continue to work.
