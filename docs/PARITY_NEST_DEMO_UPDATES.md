# RestFlowGo ↔ Nest parity (demo updates)

Aligned with Nest fixes used in production web demo:

## 1. Inventory WAC exposure
- `StockRow` returns `avg_unit_cost`, `last_receipt_unit_cost`, `cost_missing`
- Derives WAC from `total_value / available_stock` when avg is unset

## 2. Menu create — inventory select
- `POST /menu` with `lines[]` creates `menu_created` event (Nest contract)
- CHOICE / MULTI_CHOICE with `option_item_ids` does **not** require FG category
- Fixed components still require `finished_good_id`

## 3. Closing stock
- Multi-line body: `{ shop_id, shift_label, notes, lines: [{ item_id, counted_qty }] }`
- Variance valued at live WAC
- Projection applies once (skips `stock_count_completed` alias)
- GET closing form includes `unit_cost` / `expected_value`

## 4. Wiring
- `ResolutionService.SetEventStore` from `cmd/api/main.go`

## Apply
Overlay these paths onto RestFlowGo-main, then `go build ./...` and redeploy.
