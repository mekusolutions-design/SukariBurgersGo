# Nest-depth module pass (production, waste, POS, dashboard)

## Production
- Line struct: JSON tags snake_case (`actual_quantity`, `item_id`, …)
- Finish payload: inputs/outputs as snake_case maps (projection can deduct)
- usable_quantity = produced − waste (Nest inventory receives usable only)
- ensureItem for FG outputs before Event insert (FK safety)
- Projection already: deduct inputs / stock_deductions, increase FG via handleStockIncrease + batch

## Waste
- Shop required (no silent "1")
- Resolve item name/unit from Item catalog when missing
- Unit cost from InventoryProjection.avg_unit_cost when client omits
- Nest-shaped payload: waste_id, quantity_wasted, total_waste_value, dual casing

## POS
- Server totals + split validation (prior)
- Shop identity required (prior)
- Only pos_sale applies inventory (prior)

## Dashboard
- lowStockCount: exclude finished categories + written-off expired
- nearExpiryCount: stock > 0 and days 0..3, exclude finished
- Summary field names match web ShopSummary

## Receive (prior Nest depth)
- ensureItemExists Prisma columns, batch, ROP 20%, TX savepoints

## Remaining (not full Nest clone)
- Production resolveLines from recipe when client omits inputs
- Waste FEFO batch consume path
- Dashboard period-range Summary (Go Summary is today-only; KPIs have range)
- Live differential tests vs Nest on shared DB
