# Nest clone status

## Achieved (HTTP + business logic)
- Controller surface for web + mobile (auth, inventory, POS, kitchen, production, waste, variance, approvals, menu, consumption, costing, refill, trace, users, payments, sync, FG, dashboard, notifications)
- Inventory filters/summary card parity
- Decimal WAC / money paths
- Stripe PaymentIntent create + webhook verify
- M-Pesa STK + callback
- Recipe item path without ServeMux conflict
- Aliases: `/auth/me`, `/pos/orders`, `/received` list

## Realtime
- Nest uses **Socket.IO** namespaces: `/inventory`, `/pos`, …
- Go uses **native WebSocket** on:
  - `GET /ws`
  - `GET /ws/{namespace}` (inventory, pos, waste, production, payment, refill, finished-goods, notifications, menu)
- Event envelope: `{ event, type, data, namespace, ts }` + `join_shop` / `ping`
- **Not** byte-compatible with Socket.IO clients without a client change or a Socket.IO server library
- Web/mobile that hard-code `io(API_URL + '/inventory')` need either:
  1. Point at a Socket.IO-compatible layer, or
  2. Switch to `new WebSocket(API_URL + '/ws/inventory?token=…&shopId=…')`

## Honest ceiling
A true 100% Nest *runtime* clone would require Socket.IO protocol compatibility (engine.io).  
This codebase is a **functional Nest replacement** for REST + domain logic; realtime is **semantic** parity, not wire-protocol parity.
