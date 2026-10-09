# Certification progress (this package)

| Blocker | Status | Evidence |
|---------|--------|----------|
| Live Trace acceptance | CODE READY | GET /trace + /trace/diagnostics; needs live DB |
| Dashboard COGS parity | IMPROVED | KPIs: foodCostPercent, periodCogs, periodRevenue, wasteValue |
| Concurrent stock race | HARDENED | ReserveStock + CheckAvailabilityTx FOR UPDATE; needs load test |
| Mobile full contract | AUDITED | aliases /pos/orders, /waste; cutover not certified |
| Realtime parity | PARTIAL | single /ws hub + event name field; Nest namespaces not fully mirrored |

## Final status
```
NOT CERTIFIED — REPLACEMENT BLOCKERS REMAIN
```

Run on staging with shared Supabase:
1. GET /trace/diagnostics?itemId=YOUR_SKU
2. GET /dashboard/kpis — compare Nest vs Go numbers
3. Two parallel POS buys of last unit
4. Mobile against Go URL smoke
5. WS subscribe + stock_updated after receive
