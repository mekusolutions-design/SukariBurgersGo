# SukariBurgersGo — POS parity updates (real repo)

Applied to **SukariBurgersGo-main** (production Go source).

## Changes

1. **`internal/pos/service.go`**
   - Combo lines enrich `combo_selections` with `menu_item_names` and `group_name` (Nest kitchen ticket parity).
   - Payload includes `resolved_menu_items`, `menu_id`, `item_id`, `selling_price`.
   - Menu item lines also emit Nest-shaped `menu_id` / `item_id` / `selling_price` (server catalog price).

2. **`internal/http/router/router.go`**
   - `GET /pos/bootstrap` uses `rolePOSKit` (POS + Manager + Admin + **Kitchen**), matching Nest.

## Tests
```bash
export GOPROXY=https://proxy.golang.org,direct
go test ./internal/pos/ ./internal/menu/ ./pkg/... ./tests/unit/ -count=1
go build -o restflow-api ./cmd/api
```
