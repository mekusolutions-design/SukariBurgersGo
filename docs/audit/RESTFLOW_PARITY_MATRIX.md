# RESTFLOW_PARITY_MATRIX.md

**Generated:** forensic source audit of Nest (`apps/api`) + Web + Mobile vs Go (`RestFlowGo`).  
**Scope:** transport + consumer usage + known behavioral gaps.  
**Evidence standard:** route extraction from controllers/router + client `endpoints.ts` + mobile `API_ENDPOINTS`.

## Summary counts (Level-1 path parity)

| Metric | Count | Evidence |
|--------|------:|----------|
| Nest controller routes extracted | 90 | `@Controller` + `@Get/@Post/...` scan |
| Go `mux.Handle` routes | 84 | `internal/http/router/router.go` |
| Approximate path matches | ~79 | normalized `{param}` comparison |
| Nest-only paths (approx) | 11 | see below (some callbacks registered via `HandleFunc`) |
| Go-only paths | 5 | diagnostics, `/ws`, aliases |
| Mobile explicit paths | 11 | single base URL client |
| Web `endpoints.ts` domains | 20+ | centralized catalog |

**HandleFunc note:** `POST /payment/mpesa/callback` and `POST /payment/stripe/webhook` exist on Go via `mux.HandleFunc` (unauthenticated webhook surface) — not counted in `mux.Handle` scan.

## Nest-only / incomplete on Go (after normalization)

| Path | Risk | Notes |
|------|------|-------|
| GET /approvals/:approvalId | Medium | Detail view may 404 on Go |
| GET /recipe/categories | Low | Web recipe filters |
| GET /recipe/item/:itemId | Medium | Lookup by business item |
| GET /recipe/:id/availability | Medium | Kitchen/recipe UI |
| PUT /recipe/:id | Medium | Recipe edit |
| POST /recipe/:id/outputs | Medium | Multi-output recipes |
| POST /menu/finished-good-categories | Low | Create FG category |
| POST /dashboard/rebuild-sales | Ops | Admin rebuild |
| POST /payment/stripe | Medium | Initiate Stripe (webhook exists) |

## Domain status (behavioral — not just path)

| Domain | Web | Mobile | Nest | Go | Logic | Calc | Event | Security | Status |
|--------|-----|--------|------|-----|-------|------|-------|----------|--------|
| Auth login/register | Y | Y | Y | Y | JWT claims must match | n/a | n/a | Hash/JWT | PARTIAL |
| Inventory list | Y | Y | Y | Y | projection read | value float leak risk | recv/waste/pos | shop scope | PARTIAL |
| Receive goods | Y | Y | Y | Y | event+projection | WAC | received | roles | PARTIAL |
| POS create order | Y | Y | Y | Y | server resolve | COGS | pos_sale | roles | PARTIAL |
| POS availability | Y | - | Y | Y | resolve+stock | maxPortions | n/a | roles | FIXED recently |
| Menu list | Y | - | Y | Y | event fold | portions | menu_* | roles | FIXED recently |
| Kitchen / closing stock | Y | Y | Y | Y | event | variance feed | closing_stock | roles | PARTIAL |
| Waste | Y | Y | Y | Y | analytics date filter | value | waste_recorded | roles | PARTIAL |
| Consumption | Y | - | Y | Y | from/to | food cost % | pos fold | roles | PARTIAL |
| Variance | Y | - | Y | Y | event fold web shape | value | variance_* | roles | PARTIAL |
| Trace | Y | - | Y | Y | users join | n/a | Event read | roles | FIXED User→users |
| Dashboard | Y | - | Y | Y | from/to KPIs | COGS proxy | projections | roles | PARTIAL |
| Approvals | Y | - | Y | Y | pending list | n/a | approval_* | MANAGER | PARTIAL |
| Payments M-Pesa | Y | - | Y | Y | callback auth | amount | payment events | secret | PARTIAL |
| Payments Stripe | Y | - | Y | webhook | signature | amount | payment | signature | PARTIAL |
| Production | Y | - | Y | Y | start/finish | recipe cost | production_* | roles | PARTIAL |
| Refill | Y | - | Y | Y | pending/issue | qty | refill_* | roles | PARTIAL |
| Realtime | Y | Y? | namespaces | single /ws | channels differ | n/a | emit | auth | FAIL |
| Notifications | Y | Y | Y | limited | list/settings gap | n/a | n/a | roles | FAIL mobile settings |

**PASS requires evidence per row.** No domain is fully PASS under the audit standard in §81.

## Mobile cutover gate

Mobile uses **one** `API_BASE_URL`. Paths required: login, register, received, inventory, kitchen closing/active, POS orders, waste, notifications (+ settings).  
**Do not** point mobile solely at Go until notifications settings + full POS payment + offline idempotency proven.

## Certification

```
NOT CERTIFIED — GO IS NOT YET BEHAVIORALLY COMPATIBLE AS SOLE BACKEND
```
