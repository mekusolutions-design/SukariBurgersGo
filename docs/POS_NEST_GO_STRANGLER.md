# POS on Go + rest of system on Nest

## Topology

```
Web / Mobile
  ├─ POS order, menu-availability, bootstrap  →  Go API  (NEXT_PUBLIC_POS_API_URL + USE_GO_POS)
  └─ receive, production, recipes, dashboard, waste, kitchen board → Nest API
         │
         └────────── same PostgreSQL (Supabase) ──────────┘
```

## Shared contracts (must stay identical)

| Concern | Rule |
|---------|------|
| Events | Go writes `pos_sale`, `order_sent_to_kitchen`, payment/status events Nest already projects |
| Stock | Go resolves menu → deductions from Nest recipes/catalog; deducts via same `InventoryProjection` |
| COGS | Unit cost priority: WAC → production lot → recipe cost/yield (matches Nest COG thread) |
| Revenue | Server total on `pos_sale.payload.revenue` / `total_amount`; Nest dashboard rebuild-sales reads these |
| Idempotency | Client `client_idempotency_key` → `pos-order-client-*` |
| Auth | Same `JWT_SECRET` and shop claim |

## Efficiency

- Prefer `GET /pos/bootstrap` once on POS open (menus + availability + combos + categories)
- Create order is one write path; kitchen/status can stay Nest or Go — both append events Nest projections understand
- Never run Nest and Go as dual writers for the same `POST /pos/order`

## Env (web)

```bash
NEXT_PUBLIC_API_URL=https://<nest>.onrender.com
NEXT_PUBLIC_POS_API_URL=https://<go>.onrender.com
NEXT_PUBLIC_USE_GO_POS=true
```

Go service: same `DATABASE_URL`, `JWT_SECRET` as Nest.
