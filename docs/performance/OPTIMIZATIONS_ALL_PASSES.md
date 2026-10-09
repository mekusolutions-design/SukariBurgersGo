# RestFlow performance — passes 1–7 (cumulative)

## Pass 7 — POS bootstrap (one RTT)
- `GET /pos/bootstrap` → menus + availability + combos + categories
- `listMenus` called **once**; availability derived in memory
- Parallel combos + categories via `Promise.all`
- Web: `posApi.getBootstrap`, `usePosDashboard` uses bootstrap for availability
- `NewOrderPanel` prefers bootstrap menus (fallback `menuApi.list`)

## Pass 6 — Menu catalog backfill (`POST /menu/catalog/backfill`)
## Pass 5 — MenuCatalogProjection + DB-first lists
## Passes 1–4 — projections, refill listByTypes, caches, trace keyset, indexes

## Deploy
```bash
cd apps/api && npx prisma migrate deploy && npx prisma generate && npm run build
curl -X POST -H "Authorization: Bearer $TOKEN" "$API/menu/catalog/backfill"
# POS open should hit:
curl -H "Authorization: Bearer $TOKEN" "$API/pos/bootstrap"
```

## NOT PERFORMANCE CERTIFIED
