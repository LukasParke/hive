package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// recordingServer captures requests and replies with the given statuses in
// order; the last status repeats once exhausted.
func recordingServer(t *testing.T, statuses []int, seen *[]map[string]any) *httptest.Server {
	t.Helper()
	var calls atomic.Int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		idx := int(calls.Add(1)) - 1
		if idx >= len(statuses) {
			idx = len(statuses) - 1
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if seen != nil {
			*seen = append(*seen, body)
		}
		w.WriteHeader(statuses[idx])
	}))
}

func TestSendWebhookStyleChannels(t *testing.T) {
	tests := []struct {
		name    string
		channel string
	}{
		{name: "slack", channel: "slack"},
		{name: "discord case-insensitive", channel: "DISCORD"},
		{name: "webhook", channel: "webhook"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var bodies []map[string]any
			srv := recordingServer(t, []int{http.StatusOK}, &bodies)
			defer srv.Close()

			d := &Dispatcher{http: srv.Client()}
			d.send(context.Background(), tt.channel, srv.URL, "build.succeeded", map[string]any{"app": "api"})

			if len(bodies) != 1 {
				t.Fatalf("expected exactly one request, got %d", len(bodies))
			}
			want := map[string]any{
				"event":   "build.succeeded",
				"channel": tt.channel,
				"payload": map[string]any{"app": "api"},
			}
			if got := bodies[0]["channel"]; got != want["channel"] {
				t.Fatalf("channel = %v, want %v", got, want["channel"])
			}
			if bodies[0]["event"] != "build.succeeded" {
				t.Fatalf("event = %v, want build.succeeded", bodies[0]["event"])
			}
			payload, _ := bodies[0]["payload"].(map[string]any)
			if payload["app"] != "api" {
				t.Fatalf("payload = %v, want app=api", bodies[0]["payload"])
			}
		})
	}
}

func TestSendRetriesOn500ThenSucceeds(t *testing.T) {
	var bodies []map[string]any
	srv := recordingServer(t, []int{http.StatusInternalServerError, http.StatusOK}, &bodies)
	defer srv.Close()

	d := &Dispatcher{http: srv.Client()}
	d.send(context.Background(), "slack", srv.URL, "deploy.failed", nil)

	if len(bodies) != 2 {
		t.Fatalf("expected one retry after 500, got %d requests", len(bodies))
	}
}

func TestSendGivesUpAfterTwoFailures(t *testing.T) {
	var bodies []map[string]any
	srv := recordingServer(t, []int{http.StatusInternalServerError}, &bodies)
	defer srv.Close()

	d := &Dispatcher{http: srv.Client()}
	d.send(context.Background(), "slack", srv.URL, "deploy.failed", nil)

	if len(bodies) != 2 {
		t.Fatalf("expected exactly 2 attempts, got %d", len(bodies))
	}
}

func TestSendInvalidTargetURLReturns(t *testing.T) {
	d := &Dispatcher{http: http.DefaultClient}
	d.send(context.Background(), "slack", "http://exa mple.invalid", "event", nil)
}

func TestSendDoesNotRetryClientErrors(t *testing.T) {
	var bodies []map[string]any
	srv := recordingServer(t, []int{http.StatusBadRequest}, &bodies)
	defer srv.Close()

	d := &Dispatcher{http: srv.Client()}
	d.send(context.Background(), "slack", srv.URL, "deploy.failed", nil)

	if len(bodies) != 1 {
		t.Fatalf("4xx must not be retried, got %d requests", len(bodies))
	}
}

func TestSendEmptyTargetSkips(t *testing.T) {
	d := &Dispatcher{http: http.DefaultClient}
	d.send(context.Background(), "slack", "", "event", nil) // must not panic or dial
}

func TestSendUnknownChannelDoesNothing(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := &Dispatcher{http: srv.Client()}
	d.send(context.Background(), "sms", srv.URL, "event", nil)
	if called {
		t.Fatal("unknown channel must not trigger an HTTP call")
	}
}

func TestSendUnreachableTargetReturns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // closed so the dial fails immediately

	d := &Dispatcher{http: srv.Client()}
	d.send(context.Background(), "slack", srv.URL, "event", nil) // must exhaust retries and return
}

// failingPool returns a pool whose queries fail without needing a database.
func failingPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	p, err := pgxpool.New(context.Background(), "postgresql://nouser@127.0.0.1:1/nodb")
	if err != nil {
		t.Fatalf("pool config: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

func TestNotifyQueriesFailureIsSilent(t *testing.T) {
	d := NewDispatcher(failingPool(t))
	d.Notify(context.Background(), "event", map[string]any{}) // must return, not panic
}

func TestNotifyDispatchesOnlyEnabledWebhookTargets(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	seed := func(channel string, enabled bool) {
		t.Helper()
		if _, err := pool.Exec(context.Background(),
			`insert into notifications(channel, target, enabled) values ($1, $2, $3)`,
			channel, srv.URL, enabled); err != nil {
			t.Fatalf("seed notification: %v", err)
		}
	}
	seed("slack", true)
	seed("discord", false)
	seed("email", true) // unsupported channel: never dialed

	d := NewDispatcher(pool)
	d.Notify(context.Background(), "build.completed", map[string]any{"id": "1"})

	if got := hits.Load(); got != 1 {
		t.Fatalf("expected exactly 1 dispatch to enabled slack target, got %d", got)
	}
}

func TestNotifySkipsEmptyTargets(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)

	if _, err := pool.Exec(context.Background(),
		`insert into notifications(channel, target, enabled) values ('slack', '', true)`); err != nil {
		t.Fatalf("seed notification: %v", err)
	}

	d := NewDispatcher(pool)
	d.Notify(context.Background(), "event", nil) // empty target: send returns before dialing
}
