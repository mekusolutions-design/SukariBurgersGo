# Nest depth: resolveLines + FEFO

## Production resolveLines
- Loads production_started context (recipe_id, planned qty, item)
- Scales RecipeIngredient by actual/standard_yield when inputs[] empty
- Fills outputs from RecipeOutput or primary FG when outputs[] empty
- Multi-output: leaves actual_quantity 0 until client sends counts (Nest rule)

## FEFO batch consume
- `internal/fefo.ConsumeStock`: expiry ASC NULLS LAST, FOR UPDATE
- Seeds synthetic lot from projection if open lots missing
- Wired into `projections.deductStock` → POS, waste, production inputs all FEFO

## Paths covered by FEFO deduct
- pos_sale stock_deductions
- waste_recorded
- production_finished inputs
- production stock_deductions
