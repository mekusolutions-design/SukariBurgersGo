# High-level prompt — POS inventory select + deduction chain (Michael 3 Oct 2026)

## Problems
A. Multi Inventory Select: each CHOICE group must reach its own Need (e.g. 200), not +1; single-option auto-fill.
B. Add dialog duplicates rows; no sticky footer — fix web layout (dedupe by component_key).
C. Combos: every menu item's every group validated (already partially fixed).
D. Ticket payment button state-aware (web).
E. Sale blocks on raw CHICKEN BREAST while tile says "5 left" — POS expands FG recipes to ingredients.

## Domain rule (non-negotiable)
```
POS sale
  → menu item components only
  → FIXED finished_good_id  → deduct FG stock (qty_required × sold_qty)
  → Inventory select choice → deduct chosen inventory SKU only
  → NEVER expandRecipe / NEVER raw ingredients of an FG
Raw materials were deducted at production time.
Availability "left" MUST use the same component stock math as sale.
```

## Implement in Go ResolveMenuItemLine + deductionsFromMenuLines
1. Remove expandRecipe from POS sale path (FIXED, CHOICE, catalog FG, legacy header FG).
2. Always directStockDeduction on component SKU × (quantity_required × line qty).
3. Catalog production_type=recipe: deduct recipe's output item_id (FG) if present, else stock_item_id — not ingredients.
4. Fallback expandRecipe only if there are zero component lines and id is purely a recipe code with no FG stock row.
5. Availability portionsFromLines already uses component stock — keep aligned.

## Web (if in scope)
- Selection counters: group need = quantityRequired; click adds need (or fills need), not +1.
- Dedupe dialog lines by component_key.
- Payment: paid → badge; unpaid → Mark paid.
