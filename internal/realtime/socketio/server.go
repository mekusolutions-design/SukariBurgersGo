// Package socketio implements a minimal Engine.IO v4 + Socket.IO v4 server
// for websocket transport only — compatible with socket.io-client when
// transports: ['websocket'] (Nest web/mobile clients).
package socketio

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/mekusolutions-design/SukariBurgersGo/internal/auth"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return true },
}

// Nest namespaces used by RestFlow clients.
var Namespaces = []string{
	"/", "/inventory", "/pos", "/waste", "/production", "/payment",
	"/refill", "/finished-goods", "/notifications", "/menu",
}

type Server struct {
	mu sync.RWMutex
	// key: namespace + "\x00" + shopID
	rooms   map[string]map[*Conn]struct{}
	log     *slog.Logger
	authSvc *auth.Service
}

type Conn struct {
	srv       *Server
	ws        *websocket.Conn
	sid       string
	namespace string
	shopID    string
	userID    string
	send      chan string
	closed    chan struct{}
}

func NewServer(log *slog.Logger, authSvc *auth.Service) *Server {
	return &Server{
		rooms:   map[string]map[*Conn]struct{}{},
		log:     log,
		authSvc: authSvc,
	}
}

func roomKey(ns, shop string) string {
	if ns == "" {
		ns = "/"
	}
	if !strings.HasPrefix(ns, "/") {
		ns = "/" + ns
	}
	if shop == "" {
		shop = "1"
	}
	return ns + "\x00" + shop
}

// EmitToShop mirrors Nest server.to(`shop:${id}`).emit(event, data).
func (s *Server) EmitToShop(namespace, shopID, event string, data interface{}) {
	payload, err := json.Marshal([]interface{}{event, data})
	if err != nil {
		return
	}
	// Socket.IO EVENT packet: 42[/namespace],payload
	ns := namespace
	if ns == "" {
		ns = "/"
	}
	if !strings.HasPrefix(ns, "/") {
		ns = "/" + ns
	}
	var packet string
	if ns == "/" {
		packet = "42" + string(payload)
	} else {
		packet = "42" + ns + "," + string(payload)
	}

	s.mu.RLock()
	defer s.mu.RUnlock()
	for c := range s.rooms[roomKey(ns, shopID)] {
		select {
		case c.send <- packet:
		default:
		}
	}
}

// EmitInventory is a convenience for stock/low-stock events.
func (s *Server) EmitInventory(shopID, event string, data interface{}) {
	s.EmitToShop("/inventory", shopID, event, data)
}

func (s *Server) register(c *Conn) {
	key := roomKey(c.namespace, c.shopID)
	s.mu.Lock()
	if s.rooms[key] == nil {
		s.rooms[key] = map[*Conn]struct{}{}
	}
	s.rooms[key][c] = struct{}{}
	s.mu.Unlock()
	s.log.Info("socket.io connected", "ns", c.namespace, "shop", c.shopID, "sid", c.sid)
}

func (s *Server) unregister(c *Conn) {
	key := roomKey(c.namespace, c.shopID)
	s.mu.Lock()
	if m := s.rooms[key]; m != nil {
		if _, ok := m[c]; ok {
			delete(m, c)
			close(c.send)
		}
	}
	s.mu.Unlock()
}

// ServeHTTP handles Engine.IO websocket endpoint: GET /socket.io/?EIO=4&transport=websocket
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	if q.Get("transport") != "" && q.Get("transport") != "websocket" {
		// We only support websocket; reject polling with clear message
		http.Error(w, "only websocket transport supported", http.StatusBadRequest)
		return
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Warn("socket.io upgrade failed", "error", err)
		return
	}

	sid := uuid.New().String()
	c := &Conn{
		srv: s, ws: ws, sid: sid,
		namespace: "/", shopID: "1",
		send: make(chan string, 64), closed: make(chan struct{}),
	}
	// Token from query (some clients) or Authorization header
	if tok := q.Get("token"); tok != "" {
		c.authenticate(`{"token":` + jsonString(tok) + `}`)
	} else if ah := r.Header.Get("Authorization"); strings.HasPrefix(strings.ToLower(ah), "bearer ") {
		c.authenticate(`{"token":` + jsonString(strings.TrimSpace(ah[7:])) + `}`)
	}

	// Engine.IO open packet (type 0)
	open, _ := json.Marshal(map[string]interface{}{
		"sid":          sid,
		"upgrades":     []string{},
		"pingInterval": 25000,
		"pingTimeout":  20000,
		"maxPayload":   1_000_000,
	})
	_ = ws.WriteMessage(websocket.TextMessage, []byte("0"+string(open)))

	go c.writeLoop()
	c.readLoop()
}

func (c *Conn) writeLoop() {
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.closed:
			return
		case msg, ok := <-c.send:
			if !ok {
				return
			}
			_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.ws.WriteMessage(websocket.TextMessage, []byte(msg)); err != nil {
				return
			}
		case <-ticker.C:
			// Engine.IO ping
			_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.ws.WriteMessage(websocket.TextMessage, []byte("2")); err != nil {
				return
			}
		}
	}
}

func (c *Conn) readLoop() {
	defer func() {
		close(c.closed)
		c.srv.unregister(c)
		_ = c.ws.Close()
	}()
	_ = c.ws.SetReadDeadline(time.Now().Add(90 * time.Second))
	for {
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			return
		}
		_ = c.ws.SetReadDeadline(time.Now().Add(90 * time.Second))
		msg := string(data)
		if msg == "" {
			continue
		}
		switch msg[0] {
		case '2': // ping → pong
			select {
			case c.send <- "3":
			default:
			}
		case '3': // pong
			// ignore
		case '4': // message — Socket.IO payload follows
			c.handleSocketPacket(msg[1:])
		case '0', '1': // open/close at engine level after handshake — ignore
		default:
			// ignore
		}
	}
}

// handleSocketPacket processes Socket.IO packet body (after Engine.IO '4' prefix).
// CONNECT = 40[/ns]{...} or 40[/ns],auth JSON
// EVENT = 42[/ns],["event", data]
func (c *Conn) handleSocketPacket(body string) {
	if body == "" {
		return
	}
	typ := body[0]
	rest := body[1:]

	ns := "/"
	payload := rest
	if strings.HasPrefix(rest, "/") {
		// namespace included
		if i := strings.Index(rest, ","); i >= 0 {
			ns = rest[:i]
			payload = rest[i+1:]
		} else {
			ns = rest
			payload = ""
		}
	}

	switch typ {
	case '0': // CONNECT
		c.namespace = ns
		c.authenticate(payload)
		c.srv.register(c)
		// CONNECT ack: 40[/ns]{sid}
		ackBody, _ := json.Marshal(map[string]string{"sid": c.sid})
		var ack string
		if ns == "/" {
			ack = "40" + string(ackBody)
		} else {
			ack = "40" + ns + "," + string(ackBody)
		}
		select {
		case c.send <- ack:
		default:
		}
		// Nest-style connected event
		c.emit(ns, "connected", map[string]interface{}{
			"message": "Inventory real-time connected",
			"shopId":  c.shopID,
		})

	case '2': // EVENT from client
		var args []interface{}
		if err := json.Unmarshal([]byte(payload), &args); err != nil || len(args) == 0 {
			return
		}
		ev, _ := args[0].(string)
		var data interface{}
		if len(args) > 1 {
			data = args[1]
		}
		c.onClientEvent(ev, data)

	case '1': // DISCONNECT
		return
	}
}

func (c *Conn) authenticate(payload string) {
	token := ""
	shopID := ""
	if payload != "" {
		var authObj map[string]interface{}
		if json.Unmarshal([]byte(payload), &authObj) == nil {
			if t, ok := authObj["token"].(string); ok {
				token = t
			}
			if s, ok := authObj["shopId"].(string); ok {
				shopID = s
			}
			if s, ok := authObj["shop_id"].(string); ok && shopID == "" {
				shopID = s
			}
		}
	}
	if token != "" && c.srv.authSvc != nil {
		if claims, err := c.srv.authSvc.ValidateToken(token); err == nil {
			c.userID = claims.Sub
			if shopID == "" && claims.ShopID != "" {
				shopID = claims.ShopID
			}
		}
	}
	if shopID == "" {
		shopID = "1"
	}
	c.shopID = shopID
}

func (c *Conn) onClientEvent(ev string, data interface{}) {
	switch ev {
	case "join_shop", "join":
		shop := c.shopID
		if m, ok := data.(map[string]interface{}); ok {
			if s, ok := m["shopId"].(string); ok && s != "" {
				shop = s
			}
			if s, ok := m["shop_id"].(string); ok && s != "" {
				shop = s
			}
		}
		c.srv.unregister(c)
		c.shopID = shop
		c.srv.register(c)
		c.emit(c.namespace, "room_joined", map[string]string{
			"room": "shop:" + shop,
		})
	case "ping":
		c.emit(c.namespace, "pong", map[string]string{"ok": "1"})
	}
}

func (c *Conn) emit(ns, event string, data interface{}) {
	payload, err := json.Marshal([]interface{}{event, data})
	if err != nil {
		return
	}
	var packet string
	if ns == "/" {
		packet = "42" + string(payload)
	} else {
		packet = "42" + ns + "," + string(payload)
	}
	select {
	case c.send <- packet:
	default:
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
