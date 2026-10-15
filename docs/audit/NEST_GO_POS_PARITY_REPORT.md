# SukariBurgers — NestJS vs Go POS Parity Report

**Date:** 2026-10-15  
**Nest source:** SukariBurgers-main (2).zip  
**Go source:** RestFlowGo-main (1).zip → updated in this package  

## Architecture (unchanged)

| Concern | Owner |
|---------|--------|
| Schema / migrations | NestJS Prisma only |
| POS HTTP when `POS_API_URL` points to Go | Go |
| Kitchen, waste, production, pre-prep, inventory ops | Nest (clients may still call Nest) |
| Shared DB | PostgreSQL event store + projections |

## Endpoint matrix (POS)

| Method | Path | Nest | Go | Status |
|--------|------|------|-----|--------|
| POST | /pos/order | ✓ | ✓ | Equivalent (+ server total) |
| POST | /pos/orders | ✓ (mobile alias) | ✓ CreateOrderAlias | Equivalent |
| GET | /pos/orders | ✓ | ✓ | Equivalent |
| GET | /pos/orders/:id | ✓ | ✓ | Equivalent |
| POST | /pos/orders/:id/send-to-kitchen | ✓ | ✓ (no stock re-deduct) | Equivalent |
| POST | /pos/orders/:id/payment | ✓ | ✓ | Equivalent |
| GET | /pos/bootstrap | ✓ (+ KITCHEN) | ✓ (+ KITCHEN after fix) | Equivalent |
| GET | /pos/menu-availability | ✓ | ✓ | Equivalent |

## Critical rules verified

| Rule | Nest | Go | Notes |
|------|------|-----|-------|
| Client stock_deductions ignored | Yes | Yes | Server resolves only |
| Inventory only on `pos_sale` | Yes | Yes | Kitchen/payment do not re-deduct |
| Combo fixed price from catalog | Yes | Yes | |
| Menu unit price | Client `selling_price` | Catalog `UnitPrice` | **Intentional:** Go is stricter (server-authoritative) |
| Order total | Client `total_amount` | Sum of server line totals | **Intentional** security improvement |
| Idempotency | client key or new order id | Same pattern via event key | |
| Shop scope | shop_id on events | shop_id query/body + JWT actor | Default shop "1" if empty (same as Nest) |
| Split payment = total | vs client total | vs **server** total | Go stricter |

## Fixes implemented this pass

1. **Combo kitchen ticket fields** — `combo_selections` now include `menu_item_names` and `group_name`; payload also has `resolved_menu_items`, `menu_id`/`item_id`, `selling_price` for Nest-shaped clients.
2. **GET /pos/bootstrap** — roles include **KITCHEN** (Nest `@Roles` parity).

## Remaining risks (not blocking POS staging)

| ID | Severity | Item |
|----|----------|------|
| R1 | P2 | Full Nest↔Go differential tests need shared disposable DB |
| R2 | P2 | Nest menu line still accepts client `selling_price` — product decision whether Nest should match Go |
| R3 | P3 | Go still defaults empty shop_id to `"1"` like Nest |
| R4 | P1 if cutover | Live lifecycle smoke on staging DB not run in this environment |

## Tests executed

```bash
export GOPROXY=https://proxy.golang.org,direct
go test ./internal/pos/ ./internal/menu/ ./pkg/... ./tests/unit/ -count=1
# ok
CGO_ENABLED=0 go build -o /tmp/sukari-go-api ./cmd/api
# ok
```

## Staging readiness

**Staging / canary-ready for POS path** after deploy with real `DATABASE_URL`, `JWT_SECRET`, production `APP_ENV`.

**Not certified** as sole Nest replacement without live DB parity smoke (receive → POS → kitchen → pay → inventory/sales once).
