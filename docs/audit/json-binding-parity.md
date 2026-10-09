# JSON binding parity (Nest/web ↔ Go)

## Root cause class
Go only binds JSON fields that match `json` struct tags (or exact exported name).
Nest/Zod and the web client send `snake_case` and sometimes `camelCase`.
Untagged fields silently stayed zero → false validation errors like `production_id required`.

## Production finish
Already fixed in uploaded zip (map decode + dual keys).

## Fixed in this pass
| Handler | Change |
|---------|--------|
| payment M-Pesa STK | map decode: order_id/orderId, phone, amount |
| waste record | map decode dual case |
| recipe create / ingredient | map decode dual case |
| menu create / category / FG assign | map decode dual case |
| refill request / issue | map decode dual case |
| kitchen closing stock | map decode dual case |
| waste write-off | map decode dual case |
| notifications create | map decode dual case |
| received | map decode dual case |
| kitchen status | status / kitchen_status |
| POS create order | dual snake+camel tags + coalesce |
| ComboSelectionInput | UnmarshalJSON for group_index/groupIndex, menu_item_ids/menuItemIds |

## Shared helpers
`internal/http/handlers/bind.go`: `decodeMap`, `mapString`, `mapFloat`

## Verify
```
go build ./cmd/api → PASS
go test unit packages → PASS
```
