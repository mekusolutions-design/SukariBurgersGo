# Nest listMenus (SukariBurgers-main 3) vs Go POS availability

## Nest algorithm (menu.service.ts listMenus)
1. Events: `menu_created` | `menu_item_added` | `menu_adjusted` only (shop scoped, asc, take 2000).
2. Lines: `Map<componentKey, LineDraft>` per menu.
   - created: set lines from payload.lines or single FIXED finished_good_id
   - item_added: upsert one draft by componentKey
   - adjusted: **replace** entire map from payload.lines
3. maxPortions = min over lines:
   - FIXED/INVENTORY: floor(stock(finishedGoodId) / quantityRequired)
   - CHOICE: floor(sum(option stocks) / need)
4. **No recipe ingredient expansion** in listMenus.

## Why POS showed all burgers = 7 then all = 3
- Shared limiting line (e.g. lettuce) and/or **one recipe expanded onto every "Patty" menu by name**.
- Go recipe enrich must NOT use menu display names as recipe keys.

## Go fix in this package
- Line fold matches Nest Map + menu_adjusted replace.
- Recipe enrich only for explicit Catalog `recipe_id` and exact FG recipe keys (no name matching).
- ComputeMenuAvailability still min across all FIXED constraints.

## Verify
Nest GET /menu Available N === Go GET /pos/bootstrap maxPortions for:
Single Patty, Single Patty Bacon, Single Patty Cheese (must be allowed to differ).
