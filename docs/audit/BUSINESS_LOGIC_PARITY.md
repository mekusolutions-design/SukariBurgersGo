# BUSINESS_LOGIC_PARITY.md

## Method

Compared Nest services under `apps/api/src/modules/**` with Go `internal/**` + `pkg/costing` + `pkg/money` + projections engine. Classification per §62.

## Inventory / valuation

| Topic | Nest | Go | Class | Risk | Fix status |
|-------|------|-----|-------|------|------------|
| Available stock | InventoryProjection | same table | F | Low | OK if same DB |
| WAC / avg_unit_cost | Decimal on projection | money pkg + SQL | A residual | Medium | Use money end-to-end; avoid Float64 in API calc |
| Receive | event `goods_received`/`received` + projection | receiving service + lock | C/F | Medium | Verify event_type string equality with Nest |
| POS deduction | resolve recipe/FG then decrement | resolve + CheckAvailability + projection | A residual | High if race | advisory lock + FOR UPDATE helpers exist; load test pending |
| Waste value | unit_cost × qty or total_cost | analytics from events | A residual | Medium | date filters added |
| Closing stock | event + variance feed | kitchen + variance events | PARTIAL | High for MEKU | variance list fixed to event fold |

## POS / COGS / menu

| Topic | Nest | Go | Class | Risk | Fix status |
|-------|------|-----|-------|------|------------|
| Menu resolution | sellable-resolution + listMenus | ResolutionService | PARTIAL | High | availability now uses ResolveMenuItemLine |
| Client price trust | server authoritative | server resolve UnitPrice | F/C | High if bypassed | keep ignoring client COGS/deductions |
| maxPortions | stock/need floor | same after fix | A fixed | High (86'd) | FIXED |
| Combo groups | category membership | ResolveComboLine | PARTIAL | High | needs golden tests |
| pos_sale side effects | once per sale | eventstore + projection | PARTIAL | Critical | idempotency key + lock |
| send-to-kitchen | separate event | separate handler | PARTIAL | High | must not re-run pos_sale |

## Dashboard / consumption

| Topic | Nest | Go | Class | Risk | Fix status |
|-------|------|-----|-------|------|------------|
| KPI food cost % | period COGS/revenue | sum event costs + payload | A residual | Medium | from/to wired |
| Date boundaries | UTC day keys | UTC parseDateRange | PARTIAL | Medium | align shop TZ later |
| Consumption | recipe×sales | stock_deductions fold | PARTIAL | Medium | empty if POS payloads lack deductions |

## Payments

| Topic | Nest | Go | Class | Risk | Fix status |
|-------|------|-----|-------|------|------------|
| M-Pesa callback | secret + amount bind | HandleMpesaCallback | PARTIAL | Critical | no hard-coded shop in callback path required |
| Stripe webhook | signature | HandleStripeWebhook | PARTIAL | Critical | unit tests exist |
| Initiate Stripe | Nest POST /payment/stripe | missing route | A | Medium | implement or proxy Nest |

## Expected business formulas (from RestFlow usage)

```
available ≈ projection.available_stock
inventory_value ≈ projection.total_value
waste_value ≈ Σ waste event value (or WasteProjection)
food_cost% ≈ period_cogs / period_revenue × 100
max_portions ≈ min_i floor(stock_i / qty_per_portion_i)
```

Do not invent alternate MEKU formulas without product sign-off.

## Residual high risks

1. Concurrent oversell under load (needs integration race test on shared DB).  
2. Realtime channel parity.  
3. Historical Nest event payload variance (Go parsers must remain defensive).  
4. Float64 at JSON boundary vs Decimal(18,4).  
5. Mobile notifications settings endpoint.
