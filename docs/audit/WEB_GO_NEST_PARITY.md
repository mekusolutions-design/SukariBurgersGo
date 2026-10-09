# Web ↔ Go ↔ Nest parity (staging)

## POS 86'd (fixed in this package)
- **Cause:** availability used only `finished_good_id` on the event; most Nest menus resolve via **recipe / lines**.
- **Web rule:** `disabled={maxPortions <= 0 || !isAvailable}` → shows **86'd**.
- **Fix:** `ListMenuAvailability` uses `ResolveMenuItemLine` (same as create order) + inventory map for max portions.
- If still 86'd after deploy: stock for **ingredient SKUs** is truly 0, or recipe not linked on the menu event.

## Endpoints web calls most (must stay Nest-shaped)

| Area | Web path | Go must return |
|------|----------|----------------|
| POS availability | GET /pos/menu-availability | `[{menuItemId,name,isAvailable,maxPortions}]` |
| Menu list | GET /menu | `{items:[{menuId,name,sellingPrice,lines,...}]}` |
| Waste analytics | GET /waste/analytics?from&to | totals + byCause + topItems |
| Waste events | GET /waste/events?from&to | `{items,total,page}` |
| Consumption | GET /consumption/summary?from&to | totalConsumedValue, topProducts, topMenuItems |
| Variance | GET /variance/batches | `{items:[{batchId,countedBy,totalVarianceValue,...}]}` |
| Trace | GET /trace | events; join **users** not User |
| Dashboard | summary/kpis/alerts | camelCase Nest fields |

## Business logic alignment
- **POS create:** server-side resolution + stock check (not client deductions) — aligned.
- **COGS / consumption:** from pos_sale stock_deductions — aligned when events have rich payloads.
- **Variance:** event fold like Nest — aligned in this package.
- **Date filters:** from/to on waste, consumption, dashboard KPIs — aligned in this package.

## Still not sole-Go certified
Realtime namespaces, concurrent load test, mobile cutover, full recipe-cost parity under edge cases.
