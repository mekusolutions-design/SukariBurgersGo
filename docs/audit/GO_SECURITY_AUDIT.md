# GO_SECURITY_AUDIT.md

| Area | Finding | Severity | Status |
|------|---------|----------|--------|
| JWT auth | protect() on routes | High | Present |
| Role guards | roleMgr/rolePOS/roleKitchen | High | Present; matrix not fully proven equal to Nest |
| Tenant isolation | ShopIDFromContext; query shopId often fallback "1" | Critical | PARTIAL — default shop "1" is Nest legacy defect risk |
| M-Pesa callback | secret header/query | Critical | Implemented; verify prod secret required |
| Stripe webhook | signature header | Critical | Implemented + unit tests |
| IDOR | order/item by id without shop check | High | Audit per handler still incomplete |
| CORS | config driven | Medium | Verify prod origins |
| Rate limit | not verified in scan | Medium | Gap |
| Error leakage | internal errors may surface | Medium | PARTIAL |

**Do not certify security parity.**
