# Decimal implementation (remediation)

## Design
- Totals / revenue / waste value: **4 dp** (`Money.Scale`)
- Unit cost / WAC: **6 dp** (`Money.UnitCostScale`) — matches Prisma `Decimal(18,6)`
- Library: `shopspring/decimal`
- SQL: prefer `numeric::text` → `money.New` / `NewUnitCost`

## Updated
- `pkg/money` — dual scale, `MulQty`, `DivQty`, `ParseFlexible`, `AsUnitCost`/`AsTotal`
- `pkg/costing` — `WeightedAverageUnitCostDecimal` (canonical); legacy float wrapper retained
- Projections receive path — decimal WAC
- Receiving, waste, finished goods, POS foodCost accumulation, payments STK amount normalize

## Remaining (not fully eliminated)
- Some handler request DTOs still decode `float64` at the HTTP edge (then convert to Money).
- API responses still often emit JSON numbers via `.Float64()` for Nest web compatibility.
- Full migration of every service field to `money.Money` types is incremental.

## Tests
- `pkg/money` unit tests
- `pkg/costing` WAC decimal tests
