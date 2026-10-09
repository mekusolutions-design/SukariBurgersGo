# High-level prompt: Go POS menu sync with Nest (once and for all)

## Problem
POS clients point at Go (`NEXT_PUBLIC_POS_API_URL` + `USE_GO_POS`). Nest remains system of record for menu_created / menu_item_added on the **same PostgreSQL**. Go POS showed "No menu items configured yet" because ListMenus/bootstrap returned empty while Nest listMenus still had items.

## Objective
Go POS must list the **same menus** Nest lists for a shop, with a web-safe shape, without dual-writing menus.

## Contract
1. **Read path only** for menus on Go — fold Event rows Nest wrote; optional CatalogMenuItem merge.
2. **Event types:** menu_created, menu_item_added (+ legacy aliases).
3. **Shop scope:** prefer Event.shop_id = JWT/query shop; if zero rows, fall back to all menu events and filter payload.shop_id.
4. **IDs:** menu_id / menuId / MENU-{menu_code}.
5. **Response shapes:**
   - GET /pos/bootstrap → `{ success, menus, items, count, availability, combos, categories }`
   - GET /menu → `{ success, items, menus, count }`
   - GET /pos/menu-availability → `{ success, items, count }`
6. **Line safety:** every line has `options: []` (never undefined) so web `.length` never crashes.
7. **Inventory Select:** option_item_ids expanded to options; POS sale deducts those SKUs.
8. **Auth:** same JWT_SECRET; shop_id from token or `?shopId=`.

## Done when
- Nest has N menus for shop 1 → Go bootstrap.menus.length === N (same menuIds).
- POS no longer shows empty state when Nest menu module has items.
- Sell still deducts FIXED + inventory-select components.
