# RestFlow Go Replacement Certification

**Status: NOT CERTIFIED**

Date: 2026-09-29  
Basis: RestFlowGo-inventory-parity + forensic shop fail-closed fixes  

## Certification table

| Area | Audited | Differences | Fixed | Remaining | Evidence | Status |
|------|---------|-------------|-------|-----------|----------|--------|
| API routes | Yes | Depth gaps residual | Partial | Menu/recipe depth | Route extract | PARTIAL |
| Projections double-apply | Yes | Kitchen→sale | Yes | — | engine.go case | PASS |
| POS server money | Yes | Client total | Yes | — | pos/service.go | PASS |
| Combo category | Yes | Missing | Yes | — | resolution.go | PASS |
| Inventory expiry logic | Yes | Stale days | Yes | — | inventory/service.go | PASS |
| JSON binding | Yes | Untagged structs | Yes | — | handlers bind | PASS |
| M-Pesa/Stripe shop | Yes | shop=1 | Yes | — | payments/service.go | PASS |
| Shop default "1" | Yes | Widespread | Critical paths | Soft service defaults | auth+handlers | PARTIAL |
| Concurrency | Code | Race | Code fixed | Live race test | lock+UPDATE | CODE_PASS |
| Live Nest differential | No | — | — | All | Env blocked | BLOCKED |
| Dashboard numbers | Partial | Unknown | — | Numeric parity | — | UNVERIFIED |
| Realtime | Partial | Names | — | Client proof | /ws | UNVERIFIED |
| Workers | Partial | — | — | — | — | UNVERIFIED |
| Historical Nest data | No | — | — | Replay | Env | BLOCKED |

## Commands

```
go build ./cmd/api → PASS
go test ./internal/inventory ./internal/projections ./pkg/money ./tests/unit → PASS
```

## Rule

NOT CERTIFIED until live Postgres lifecycle + concurrent stock + Nest numeric parity pass.
