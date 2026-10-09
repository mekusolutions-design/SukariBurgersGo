# GO_ARCHITECTURE_AUDIT.md

## Package map

```
cmd/api, cmd/worker
internal/{auth,http,eventstore,projections,locking,realtime,worker}
internal/{pos,menu,inventory,receiving,waste,variance,dashboard,trace,...}
pkg/{money,costing,errors}
```

## Strengths

- Event store + projection engine separation
- Advisory locks for shop-scoped mutations
- money package (shopspring/decimal) present
- Domain packages roughly match Nest modules

## Weaknesses

- Handlers sometimes map JSON ad hoc (duplicated field aliases)
- float64 still used in POS totals and many service APIs
- Menu/POS resolution complexity split across menu + pos (risk of drift)
- Realtime single hub vs Nest multi-namespace gateways
- Operational reads still scan Event JSON (consumption, menu fold, variance fold) — correct for history, heavy for hot paths

## Recommendations (priority)

1. Keep JWT shop authoritative; reject cross-shop IDs.  
2. Promote read models for menu availability & consumption daily aggregates.  
3. Expand contract tests (consumer-driven).  
4. Differential Nest vs Go golden scenarios on shared Supabase clone.
