package realtime

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/luke/hive/control-plane/internal/db"
)

// waitFor polls cond every 10ms until it returns true or the timeout elapses.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// wsServer starts an httptest server whose /ws endpoint is handled by the hub
// and returns its websocket URL.
func wsServer(t *testing.T, h *Hub) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", h.HandleWS)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
}

func dial(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", url, err)
	}
	return c
}

// clientCount returns the number of currently registered hub clients.
func clientCount(h *Hub) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.clients)
}

func rstClose(t *testing.T, c *websocket.Conn) {
	t.Helper()
	// Abruptly reset the TCP connection so the next server-side write fails.
	if tcp, ok := c.UnderlyingConn().(*net.TCPConn); ok {
		_ = tcp.SetLinger(0)
	}
	_ = c.UnderlyingConn().Close()
}

func TestNewHubIsEmpty(t *testing.T) {
	h := NewHub()
	if h.clients == nil {
		t.Fatal("NewHub must initialize the client map")
	}
	if n := clientCount(h); n != 0 {
		t.Fatalf("fresh hub clients = %d, want 0", n)
	}
}

func TestHandleWSUpgradeFailureRegistersNothing(t *testing.T) {
	h := NewHub()
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", h.HandleWS)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// A plain GET without upgrade headers must be rejected by the upgrader
	// and must not leave a registered client behind.
	res, err := http.Get(ts.URL + "/ws")
	if err != nil {
		t.Fatalf("plain GET: %v", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("plain GET status = %d, want 400", res.StatusCode)
	}
	waitFor(t, time.Second, "hub to stay empty", func() bool { return clientCount(h) == 0 })
}

func TestBroadcastFanoutReachesEveryClient(t *testing.T) {
	h := NewHub()
	url := wsServer(t, h)

	a := dial(t, url)
	defer func() { _ = a.Close() }()
	b := dial(t, url)
	defer func() { _ = b.Close() }()

	waitFor(t, 2*time.Second, "both clients to register", func() bool { return clientCount(h) == 2 })

	sent := db.Notification{Channel: "deploys", Payload: `{"app":"web","status":"complete"}`}
	h.Broadcast(sent)

	got := make([]db.Notification, 2)
	for i, c := range []*websocket.Conn{a, b} {
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		if err := c.ReadJSON(&got[i]); err != nil {
			t.Fatalf("client %d read: %v", i, err)
		}
		if got[i] != sent {
			t.Fatalf("client %d got %+v, want %+v", i, got[i], sent)
		}
	}

	// Marshaling shape: notifications travel as {"channel":..., "payload":...}.
	raw, err := json.Marshal(got[0])
	if err != nil {
		t.Fatalf("marshal notification: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("unmarshal notification: %v", err)
	}
	if _, ok := wire["channel"]; !ok {
		t.Fatalf("notification JSON missing channel key: %s", raw)
	}
	if _, ok := wire["payload"]; !ok {
		t.Fatalf("notification JSON missing payload key: %s", raw)
	}
}

func TestBroadcastDropsDeadSubscriber(t *testing.T) {
	h := NewHub()
	url := wsServer(t, h)

	live := dial(t, url)
	defer func() { _ = live.Close() }()
	dead := dial(t, url)
	defer func() { _ = dead.Close() }()

	waitFor(t, 2*time.Second, "both clients to register", func() bool { return clientCount(h) == 2 })

	// Shut down only the server-side write half of the dead subscriber's
	// connection: the hub's next WriteJSON fails immediately while the
	// connection's read loop stays alive, so eviction can only come from the
	// broadcast error path.
	// Identify the dead subscriber's server-side connection deterministically:
	// the server sees the client's local TCP address as its remote address.
	h.mu.Lock()
	var deadServerConn *websocket.Conn
	for c := range h.clients {
		if c.RemoteAddr().String() == dead.LocalAddr().String() {
			deadServerConn = c
		}
	}
	h.mu.Unlock()
	if deadServerConn == nil {
		t.Fatal("dead subscriber not found among hub clients")
	}
	if tcp, ok := deadServerConn.UnderlyingConn().(*net.TCPConn); ok {
		if err := tcp.CloseWrite(); err != nil {
			t.Fatalf("close write half: %v", err)
		}
	} else {
		t.Fatalf("underlying conn is %T, want *net.TCPConn", deadServerConn.UnderlyingConn())
	}

	h.Broadcast(db.Notification{Channel: "ops", Payload: "{}"})
	waitFor(t, 2*time.Second, "dead subscriber eviction", func() bool { return clientCount(h) == 1 })

	// The surviving client still receives later broadcasts. Frames queued
	// before the eviction may arrive first, so drain until the new one.
	msg := db.Notification{Channel: "ops", Payload: `{"n":2}`}
	h.Broadcast(msg)
	_ = live.SetReadDeadline(time.Now().Add(2 * time.Second))
	var got db.Notification
	for {
		if err := live.ReadJSON(&got); err != nil {
			t.Fatalf("live client read after drop: %v", err)
		}
		if got == msg {
			break
		}
	}
}

func TestReadLoopUnregistersClosingClient(t *testing.T) {
	h := NewHub()
	url := wsServer(t, h)

	c := dial(t, url)
	waitFor(t, 2*time.Second, "client to register", func() bool { return clientCount(h) == 1 })

	// Inbound messages are drained by the read loop...
	_ = c.SetWriteDeadline(time.Now().Add(time.Second))
	if err := c.WriteMessage(websocket.TextMessage, []byte("hello")); err != nil {
		t.Fatalf("client write: %v", err)
	}
	_ = c.Close()
	waitFor(t, 2*time.Second, "closing client unregister", func() bool { return clientCount(h) == 0 })
}

// TestPingTickerEvictsStaleConnection exercises the 20s keepalive loop: the
// first tick pings the live client (pong handler refreshes the deadline), then
// the abruptly closed client's ping write fails and the ticker goroutine
// evicts it. This test intentionally waits across two keepalive ticks.
func TestPingTickerEvictsStaleConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("keepalive timing test")
	}
	h := NewHub()
	url := wsServer(t, h)

	c := dial(t, url)
	defer func() { _ = c.Close() }()
	// The gorilla client only answers pings while a read loop is running.
	go func() {
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}()
	waitFor(t, 2*time.Second, "client to register", func() bool { return clientCount(h) == 1 })
	if n := clientCount(h); n != 1 {
		t.Fatalf("healthy client was evicted, clients = %d", n)
	}

	// Survive the first ping/pong cycle (~20s) so the server's pong handler
	// runs and the read deadline is refreshed.
	time.Sleep(22 * time.Second)
	rstClose(t, c)

	// Next tick's WriteControl fails against the reset socket → eviction.
	waitFor(t, 25*time.Second, "stale client eviction by ping ticker", func() bool {
		return clientCount(h) == 0
	})
}
