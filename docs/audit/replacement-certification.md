# Replacement Certification

## Status: NOT CERTIFIED

### Gates

| Gate | Result |
|------|--------|
| No P0 code defects remaining in identified paths | FIXED in code |
| No P0 proven with live integration tests | FAIL — no DATABASE_URL |
| No client financial authority | FIXED in POS code |
| No duplicate inventory/revenue on kitchen | FIXED in projection code |
| Payment forgery path closed | FIXED for configured secret; prod requires MPESA_CALLBACK_SECRET |
| Combo category membership | FIXED in code |
| Concurrency-safe inventory | FIXED conditional update |
| All web/mobile routes | PARTIAL |
| Realtime client parity | UNVERIFIED |
| COGS numeric parity | UNVERIFIED |
| Dashboard numeric parity | UNVERIFIED |
| Race detector + full go test ./... | PARTIAL (unit only) |

### Commands executed

```
go build ./cmd/api     → PASS
go test ./tests/unit/ ./pkg/money/ → PASS
```

### Answer to final question

If Nest is turned off tomorrow and all clients point at this Go build:

**Known residual risk paths remain:**
- Integration behavior on real menu/combo/production data not dual-tested
- Recipe/menu residual route gaps
- M-Pesa only secure when MPESA_CALLBACK_SECRET is set
- Realtime event name compatibility unproven
- COGS/dashboard numbers not proven equal to Nest

Therefore: **FULL REPLACEMENT VERIFIED = NO**
