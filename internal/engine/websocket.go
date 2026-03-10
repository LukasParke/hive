package engine

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/lholliger/hive/internal/monitor"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// ---------- WebSocket Hub (broadcast pattern) ----------

// wsClient wraps a websocket.Conn with a per-connection mutex to prevent
// concurrent writes, which gorilla/websocket does not support.
type wsClient struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (c *wsClient) writeMessage(messageType int, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return c.conn.WriteMessage(messageType, data)
}

type wsHub struct {
	mu      sync.RWMutex
	clients map[*wsClient]struct{}
}

func newWSHub() *wsHub {
	return &wsHub{clients: make(map[*wsClient]struct{})}
}

func (h *wsHub) add(c *wsClient) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *wsHub) remove(c *wsClient) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
}

func (h *wsHub) broadcast(data []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		if err := c.writeMessage(websocket.TextMessage, data); err != nil {
			c.conn.Close()
			go h.remove(c)
		}
	}
}

func (h *wsHub) count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// ---------- Metrics Hub (singleton) ----------

var (
	metricsHub     *wsHub
	metricsHubOnce sync.Once
)

func getMetricsHub() *wsHub {
	metricsHubOnce.Do(func() {
		metricsHub = newWSHub()
	})
	return metricsHub
}

// StartMetricsBroadcast should be called once at engine startup to push metrics
// snapshots to all connected WebSocket clients.
func (s *Server) StartMetricsBroadcast(ctx context.Context, intervalSec int) {
	hub := getMetricsHub()
	if intervalSec <= 0 {
		intervalSec = 5
	}
	ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if hub.count() == 0 {
				continue
			}

			cluster := s.fetchClusterSummary(ctx)
			nodes := s.fetchNodeMetrics(ctx)
			topContainers := s.fetchTopContainers(ctx, 10)
			services, _ := monitor.ServiceHealthCache.GetAll()

			data, _ := json.Marshal(map[string]any{
				"type":          "metrics",
				"cluster":       cluster,
				"nodes":         nodes,
				"topContainers": topContainers,
				"services":      services,
				"ts":            time.Now().Unix(),
			})
			hub.broadcast(data)
		}
	}
}

// wsMetrics handles /ws/metrics — streams metrics to the browser.
func (s *Server) wsMetrics(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Warnw("ws upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	client := &wsClient{conn: conn}
	hub := getMetricsHub()

	// Send initial snapshot before adding to hub so there's no race
	cluster := s.fetchClusterSummary(r.Context())
	nodes := s.fetchNodeMetrics(r.Context())
	topContainers := s.fetchTopContainers(r.Context(), 10)
	services, _ := monitor.ServiceHealthCache.GetAll()
	initial, _ := json.Marshal(map[string]any{
		"type":          "metrics",
		"cluster":       cluster,
		"nodes":         nodes,
		"topContainers": topContainers,
		"services":      services,
		"ts":            time.Now().Unix(),
	})
	_ = client.writeMessage(websocket.TextMessage, initial)

	hub.add(client)
	defer hub.remove(client)

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

// wsLogs handles /ws/logs/{appId} — streams live container logs.
func (s *Server) wsLogs(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}

	appID := chi.URLParam(r, "appId")
	app, err := s.store.GetApp(r.Context(), appID)
	if handleErr(w, err) {
		return
	}

	conn, upgradeErr := upgrader.Upgrade(w, r, nil)
	if upgradeErr != nil {
		s.log.Warnw("ws upgrade failed", "error", upgradeErr)
		return
	}
	defer conn.Close()

	tail := r.URL.Query().Get("tail")
	if tail == "" {
		tail = "200"
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// Read from client to detect close
	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				cancel()
				return
			}
		}
	}()

	// Resolve service name: try hive-app-{name}, then {name}
	serviceName := "hive-app-" + app.Name
	logReader, logErr := s.sc.ServiceLogs(ctx, serviceName, tail, true)
	if logErr != nil {
		logReader, logErr = s.sc.ServiceLogs(ctx, app.Name, tail, true)
		if logErr != nil {
			msg, _ := json.Marshal(map[string]string{"error": "failed to open logs: " + logErr.Error()})
			_ = conn.WriteMessage(websocket.TextMessage, msg)
			return
		}
	}
	defer logReader.Close()

	buf := make([]byte, 8192)
	for {
		n, readErr := logReader.Read(buf)
		if n > 0 {
			// Docker log stream has an 8-byte header per line; strip it for clean output
			line := stripDockerLogHeader(buf[:n])
			msg, _ := json.Marshal(map[string]any{
				"type": "log",
				"data": string(line),
			})
			_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if writeErr := conn.WriteMessage(websocket.TextMessage, msg); writeErr != nil {
				return
			}
		}
		if readErr != nil {
			if readErr != io.EOF {
				s.log.Debugw("log stream ended", "app", appID, "error", readErr)
			}
			return
		}
	}
}

// wsEvents handles /ws/events — deployment status & service events.
func (s *Server) wsEvents(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Warnw("ws upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	client := &wsClient{conn: conn}
	hub := getEventsHub()
	hub.add(client)
	defer hub.remove(client)

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

var (
	eventsHub     *wsHub
	eventsHubOnce sync.Once
)

func getEventsHub() *wsHub {
	eventsHubOnce.Do(func() {
		eventsHub = newWSHub()
	})
	return eventsHub
}

var (
	updatesHub     *wsHub
	updatesHubOnce sync.Once
)

func getUpdatesHub() *wsHub {
	updatesHubOnce.Do(func() {
		updatesHub = newWSHub()
	})
	return updatesHub
}

// BroadcastEvent sends a deployment/service event to all connected clients.
func BroadcastEvent(eventType string, payload any) {
	hub := getEventsHub()
	data, _ := json.Marshal(map[string]any{
		"type":    eventType,
		"payload": payload,
		"ts":      time.Now().Unix(),
	})
	hub.broadcast(data)
}

// wsBuildLogs handles /ws/build/{deploymentId} — streams build logs.
func (s *Server) wsBuildLogs(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}

	deploymentID := chi.URLParam(r, "deploymentId")

	conn, upgradeErr := upgrader.Upgrade(w, r, nil)
	if upgradeErr != nil {
		s.log.Warnw("ws upgrade failed", "error", upgradeErr)
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	go func() {
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				cancel()
				return
			}
		}
	}()

	// Poll deployment logs from the database until the build completes
	lastLen := 0
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d, err := s.store.GetDeployment(ctx, deploymentID)
			if err != nil {
				return
			}
			if len(d.Logs) > lastLen {
				newLogs := d.Logs[lastLen:]
				lastLen = len(d.Logs)
				msg, _ := json.Marshal(map[string]any{
					"type": "build_log",
					"data": newLogs,
				})
				if writeErr := conn.WriteMessage(websocket.TextMessage, msg); writeErr != nil {
					return
				}
			}
			if d.Status == "success" || d.Status == "failed" {
				msg, _ := json.Marshal(map[string]any{
					"type":   "build_complete",
					"status": d.Status,
				})
				_ = conn.WriteMessage(websocket.TextMessage, msg)
				return
			}
		}
	}
}

// wsUpdates handles /ws/updates — streams update progress and status changes.
func (s *Server) wsUpdates(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Warnw("ws upgrade failed", "error", err)
		return
	}
	defer conn.Close()

	client := &wsClient{conn: conn}
	hub := getUpdatesHub()
	hub.add(client)
	defer hub.remove(client)

	for {
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
	}
}

// stripDockerLogHeader removes Docker's 8-byte multiplexed stream header from log lines.
func stripDockerLogHeader(data []byte) []byte {
	var result []string
	for len(data) > 0 {
		if len(data) >= 8 {
			// Docker multiplexed stream: [stream_type(1) 0 0 0 size(4)] payload
			size := int(data[4])<<24 | int(data[5])<<16 | int(data[6])<<8 | int(data[7])
			data = data[8:]
			if size > 0 && size <= len(data) {
				result = append(result, string(data[:size]))
				data = data[size:]
				continue
			}
		}
		// Fallback: treat remaining as raw text
		result = append(result, string(data))
		break
	}
	return []byte(strings.Join(result, ""))
}
