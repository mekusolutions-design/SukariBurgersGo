# SukariBurgers POS — NestJS vs Go Static Parity Audit

**Status: NOT CERTIFIED for production cutover.** This audit is based on the supplied source archives. The Go source archive is named `RestFlowGo-main` and declares module `github.com/restflow/api-go`; it is not named `SukariBurgersGo`. Confirm this is the exact source currently deployed as the SukariBurgers Go POS service before deploying this ZIP.

## Architecture and scope

- NestJS source: `apps/api/src/modules/pos/pos.service.ts`, `pos.controller.ts`, `dto/create-order.dto.ts`, `dto/update-payment.dto.ts`, plus `menu/sellable-resolution.service.ts` and `menu/combo.service.ts`.
- Go source: `internal/http/router/router.go`, `internal/http/handlers/pos.go`, `internal/http/handlers/pos_extra.go`, `internal/pos/service.go`, `internal/menu/resolution.go`, `internal/menu/list_menus.go`.
- The mobile source confirms a POS strangler configuration (`EXPO_PUBLIC_POS_API_URL`, `EXPO_PUBLIC_USE_GO_POS`) and uses `/pos/order`, `/pos/orders`, `/pos/bootstrap`, `/pos/menu-availability`, `/pos/orders/{orderId}/send-to-kitchen`, and `/pos/orders/{orderId}/payment`. Non-POS API traffic is documented to remain on Nest.
- No Go-owned migration was executed and no production service/database was contacted by this audit.

## Changes made in this pass

1. Removed the unscoped event-log fallback in `internal/menu/list_menus.go`. A shop with no matching menu events no longer receives another shop's event-derived menu.
2. POS `ListOrders`, `MenuAvailability`, `Bootstrap`, and `MenuHandler.ListMenus` now use the authenticated shop context rather than accepting `shopId`/`shop_id` query parameters as tenant overrides. This matches Nest controllers that use `@CurrentShop()`.
3. Added a guard against replaying a globally unique idempotency key belonging to another shop in `internal/pos/service.go`.
4. Added CreateOrder validation for Nest DTO enums, positive integer item quantities, line type, split-payment methods, and positive split amounts.
5. Added UpdatePayment enum validation to match `UpdatePaymentSchema`.
6. Changed Go order history to load the most recent 30 days before lifecycle folding, sort newest-first, paginate after folding, and return total/page/pageSize/totalPages metadata. It now includes the principal fields exposed by Nest list history and defaults missing legacy payment status to `paid`.
7. Aligned `GET /pos/bootstrap` authorization with Nest by allowing KITCHEN in addition to POS, MANAGER, and ADMIN.
8. Added unit tests for input validation and the menu-event shop-scope helper.

## Endpoint parity matrix (static source inspection)

| NestJS route | Go route | Status | Notes |
|---|---|---|---|
| `POST /auth/login` | `POST /auth/login` | Partial | Route exists; JWT claims, disabled-user behavior, token compatibility, and response semantics still require differential tests against the deployed Nest contract. |
| `POST /pos/order` | `POST /pos/order` | Partial / P1 | Both resolve sellable lines server-side and record `pos_sale`. Go intentionally ignores client totals and derives line prices from Go menu resolution, whereas Nest currently records `dto.total_amount` and uses `selling_price` for regular lines. Tax/discount semantics therefore need an agreed, source-backed reconciliation before cutover. |
| Mobile alias `POST /pos/orders` | `POST /pos/orders` | Route present | Go alias delegates to the same order creation handler. Verify actual client request/response with a staging contract test. |
| `GET /pos/bootstrap` | `GET /pos/bootstrap` | Partial | Route and Nest role set now align (POS/MANAGER/ADMIN/KITCHEN); the untrusted query shop override was removed. Data-source equivalence and empty-state behavior still need database-backed differential tests. |
| `GET /pos/menu-availability` | `GET /pos/menu-availability` | Partial | Route exists; shop scope now comes from authenticated principal. Availability calculations still require identical fixture comparisons. |
| `GET /pos/orders` | `GET /pos/orders` | Improved; unverified | Go now folds lifecycle events before sorting/pagination and returns total metadata. Lifecycle folding still has a 2,000-event cap; verify parity under larger history. |
| `GET /pos/orders/:orderId` | `GET /pos/orders/{orderId}` | Partial / P2 | Route exists and scopes queries by shop. Go reads a bounded set of sale/lifecycle events (including a 1,000-sale-event limit); older records can be missed. |
| `POST /pos/orders/:orderId/send-to-kitchen` | same | Partial / P1 | Route exists; Go builds a kitchen event from the stored order and strips stock deduction fields to avoid repeating inventory effects. Concurrent duplicate sends and payload parity require DB-backed tests. |
| `POST /pos/orders/:orderId/payment` | same | Partial / P1 | Route exists; Go now validates status/method enums. It records a status event, as Nest does; this endpoint is not proof of gateway settlement. |
| `POST /payment/mpesa` | `POST /payment/mpesa` | Partial | Route exists. STK initiation, callback verification, duplicate callbacks, and order/payment reconciliation not verified end-to-end. |
| `POST /payment/stripe` | `POST /payment/stripe` | Partial | Route exists. Intent/webhook and payment-state parity not verified end-to-end. |
| `POST /payment/mpesa/callback` | same | Partial | Public callback route exists; signature/secret validation and replay handling require dedicated tests. |
| `POST /payment/stripe/webhook` | same | Partial | Public webhook route exists; signature validation and duplicate-event handling require dedicated tests. |
| `GET /menu` | `GET /menu` | Improved; partial | Query shop override removed. Menu data is merged from shop-scoped event data and `CatalogMenuItem`; full parity with Nest `MenuService.listMenus` still needs fixtures. |
| `GET /menu/combos` | same | Partial | Route exists; selection-group shape and active/inactive behavior require differential tests. |
| `POST /menu/combos/preview` | same | Partial | Route exists; combo group and stock-resolution edge cases require parity tests. |
| `GET /kitchen/active-orders` and history routes | corresponding Go routes | Partial | Routes exist; Go and Nest lifecycle/event folding must be compared on existing historical data. |

The router has additional inventory, recipe, waste, production, dashboard, refill, trace, and reporting routes. They are outside the narrow POS cutover matrix above and should not be considered certified by this POS pass.

## Critical unresolved risks — do not cut over yet

### P0 — Concurrent stock oversell remains possible

Go checks stock availability in `internal/pos/service.go` before `eventStore.Append`. The event store acquires a shop-scoped advisory lock inside its own transaction, but the availability read is not repeated under that same lock/transaction. Two concurrent requests can both pass the pre-check and then append sales. This must be fixed with a transaction-safe final availability check / projection invariant and a concurrent-order regression test before production use.

### P1 — POS totals, discounts, and taxes differ

Nest's current `createOrder` records `dto.total_amount` and `tax_amount`/`discount_amount`; regular line `line_total` is based on client `selling_price`. Go ignores client total and derives price from its server-side sellable resolver, but the computed order total currently sums line totals without applying tax or discount. Do not silently alter the accounting formula. Confirm the intended authoritative rule with the Nest owner, then add matching deterministic fixtures and tests on both implementations.

### P1 — Payment workflow not certified

Go's order-payment endpoint changes the order's payment-status event, while actual M-Pesa/Stripe payment integration is implemented separately. No live or staging gateway callbacks were exercised. Do not treat a manual `paid` status as proof of payment settlement.

### P1 — Combo and inventory parity not certified end-to-end

Go's combo resolver supports classic and expanded selection shapes and aggregates deductions. Need fixture comparisons for fixed-price combos, multiple selection groups, quantity multipliers, duplicate selected SKUs, inactive/unavailable components, recipes versus directly stocked SKUs, and repeated items. No database was available for those tests.

### P2 — Historical order reads are bounded

`GetOrder` and lifecycle fold queries use bounded event reads. Orders beyond those bounds can be absent or have stale status. Compare with Nest's payload-filtered event queries and replace bounded scans with indexed scoped queries or safe pagination if evidence confirms gaps.

### Architecture blocker — existing Go worker schema ownership

The supplied Go archive contains `internal/worker/store.go` with `EnsureSchema` issuing `CREATE TABLE IF NOT EXISTS` for `BackgroundJob` and `ReconcileMismatch`, plus SQL files under `db/migrations/`. Those tables are not represented in the supplied Nest Prisma schema. This pass did not execute or delete them because doing so would affect the separately configured worker; however, this violates the stated rule that Go must not own schema changes. Keep the worker disabled until its schema is either made Nest/Prisma-owned and reviewed or the worker schema bootstrap is removed. The POS API itself does not need those worker tables for its primary route set.

## Tests and validation

- Baseline `go test ./...`: **blocked**, not failed by source test assertions. Local toolchain is Go 1.23.2; `go.mod` requires Go 1.26.0. Automatic toolchain download failed because this environment cannot resolve `proxy.golang.org`.
- `go vet ./...`: blocked by the same Go toolchain requirement; not claimed as passed.
- `gofmt`: run on changed Go files; formatting completed.
- Unit tests added but **not executed** due the Go toolchain block.
- Nest tests and DB-backed differential tests: not run; dependencies and isolated test database were not provisioned.
- Production DB writes, deployment, and external payment requests: not performed.

## Staging gate

Before any production cutover:

1. Confirm the supplied `RestFlowGo-main` archive is the exact source deployed for SukariBurgers POS; its module path and Render blueprint names are generic RestFlow names.
2. Build/test using Go 1.26+ (the repository's declared minimum), then run `go test ./...`, `go vet ./...`, race tests, and a clean build.
3. Resolve the P0 stock race and P1 total/tax/discount rule discrepancy.
4. Run Nest-vs-Go differential tests against an isolated database with identical fixture data for prices, discounts, taxes, combo selection, inventory deduction, payment states, and order history.
5. Verify login/JWT role/shop isolation and all client routes from web/mobile without changing the Nest base URL for non-POS traffic.
6. Deploy only to staging, monitor stock movements and payment callbacks, and keep a rollback path to route POS traffic back to Nest.
