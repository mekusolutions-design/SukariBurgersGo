package realtime

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/restflow/api-go/internal/auth"
	"github.com/restflow/api-go/internal/realtime/socketio"
	apperrors "github.com/restflow/api-go/pkg/errors"
)

// Nest Socket.IO namespaces we mirror as WS paths (and /ws?ns=).
var NestNamespaces = []string{
	"inventory", "pos", "waste", "production", "payment",
	"refill", "finished-goods", "notifications", "menu",
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

type client struct {
	hub       *Hub
	conn      *websocket.Conn
	shopID    string
	userID    string
	namespace string
	send      chan []byte
}

type Hub struct {
	mu      sync.RWMutex
	// key: namespace + ":" + shopID
	rooms   map[string]map[*client]struct{}
	log     *slog.Logger
	authSvc *auth.Service
	Sio     *socketio.Server // Nest Socket.IO gateway (optional)
}

func NewHub(log *slog.Logger, authSvc *auth.Service) *Hub {
	return &Hub{
		rooms:   map[string]map[*client]struct{}{},
		log:     log,
		authSvc: authSvc,
	}
}

func roomKey(ns, shopID string) string {
	if ns == "" {
		ns = "default"
	}
	if shopID == "" {
		shopID = "1"
	}
	return ns + ":" + shopID
}

func (h *Hub) BroadcastTyped(shopID, event string, payload interface{}) {
	h.BroadcastNamespace("", shopID, event, payload)
}

func (h *Hub) Broadcast(shopID, event string, payload interface{}) {
	h.BroadcastNamespace("", shopID, event, payload)
}

// BroadcastNamespace emits to clients subscribed to a Nest-style namespace.
func (h *Hub) BroadcastNamespace(ns, shopID, event string, payload interface{}) {
	msg, err := json.Marshal(map[string]interface{}{
		"event":     event,
		"type":      event,
		"namespace": ns,
		"data":      payload,
		"ts":        time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	// Exact namespace room
	if ns != "" {
		for c := range h.rooms[roomKey(ns, shopID)] {
			select {
			case c.send <- msg:
			default:
			}
		}
	}
	// Also fans out to default /ws subscribers (backward compatible)
	for c := range h.rooms[roomKey("", shopID)] {
		select {
		case c.send <- msg:
		default:
		}
	}
	// Inventory events also go to inventory namespace listeners
	if ns == "" && (strings.Contains(event, "stock") || strings.Contains(event, "inventory") || event == "low_stock_alert") {
		for c := range h.rooms[roomKey("inventory", shopID)] {
			select {
			case c.send <- msg:
			default:
			}
		}
	}
	// Socket.IO Nest clients
	if h.Sio != nil {
		sioNS := ns
		if sioNS == "" {
			if strings.Contains(event, "stock") || strings.Contains(event, "inventory") || event == "low_stock_alert" || event == "dashboard_alert" {
				sioNS = "inventory"
			} else if strings.Contains(event, "order") || event == "order_created" {
				sioNS = "pos"
			} else if strings.Contains(event, "waste") {
				sioNS = "waste"
			} else if strings.Contains(event, "production") {
				sioNS = "production"
			} else if strings.Contains(event, "payment") {
				sioNS = "payment"
			} else if strings.Contains(event, "refill") {
				sioNS = "refill"
			} else {
				sioNS = "inventory"
			}
		}
		h.Sio.EmitToShop("/"+strings.TrimPrefix(sioNS, "/"), shopID, event, payload)
		// dual-emit Nest event names without namespace prefix listeners on /
		h.Sio.EmitToShop("/", shopID, event, payload)
	}
}

func (h *Hub) ConnectionCount(shopID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	n := 0
	for k, m := range h.rooms {
		if strings.HasSuffix(k, ":"+shopID) {
			n += len(m)
		}
	}
	return n
}

func (h *Hub) register(c *client) {
	key := roomKey(c.namespace, c.shopID)
	h.mu.Lock()
	if h.rooms[key] == nil {
		h.rooms[key] = map[*client]struct{}{}
	}
	h.rooms[key][c] = struct{}{}
	h.mu.Unlock()
	h.log.Info("ws connected", "namespace", c.namespace, "shop_id", c.shopID, "user_id", c.userID)
}

func (h *Hub) unregister(c *client) {
	key := roomKey(c.namespace, c.shopID)
	h.mu.Lock()
	if m := h.rooms[key]; m != nil {
		if _, ok := m[c]; ok {
			delete(m, c)
			close(c.send)
		}
	}
	h.mu.Unlock()
}

func (c *client) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *client) readPump() {
	defer func() {
		c.hub.unregister(c)
		c.conn.Close()
	}()
	c.conn.SetReadLimit(1 << 20)
	_ = c.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		return nil
	})
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		// Nest-style join_shop / ping
		var msg map[string]interface{}
		if json.Unmarshal(data, &msg) == nil {
			ev, _ := msg["event"].(string)
			if ev == "" {
				ev, _ = msg["type"].(string)
			}
			if ev == "join_shop" || ev == "join" {
				if sid, ok := msg["shopId"].(string); ok && sid != "" {
					c.hub.unregister(c)
					c.shopID = sid
					c.hub.register(c)
					ack, _ := json.Marshal(map[string]interface{}{
						"event": "room_joined", "type": "room_joined",
						"data": map[string]string{"room": "shop:" + sid, "namespace": c.namespace},
					})
					select {
					case c.send <- ack:
					default:
					}
				}
			}
			if ev == "ping" {
				pong, _ := json.Marshal(map[string]string{"event": "pong", "type": "pong"})
				select {
				case c.send <- pong:
				default:
				}
			}
		}
	}
}

// ServeHTTP upgrades WebSocket. Path may be /ws, /ws/{ns}, or Nest-like /{ns}.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ns := r.URL.Query().Get("ns")
	if ns == "" {
		ns = r.URL.Query().Get("namespace")
	}
	path := strings.Trim(r.URL.Path, "/")
	if ns == "" {
		parts := strings.Split(path, "/")
		if len(parts) >= 1 {
			p0 := parts[0]
			if p0 == "ws" && len(parts) >= 2 {
				ns = parts[1]
			} else if p0 != "ws" && p0 != "" {
				for _, n := range NestNamespaces {
					if p0 == n {
						ns = n
						break
					}
				}
			}
		}
	}

	token := r.URL.Query().Get("token")
	if token == "" {
		if ah := r.Header.Get("Authorization"); strings.HasPrefix(strings.ToLower(ah), "bearer ") {
			token = strings.TrimSpace(ah[7:])
		}
	}
	shopID := r.URL.Query().Get("shopId")
	if shopID == "" {
		shopID = r.URL.Query().Get("shop_id")
	}
	userID := ""
	if token != "" && h.authSvc != nil {
		claims, err := h.authSvc.ValidateToken(token)
		if err != nil {
			apperrors.WriteJSON(w, r, apperrors.Unauthorized("Invalid token"))
			return
		}
		userID = claims.Sub
		if shopID == "" && claims.ShopID != "" {
			shopID = claims.ShopID
		}
	}
	if shopID == "" {
		shopID = "1"
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.log.Warn("ws upgrade failed", "error", err)
		return
	}
	c := &client{
		hub: h, conn: conn, shopID: shopID, userID: userID,
		namespace: ns, send: make(chan []byte, 32),
	}
	h.register(c)
	welcome, _ := json.Marshal(map[string]interface{}{
		"event": "connected", "type": "connected",
		"data": map[string]interface{}{
			"message": "RestFlow real-time connected",
			"namespace": ns, "shopId": shopID,
		},
	})
	c.send <- welcome
	go c.writePump()
	c.readPump()
}

// HandleNamespace returns a handler fixed to a Nest namespace path (e.g. /inventory).
func (h *Hub) HandleNamespace(ns string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		q.Set("ns", ns)
		r2 := r.Clone(r.Context())
		r2.URL.RawQuery = q.Encode()
		h.ServeHTTP(w, r2)
	})
}
