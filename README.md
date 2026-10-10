# RestFlow Go — units parity

Pure Go package matching Nest `apps/api/src/common/utils/stock-units.ts`:

| TS | Go |
|----|-----|
| `toCanonicalStockQty` | `units.ToCanonicalStockQty` |
| `quantityInItemUnit` | `units.QuantityInItemUnit` |
| `scaleFactorForUnitChange` | `units.ScaleFactorForUnitChange` |
| `canonicalStorageUnit` | `units.CanonicalStorageUnit` |
| web `dedupeMenuLines` | `units.DedupeMenuLines` |

## Run tests

```bash
cd restflow-units-go
go test ./...
```

Wire into a future Go receive/POS path the same way Nest does:

1. **Receive** — `ToCanonicalStockQty` before append event; scale projection if unit flips.
2. **POS deduct** — `QuantityInItemUnit(componentQty, componentUnit, item.Unit)`.
3. **Display** — `DedupeMenuLines` before rendering components.
