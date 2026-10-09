# RestFlow Go Backend — Final Engineering Report (session)

## Summary

A production-oriented Go API replaces NestJS controllers/services for the primary RestFlow domains while targeting the **existing PostgreSQL schema** and **event-sourced write model**.

| Metric | Value |
|--------|-------|
| Go source files | 47 |
| Lines of Go | ~6,250 |
| API binary | builds |
| Worker binary | builds |
| Unit/contract tests | pass |

## Module mapping

| NestJS | Go package | Status |
|--------|------------|--------|
| AuthModule | internal/auth | Done |
| UsersModule | internal/auth + handlers | Done (me) |
| InventoryModule | internal/inventory | Done |
| ReceivedModule | internal/receiving | Done |
| PosModule | internal/pos + menu resolution | Done |
| KitchenModule | internal/kitchen | Done (active + status) |
| RecipeModule | internal/recipe | Done |
| WasteModule | internal/waste | Done |
| ProductionModule | internal/production | Done |
| PaymentModule | internal/payments | Done (STK + webhooks) |
| VarianceModule | internal/variance | Done |
| ApprovalsModule | internal/approvals | Done |
| SyncModule | internal/sync | Done |
| DashboardModule | internal/dashboard | Done |
| NotificationsModule | internal/notifications | Done |
| MenuModule | internal/menu | Done (catalog/combos + resolution) |
| EventStoreService | internal/eventstore | Done |
| ProjectionEngine | internal/projections | Done |
| AdvisoryLock | internal/locking | Done |
| Gateways | internal/realtime | Hub skeleton |
| Bull jobs | cmd/worker | Rebuild CLI |

## Security

- JWT validation, bcrypt passwords
- Shop ID from claims only
- Webhook routes separate from user auth
- Secrets via env; not logged
- CORS configurable; body size limits

## Database

No schema rewrite. First release uses existing tables/indexes/Decimals.

## Cutover recommendation

1. Run Prisma migrations on target DB (unchanged).
2. Deploy Go API alongside Nest; compare critical flows (POS, receive, kitchen).
3. Run integration + frontend smoke tests.
4. Switch load balancer / Render service to Go.
5. Keep Nest image for rollback 1–2 weeks.

## Remaining work (explicit)

- Full WS upgrade + multi-instance Redis
- Live DB integration test suite
- Stripe-Signature full verify
- Rate limit middleware
- OTEL metrics
- Optional: sqlc for all queries
