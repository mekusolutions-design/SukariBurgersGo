# Realtime architecture (Nest-compatible, non-invasive)

```
Domain services
    → EventStore.Append
    → ProjectionEngine (same TX)
    → Broadcaster.BroadcastNamespace(...)   // port only
            ├─ Hub native WS (/ws, /ws/{ns})
            └─ Socket.IO adapter (/socket.io/)  // transport only
```

## Rules enforced in code
1. Domain / projections import `realtime.Broadcaster` only — never `realtime/socketio`.
2. Socket.IO lives in `internal/realtime/socketio` and is attached to `Hub.Sio` in `main`.
3. REST paths (`/inventory`, `/pos`, …) are never used for Engine.IO.
4. Emits happen **after** successful projection (append → project → emit).
5. Event names match Nest: `stock_updated`, `inventory_updated`, `low_stock_alert`, …

## Routes
| Path | Role |
|------|------|
| `GET /inventory` etc. | REST |
| `GET /socket.io/` | Engine.IO + Socket.IO (websocket) |
| `GET /ws`, `GET /ws/{ns}` | Native WebSocket |

## Wire-up (`cmd/api/main.go`)
```go
hub := realtime.NewHub(log, authSvc)
sio := socketio.NewServer(log, authSvc)
hub.Sio = sio
projEngine.SetBroadcaster(hub)
```
