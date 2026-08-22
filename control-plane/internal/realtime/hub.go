package realtime

import (
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/luke/hive/control-plane/internal/db"
)

// Hub manages websocket clients and broadcasts notifications to them.
type Hub struct {
	upgrader websocket.Upgrader
	mu       sync.Mutex
	clients  map[*websocket.Conn]struct{}
}

// NewHub returns a ready-to-use Hub.
func NewHub() *Hub {
	return &Hub{
		upgrader: websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
		clients:  map[*websocket.Conn]struct{}{},
	}
}

// HandleWS upgrades a request to a websocket client connection.
func (h *Hub) HandleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	_ = conn.SetReadDeadline(time.Now().Add(45 * time.Second))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(45 * time.Second))
	})
	h.mu.Lock()
	h.clients[conn] = struct{}{}
	h.mu.Unlock()
	go func() {
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if err := conn.WriteControl(websocket.PingMessage, []byte("ping"), time.Now().Add(5*time.Second)); err != nil {
				h.mu.Lock()
				delete(h.clients, conn)
				h.mu.Unlock()
				_ = conn.Close()
				return
			}
		}
	}()
	go func() {
		defer func() {
			h.mu.Lock()
			delete(h.clients, conn)
			h.mu.Unlock()
			_ = conn.Close()
		}()
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()
}

// Broadcast delivers a notification to every connected client.
func (h *Hub) Broadcast(n db.Notification) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		if err := c.WriteJSON(n); err != nil {
			_ = c.Close()
			delete(h.clients, c)
		}
	}
}
