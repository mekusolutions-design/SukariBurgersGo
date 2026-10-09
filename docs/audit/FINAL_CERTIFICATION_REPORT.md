# FINAL_CERTIFICATION_REPORT.md

## Totals (this audit cycle)

| Metric | Value |
|--------|------:|
| TOTAL ROUTES AUDITED (Nest) | 90 |
| TOTAL GO ROUTES SCANNED | 84+ (HandleFunc extras) |
| TOTAL ROUTES APPROX MATCHING | ~79 |
| TOTAL ROUTES FIXED (prior+this) | menuId, alerts array, users join, date filters, variance shape, POS availability resolve |
| TOTAL BUSINESS RULES AUDITED | 40+ domain topics (see BUSINESS_LOGIC_PARITY) |
| TOTAL CALCULATIONS AUDITED | WAC/COGS/food cost/maxPortions/waste value/KPI |
| TOTAL DIFFERENCES FOUND | 25+ material |
| TOTAL DIFFERENCES FIXED | 8+ high-visibility consumer breaks |
| TOTAL TESTS ADDED | contract parity_matrix_test + existing unit/contract suite |
| TOTAL TESTS PASSED | not executed against live shared DB in this environment |
| TOTAL TESTS BLOCKED | Nest↔Go differential / race / mobile e2e (no live Nest+Go harness here) |

## Remaining blockers (replacement)

1. Live differential scenarios Nest vs Go on shared production-like DB  
2. Concurrent stock race proof  
3. Realtime namespace parity  
4. Mobile full contract + notifications settings  
5. Remaining Nest-only recipe/admin routes  
6. Security/tenant default shop "1" elimination  
7. Golden financial equality (cent-level) across dashboard/POS/consumption  

## FINAL CERTIFICATION

```
NOT CERTIFIED — GO IS NOT BEHAVIORALLY COMPATIBLE AS A FULL NEST REPLACEMENT
```

**Evidence:** route gaps remain; consumer 86'd POS bug class required code fix; Trace SQL table bug; date filters ignored until fixed; variance/menu shape mismatches; no live dual-backend golden run in this session.

**Allowed staging posture:** Nest primary for Web; Go optional for isolated POS experiments after availability deploy; Mobile stays on Nest.

## Update — decimal remediation pass

- Implemented dual-scale `pkg/money` (4dp totals, 6dp unit cost)
- Implemented decimal WAC in `pkg/costing` and projections receive path
- Receiving / waste / FG / POS food cost / M-Pesa amount use Money helpers
- Added recipe routes: categories, by item, availability
- **Still NOT CERTIFIED** as sole Nest replacement (live differential + concurrency + realtime + full float edge elimination pending)
