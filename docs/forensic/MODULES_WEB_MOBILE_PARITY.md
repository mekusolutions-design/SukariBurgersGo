# Nest modules vs Go vs web/mobile

## Route coverage (Nest controller ↔ Go mux ↔ clients)

| Module | Nest routes | Go | Web | Mobile |
|--------|-------------|-----|-----|--------|
| auth | login, register | yes | yes | yes |
| users | me, list, get, role | yes | yes | — |
| inventory | list, summary, alerts, categories, :id, batches | yes | yes | GET /inventory |
| received | POST | yes + GET list | yes | POST /received |
| pos | order, orders, kitchen, payment, availability | yes (+ /pos/orders alias) | yes | POST /pos/orders |
| kitchen | active-orders, queue, history, closing-stock, status, batches | yes (active-orders deepened this pass) | yes | active-orders, closing-stock |
| production | start, finish, history | yes | yes | — |
| waste | record, write-off, candidates, analytics, events | yes (+ POST /waste alias) | yes | POST /waste |
| recipe | full CRUD + availability | yes | yes | — |
| menu | catalogs, categories, combos, availability, FG | yes | yes | — |
| dashboard | summary, kpis, alerts, rebuild-sales | yes (period + Nest filters) | yes | — |
| approvals | pending, history, approve/reject | yes (Nest event types) | yes | — |
| variance | batches, flag, reason | yes | yes | — |
| refill | pending, request, issue | yes | yes | — |
| consumption | summary, products, menu-items, alerts | yes | yes | — |
| costing | sku/:id | yes | yes | — |
| finished-goods | add, adjust, list | yes | yes | — |
| payment | mpesa, stripe, callbacks | yes | yes | — |
| notifications | create, list, settings | yes | yes | list, settings |
| sync | bulk | yes | — | — |
| trace | list, item | yes | yes | — |

## Logic deepened this pass

### Kitchen ActiveOrders (Nest getActiveOrders)
- Sources: `pos_sale` + `order_sent_to_kitchen` + `order_status_updated` (48h)
- Filters out completed/cancelled/served/not_sent
- Item mapping: resolved_menu_items, combo selections, stock_deductions fallback
- Dual keys: order_id / orderId, kitchen_status

### Production queue
- Finished-id keys: production_id, productionId, batch_id (Nest extractString set)

## Intentional differences

| Area | Nest | Go | Why |
|------|------|-----|-----|
| Kitchen follow-up events | enter handlePosSale | projection no-op | prevents double stock deduct |
| Expiry cron | Bull @ 02:00 | worker job `expiry_sweep` | enqueue via ops/scheduler |
| Excel import job | present | not ported | admin-only |

## Mobile aliases already present
- POST /pos/orders → CreateOrder
- POST /waste → Record
- GET /kitchen/active-orders, closing-stock
- GET /inventory, POST /received
