package realtime

// Broadcaster is the only realtime dependency domain/projections may use.
// Transports (native WS, Socket.IO) are adapters behind Hub.
type Broadcaster interface {
	// Broadcast emits to the default channel for a shop (Nest gateway fan-out).
	Broadcast(shopID, event string, payload interface{})
	// BroadcastNamespace targets a Nest Socket.IO-style namespace (e.g. "inventory", "pos").
	BroadcastNamespace(ns, shopID, event string, payload interface{})
}

// Ensure Hub implements Broadcaster at compile time.
var _ Broadcaster = (*Hub)(nil)
