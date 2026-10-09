# Go Remediation (forensic pass)

## Applied now
1. auth.RequireShopID — no silent shop "1" from empty JWT
2. POS CreateOrder — empty shop → Forbidden
3. M-Pesa STK — empty shop → Forbidden  
4. EventStore — mutating events require shop_id in payload
5. Inventory List/Summary/Alerts/Batches — JWT shop required

## Confirmed present from prior packages
- Only pos_sale applies sale side effects
- Kitchen payload without stock_deductions
- Server totals + split vs serverTotalAmount
- Combo category membership
- Advisory lock + conditional stock deduct
- Inventory live next_expiry_date
- Dual snake/camel JSON binding

## Classification of major Nest vs Go differences
| Issue | Class | Action |
|-------|-------|--------|
| Kitchen event re-applying sale | B Nest risk | Go fixed (do not copy) |
| Client-authoritative price | B Nest risk | Go fixed |
| Inventory stale days_to_expiry_min | A Go was wrong | Go fixed to match Nest live days |
| production_id JSON tags | A Go | Fixed map decode |
| shop_id="1" fallbacks | A Go | Fail-closed on critical paths |
