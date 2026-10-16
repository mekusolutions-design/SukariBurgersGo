# POS availability vs Nest Menu (follow-up)

## Logs interpretation
- Go `GET /pos/bootstrap` 200 on sukariburgersgo.onrender.com → web POS chips use Go.
- Nest `GET /menu?shopId=1` → Menu page uses Nest listMenus.
- Burgers still all "7 left" on POS → Go was not applying recipe ingredient limits (bacon g / cheddar g).

## Fix
Before computing maxPortions, expand RecipeIngredient for:
- each FIXED finished_good_id
- CatalogMenuItem.recipe_id
- menu id / code / name keys

Then min across those constraints (same rule as ops brief).

## Deploy
Push this tree; confirm Render redeploy; hard-refresh POS.
Cheese burgers with exhausted cheddar should show 0 / 86'd, not 7.
