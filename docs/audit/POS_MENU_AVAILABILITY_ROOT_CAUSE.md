# POS ↔ Menu availability — root cause & fix (16 Oct 2026)

## Root cause
1. **Go did not fold `menu_adjusted` events.** Nest persists full multi-line bills of materials (lettuce + bacon + cheddar + …) on `menu_adjusted`. Go only read `menu_created` / `menu_item_added` / `menu_updated`, so many burgers kept a **partial** line set (often only the shared base, e.g. lettuce → 7 portions for every variant).
2. **Availability formula** is Nest’s min-across-FIXED-lines; Go now uses `ComputeMenuAvailability` for the same rule.
3. **Name-based stock fallback** is restricted to menus **without** any SKU constraints so it cannot override burger mins.

## Formula (single source of truth)
```
maxPortions = min over constraints of floor(stock(item_id) / qty_required)
```

## Tests
Ops table (Single Patty, Bacon, Cheese, Double variants) + standalone BACON isolation + soda FIXED — all PASS.

## Verify after deploy
Menu page Available N === POS chip “N left” for:
Single Patty, Single Patty Bacon, Single Patty Cheese, Double Patty Double Bacon/Cheese.
