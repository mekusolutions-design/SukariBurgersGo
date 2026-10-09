# Remediation Matrix

| Finding | Severity | Location | Root Cause | Fix | Regression Test | Status |
|---------|----------|----------|------------|-----|-----------------|--------|
| Double inventory/revenue on kitchen events | P0 | projections/engine.go | case EventPosSale, EventOrderSentToKitchen → handlePosSale | Only EventPosSale calls handlePosSale; kitchen/status/payment no-op | tests/unit/p0_projection_test.go | FIXED (code+unit) |
| Kitchen payload copied stock_deductions | P0 | pos/service.go SendToKitchen | Blind copy of order payload | Kitchen snapshot without stock_deductions/cogs | manual path | FIXED (code) |
| Client unit_price / total_amount authoritative | P0 | pos/service.go | Trusted line.UnitPrice & in.TotalAmount | Server catalog/combo price; server line totals | unit + code review | FIXED (code) |
| M-Pesa callback shop_id=1, no secret | P0 | payments/service.go | Hardcoded shop, no auth | Callback secret fail-closed in prod; lookupCheckout for shop/order | code review | FIXED (code) — live callback UNVERIFIED |
| Stripe shop_id=1 | P1 | payments HandleStripeWebhook | Hardcoded | metadata.shop_id required | code review | FIXED (code) |
| Concurrent oversell | P0 | projections deductStock | GREATEST(0, stock-qty) | WHERE available_stock >= qty | code review | FIXED (code) — race test needs DB |
| Combo wrong category | P0 | menu/resolution.go | Count-only validation | assertMenuItemInCategory | code review | FIXED (code) |
| COGS/dashboard parity | P1 | multi | Never proven vs Nest | — | — | UNVERIFIED |
| Full menu Nest routes | P1 | menu | Incomplete | partial | — | UNVERIFIED |
| Live dual Nest↔Go | P0 gate | env | No shared DB harness | — | — | UNVERIFIED |
| Mobile sync / workers / realtime names | P1 | multi | Incomplete proof | — | — | UNVERIFIED |
