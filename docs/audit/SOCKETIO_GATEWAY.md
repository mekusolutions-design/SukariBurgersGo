# Socket.IO gateway (Nest realtime parity)

## Endpoint
- Engine.IO / Socket.IO: `GET /socket.io/?EIO=4&transport=websocket`
- Transport: **websocket only** (matches mobile `transports: ['websocket']`)

## Namespaces (Nest)
`/`, `/inventory`, `/pos`, `/waste`, `/production`, `/payment`, `/refill`, `/finished-goods`, `/notifications`, `/menu`

## Client (unchanged Nest mobile)
```ts
io(`${API_BASE_URL}/inventory`, {
  auth: { token },
  transports: ['websocket'],
});
```

## Auth
- `handshake.auth.token` (CONNECT packet JSON)
- or `?token=`
- or `Authorization: Bearer`

## Rooms
- On connect / `join_shop` → room `namespace + shopId`
- Server emits: `stock_updated`, `inventory_updated`, `low_stock_alert`, `order_created`, etc.

## Also available
- Native WS: `/ws` and `/ws/{namespace}` (non-Socket.IO clients)

## Limitation
- HTTP long-polling Engine.IO transport is not implemented (websocket-only).
