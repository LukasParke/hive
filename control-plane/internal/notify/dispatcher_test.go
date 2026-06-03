package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSendWebhookStyleChannels(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := &Dispatcher{http: srv.Client()}
	d.send(context.Background(), "slack", srv.URL, "build.succeeded", map[string]any{"ok": true})
	if !called {
		t.Fatalf("expected webhook dispatcher to call target URL")
	}
}
