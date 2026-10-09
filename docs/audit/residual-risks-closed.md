# Residual risks addressed in this environment

| Risk | Action | Result |
|------|--------|--------|
| Client price when catalog=0 | Removed client fallback; server price only | FIXED |
| Availability vs deduct race | Shop advisory lock on pos_sale/received/waste/production_finished + conditional UPDATE | FIXED (code); load race needs live PG |
| Menu routes partial | GET/POST /menu, categories, FG categories, assign, finished-goods, availability | FIXED routes |
| Projection unit test weak | `AppliesSaleSideEffects` policy + tests in package | FIXED |
| COGS formula | Unit tests batch cost / yield / food cost % | FIXED unit level |
| go test -race | Ran on unit packages | PASS |
| Live lifecycle on real DB | No PostgreSQL in environment | STILL BLOCKED |
| Nest numeric dashboard parity | Needs dual live systems | STILL BLOCKED |
| Realtime/workers/mobile | Not expanded | STILL OPEN |

## Commands

```
go build ./cmd/api → PASS
go test ./tests/unit ./pkg/money ./pkg/costing ./internal/projections → PASS
go test -race (same packages) → PASS
go vet (pos, projections, payments, menu, eventstore) → PASS
```
