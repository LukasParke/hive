package cloudflare

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// success=false envelope variants per endpoint: every public method must
// surface the structured v4 errors instead of silently treating the call
// as successful.

func TestClientCreateTunnelSuccessFalse(t *testing.T) {
	f := &fakeCF{t: t}
	f.handler = func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelopeErr(t, w)
	}
	_, err := f.client().CreateTunnel(context.Background(), "acct", "web")
	if err == nil || !strings.Contains(err.Error(), "could not find record") {
		t.Fatalf("expected envelope error, got %v", err)
	}
	if !strings.Contains(err.Error(), "create tunnel") {
		t.Errorf("error %q missing method context", err)
	}
}

func TestClientDeleteTunnelSuccessFalse(t *testing.T) {
	f := &fakeCF{t: t}
	f.handler = func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelopeErr(t, w)
	}
	err := f.client().DeleteTunnel(context.Background(), "acct", "tid")
	if err == nil || !strings.Contains(err.Error(), "could not find record") {
		t.Fatalf("expected envelope error, got %v", err)
	}
	if !strings.Contains(err.Error(), "delete tunnel") {
		t.Errorf("error %q missing method context", err)
	}
}

func TestClientGetTunnelSuccessFalse(t *testing.T) {
	f := &fakeCF{t: t}
	f.handler = func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelopeErr(t, w)
	}
	_, err := f.client().GetTunnel(context.Background(), "acct", "tid")
	if err == nil || !strings.Contains(err.Error(), "could not find record") {
		t.Fatalf("expected envelope error, got %v", err)
	}
}

func TestClientTunnelTokenSuccessFalse(t *testing.T) {
	f := &fakeCF{t: t}
	calls := 0
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		calls++
		if strings.HasSuffix(r.URL.Path, "/token") {
			writeEnvelopeErr(t, w)
			return
		}
		writeEnvelope(t, w, http.StatusOK, map[string]any{"id": "tid", "token": "tok"})
	}
	_, err := f.client().CreateTunnel(context.Background(), "acct", "web")
	if err == nil || !strings.Contains(err.Error(), "fetch tunnel token") {
		t.Fatalf("expected token fetch failure, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected create+token calls, got %d", calls)
	}
}

func TestClientCreateDNSRouteSuccessFalse(t *testing.T) {
	f := &fakeCF{t: t}
	f.handler = func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelopeErr(t, w)
	}
	_, err := f.client().CreateDNSRoute(context.Background(), "zid", "app.example.com", "tid")
	if err == nil || !strings.Contains(err.Error(), "could not find record") {
		t.Fatalf("expected envelope error, got %v", err)
	}
}

func TestClientSuccessFalseWithoutErrorsFallsBackToSnippet(t *testing.T) {
	f := &fakeCF{t: t}
	f.handler = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":false,"errors":[],"result":null}`))
	}
	err := f.client().DeleteDNSRecord(context.Background(), "zid", "rec")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), `"success":false`) {
		t.Errorf("expected raw body fallback in %q", err)
	}
}

// Non-JSON bodies and transport failures.

func TestClientNonJSONSuccessBodyFails(t *testing.T) {
	f := &fakeCF{t: t}
	f.handler = func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>gateway error</html>"))
	}
	_, err := f.client().GetTunnel(context.Background(), "acct", "tid")
	if err == nil || !strings.Contains(err.Error(), "decode cloudflare response") {
		t.Fatalf("expected decode failure, got %v", err)
	}
}

func TestClientEmptyErrorBodySnippet(t *testing.T) {
	f := &fakeCF{t: t}
	f.handler = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}
	_, err := f.client().GetTunnel(context.Background(), "acct", "tid")
	if err == nil || !strings.Contains(err.Error(), "(empty body)") {
		t.Fatalf("expected empty-body snippet, got %v", err)
	}
}

func TestClientLongErrorBodyTruncatedTo512Bytes(t *testing.T) {
	f := &fakeCF{t: t}
	long := strings.Repeat("x", 2000)
	f.handler = func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(long))
	}
	_, err := f.client().GetTunnel(context.Background(), "acct", "tid")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "…") {
		t.Errorf("expected truncation marker in %q", msg)
	}
	if !strings.Contains(msg, strings.Repeat("x", maxErrorSnippet)) {
		t.Errorf("expected %d-byte snippet in error", maxErrorSnippet)
	}
	if strings.Contains(msg, strings.Repeat("x", maxErrorSnippet+100)) {
		t.Errorf("error body not truncated: %d x's", strings.Count(msg, "x"))
	}
}

func TestClientContextTimeout(t *testing.T) {
	f := &fakeCF{t: t}
	release := make(chan struct{})
	f.handler = func(w http.ResponseWriter, _ *http.Request) {
		<-release
	}
	srv := httptest.NewServer(http.HandlerFunc(f.handler))
	defer func() {
		close(release)
		srv.Close()
	}()
	c := NewClient("test-token")
	c.baseURL = srv.URL + "/client/v4"
	c.http = &http.Client{Timeout: 50 * time.Millisecond}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.GetTunnel(ctx, "acct", "tid")
	if err == nil || !strings.Contains(err.Error(), "cloudflare request") {
		t.Fatalf("expected request timeout error, got %v", err)
	}
}

// Result decoding failures.

func TestClientCreateTunnelInvalidResult(t *testing.T) {
	f := &fakeCF{t: t}
	f.handler = func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelopeRaw(t, w, `"just-a-string"`)
	}
	_, err := f.client().CreateTunnel(context.Background(), "acct", "web")
	if err == nil || !strings.Contains(err.Error(), "decode create tunnel result") {
		t.Fatalf("expected result decode failure, got %v", err)
	}
}

func TestClientGetTunnelInvalidResult(t *testing.T) {
	f := &fakeCF{t: t}
	f.handler = func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelopeRaw(t, w, `{"id":123}`)
	}
	_, err := f.client().GetTunnel(context.Background(), "acct", "tid")
	// {"id":123} unmarshals into tunnelResult fine; force a type error.
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestClientGetTunnelNonObjectResult(t *testing.T) {
	f := &fakeCF{t: t}
	f.handler = func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelopeRaw(t, w, `[1,2,3]`)
	}
	_, err := f.client().GetTunnel(context.Background(), "acct", "tid")
	if err == nil || !strings.Contains(err.Error(), "decode tunnel status") {
		t.Fatalf("expected tunnel status decode failure, got %v", err)
	}
}

func TestClientTunnelTokenNonStringResult(t *testing.T) {
	f := &fakeCF{t: t}
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/token") {
			writeEnvelopeRaw(t, w, `{"not":"a-string"}`)
			return
		}
		writeEnvelope(t, w, http.StatusOK, map[string]any{"id": "tid"})
	}
	_, err := f.client().CreateTunnel(context.Background(), "acct", "web")
	if err == nil || !strings.Contains(err.Error(), "decode tunnel token") {
		t.Fatalf("expected token decode failure, got %v", err)
	}
}

func TestClientInvalidAccountIDFailsRequestBuild(t *testing.T) {
	f := &fakeCF{t: t}
	f.handler = func(w http.ResponseWriter, _ *http.Request) {}
	// A control character in the path makes http.NewRequestWithContext fail
	// before any bytes hit the wire.
	_, err := f.client().GetTunnel(context.Background(), "bad\x7faccount", "tid")
	if err == nil || !strings.Contains(err.Error(), "build cloudflare request") {
		t.Fatalf("expected request build failure, got %v", err)
	}
}

func TestClientCreateDNSRouteInvalidResult(t *testing.T) {
	f := &fakeCF{t: t}
	f.handler = func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelopeRaw(t, w, `"nope"`)
	}
	_, err := f.client().CreateDNSRoute(context.Background(), "zid", "app.example.com", "tid")
	if err == nil || !strings.Contains(err.Error(), "decode dns record result") {
		t.Fatalf("expected dns record decode failure, got %v", err)
	}
}

func writeEnvelopeRaw(t *testing.T, w http.ResponseWriter, rawResult string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"success":true,"errors":[],"result":` + rawResult + `}`))
}
