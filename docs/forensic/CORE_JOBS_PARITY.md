# Nest core/ + jobs/ vs Go alignment

## Core matrix

| Nest file | Go counterpart | Status |
|-----------|----------------|--------|
| event-store.service.ts | internal/eventstore/store.go | Aligned: idempotency, TX + projection, shop_id |
| event-store.logic.ts | (inlined in store) | Aligned |
| idempotency.service.ts | eventstore unique idempotency_key | Aligned |
| advisory-lock.service.ts | internal/locking + eventstore AcquireShopLock | Aligned for POS/receive/waste/production |
| fefo-stock.service.ts | internal/fefo/service.go | Aligned: FEFO consume, synthetic seed, soft-fail if table missing |
| projection-engine.service.ts | internal/projections/engine.go | See matrix below |
| sales-projection-backfill.service.ts | dashboard.RebuildSales + worker rebuild | Partial (on-demand) |

## Projection event routing

| Event | Nest | Go |
|-------|------|-----|
| received, finished_good_added | stock increase | same |
| production_finished | deduct inputs + FG increase | same |
| waste / waste_recorded | deduct | same |
| batch_expired | available→expired + waste proj | same (improved this pass) |
| finished_good_adjusted | adjustment | same |
| refill_issued | deduct | same |
| pos_sale | sales + stock_deductions | same |
| order_sent_to_kitchen / status / payment | handlePosSale (risk of re-deduct if payload has lines) | **no-op** (safer; intentional) |
| closing_stock_counted | set counted qty | same |

## Jobs

| Nest job | Go job type | Status |
|----------|-------------|--------|
| expiry-sweep (cron 02:00) | `expiry_sweep` | **Added this pass** (enqueue via worker API / ops) |
| projection-rebuild | `full_projection_rebuild` | Present |
| excel-import | — | Not ported (ops Excel path) |
| nightly reconcile | `nightly_reconcile_backfill` | Present |
| variance bulk | `bulk_recompute_variance_consumption` | Present |

## Gaps remaining (non-blocking for POS path)

1. Go has no in-process cron for expiry — run worker + enqueue `expiry_sweep` daily (or external scheduler).
2. Excel import processor not in Go.
3. Nest still routes kitchen events into handlePosSale; Go intentionally does not (prevents double inventory).
