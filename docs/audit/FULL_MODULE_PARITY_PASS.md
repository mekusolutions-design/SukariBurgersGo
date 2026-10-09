# Full module parity pass (Nest ↔ Go ↔ Web/Mobile)

## Inventory (prior + cards sync)
- List status filters match Nest
- Summary counts use same predicates as filters
- Alerts, categories (canonical), batches, dual-case JSON

## Routes added this pass
| Method | Path | Purpose |
|--------|------|---------|
| GET | `/auth/me` | Web `endpoints.auth.me` |
| POST | `/pos/orders` | Mobile create order alias |
| GET | `/received` | Web list receives |
| PUT | `/recipe/{recipeId}` | Web recipe update |
| POST | `/recipe/{recipeId}/outputs` | Web recipe outputs |
| GET | `/recipe/item/{itemId}` | Prefix mux (ServeMux-safe) |
| GET | `/notifications` | Mobile list |
| GET/PUT | `/notifications/settings` | Mobile settings stub |
| POST | `/dashboard/rebuild-sales` | Nest admin rebuild |

## Already present (verified in router)
Auth, users, inventory, POS core, kitchen, production, waste, variance, approvals, menu, consumption, costing, refill, trace, payments callbacks, sync, finished-goods

## Intentionally different
- Inventory WebSocket namespace → Go `/ws` multiplex (clients may need adapter)
- Stripe initiate may be partial vs Nest payment module depth
- Recipe `RecipeOutput` table insert best-effort if table missing

## Build
`go build ./cmd/api` OK after this pass
