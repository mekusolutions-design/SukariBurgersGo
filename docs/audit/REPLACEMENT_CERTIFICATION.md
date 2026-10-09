# REPLACEMENT_CERTIFICATION.md

## Executive status

**NOT CERTIFIED — REPLACEMENT BLOCKERS REMAIN**

Go is improved for Trace + production history contracts and retains prior P0 POS/security fixes from earlier packages. It is **not** proven as a full Nest replacement for web+mobile on live data.

## Fixed in this pass
| Area | Fix |
|------|-----|
| Trace search | q / itemId / batch / eventType; Nest-shaped `{events,items,totalEvents}` |
| Trace item | `{itemId,itemName,unit,currentStock,events,batches,relatedItemIds}` |
| Tenant on Trace | Shop from JWT only |
| Event aliases | received, waste, pos_sale, production_*, refill_*, closing_stock, variance |
| Production history | `GET /production/history` from started/finished events |
| Nest production history | Removed empty stub; folds real events |

## P0 remaining (block full cutover)
1. Live lifecycle test on shared Postgres (receive → POS → kitchen → pay → trace)
2. Nest vs Go numeric parity (dashboard/COGS/waste analytics)
3. Concurrent oversell under load on real DB
4. Mobile single-URL full contract verification
5. Realtime channel parity
6. Full route matrix PASS for every Nest controller method

## P1 remaining
- Menu catalog vs Nest event-sourced menus
- Consumption real history series
- OpenAPI regeneration completeness
- Decimal money throughout remaining float paths

## Final certification status

```
NOT CERTIFIED — REPLACEMENT BLOCKERS REMAIN
```

Staging recommendation: **Nest primary** for Trace/history domains until live Trace acceptance test passes; POS-only Go canary remains acceptable.
