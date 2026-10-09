# Go COG cost thread (Nest parity)

Port of Nest Michael / Oct 3 production unit cost + waste + menu costing.

## Rules

- good = actual yield (stocked)
- waste = Waste qty field only
- totalMade = good + waste
- productionCost = Σ(inputs × WAC); mislabeled client unit_cost treated as batch total when ≈ input sum
- unitCost = productionCost ÷ totalMade
- stock qty = good only
- one waste event per batch with unitCost and value

## Costing API priority (`GET`-style SKU cost)

1. On-hand WAC
2. Latest production_finished unit_cost
3. Recipe cost per yield (batch ÷ standard_yield)
4. Stored WAC / missing

## Files

- pkg/costing/thread.go, thread_test.go
- internal/production/service.go (Finish)
- internal/costing/service.go
