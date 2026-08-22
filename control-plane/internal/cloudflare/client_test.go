package cloudflare

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeCF spins up an httptest server that records requests and replays
// canned responses; it returns the client pointed at the server.
type fakeCF struct {
	t *testing.T

	handler func(w http.ResponseWriter, r *http.Request)

	lastMethod string
	lastPath   string
	lastAuth   string
	lastBody   map[string]any
}

func (f *fakeCF) client() *Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := map[string]any{}
		if r.Body != nil {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		f.lastMethod = r.Method
		f.lastPath = r.URL.Path
		f.lastAuth = r.Header.Get("Authorization")
		f.lastBody = body
		f.handler(w, r)
	}))
	f.t.Cleanup(srv.Close)
	c := NewClient("test-token")
	c.baseURL = srv.URL + "/client/v4"
	return c
}

func writeEnvelope(t *testing.T, w http.ResponseWriter, status int, result any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"success": status >= 200 && status < 300,
		"errors":  []cfError{},
		"result":  result,
	}); err != nil {
		t.Fatalf("encode envelope: %v", err)
	}
}

func TestClientCreateTunnel(t *testing.T) {
	f := &fakeCF{t: t}
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/cfd_tunnel"):
			if got := f.lastBody["name"]; got != "prod-edge" {
				t.Errorf("request name = %v, want prod-edge", got)
			}
			writeEnvelope(t, w, http.StatusOK, map[string]string{"id": "tunnel-uuid", "token": "conn-token"})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/cfd_tunnel/tunnel-uuid/token"):
			writeEnvelope(t, w, http.StatusOK, `{"a":"acc","t":"tunnel-uuid","s":"secret"}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			writeEnvelope(t, w, http.StatusNotFound, nil)
		}
	}
	ref, err := f.client().CreateTunnel(context.Background(), "acct", "prod-edge")
	if err != nil {
		t.Fatalf("CreateTunnel: %v", err)
	}
	if ref.ID != "tunnel-uuid" || ref.Token != "conn-token" {
		t.Errorf("ref = %+v, want id tunnel-uuid token conn-token", ref)
	}
	var creds map[string]any
	if err := json.Unmarshal(ref.CredentialsJSON, &creds); err != nil {
		t.Fatalf("credentials JSON invalid: %v", err)
	}
	if creds["a"] != "acc" || creds["s"] != "secret" {
		t.Errorf("credentials = %v", creds)
	}
	if f.lastAuth != "Bearer test-token" {
		t.Errorf("auth header = %q", f.lastAuth)
	}
}

func TestClientDeleteTunnel(t *testing.T) {
	f := &fakeCF{t: t}
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/client/v4/accounts/acct/cfd_tunnel/tid" {
			writeEnvelope(t, w, http.StatusOK, map[string]string{"id": "tid"})
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		writeEnvelope(t, w, http.StatusNotFound, nil)
	}
	if err := f.client().DeleteTunnel(context.Background(), "acct", "tid"); err != nil {
		t.Fatalf("DeleteTunnel: %v", err)
	}
}

func TestClientGetTunnel(t *testing.T) {
	f := &fakeCF{t: t}
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/client/v4/accounts/acct/cfd_tunnel/tid" {
			writeEnvelope(t, w, http.StatusOK, map[string]string{"id": "tid", "status": "healthy"})
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		writeEnvelope(t, w, http.StatusNotFound, nil)
	}
	status, err := f.client().GetTunnel(context.Background(), "acct", "tid")
	if err != nil {
		t.Fatalf("GetTunnel: %v", err)
	}
	if status != "healthy" {
		t.Errorf("status = %q, want healthy", status)
	}
}

func TestClientCreateDNSRoute(t *testing.T) {
	f := &fakeCF{t: t}
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/client/v4/zones/zid/dns_records" {
			if f.lastBody["type"] != "CNAME" || f.lastBody["name"] != "*.example.com" ||
				f.lastBody["content"] != "tid.cfargotunnel.com" || f.lastBody["proxied"] != true {
				t.Errorf("dns body = %v", f.lastBody)
			}
			writeEnvelope(t, w, http.StatusOK, map[string]string{"id": "rec-1"})
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		writeEnvelope(t, w, http.StatusNotFound, nil)
	}
	recID, err := f.client().CreateDNSRoute(context.Background(), "zid", "*.example.com", "tid")
	if err != nil {
		t.Fatalf("CreateDNSRoute: %v", err)
	}
	if recID != "rec-1" {
		t.Errorf("recordID = %q, want rec-1", recID)
	}
}

func TestClientDeleteDNSRecord(t *testing.T) {
	f := &fakeCF{t: t}
	f.handler = func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && r.URL.Path == "/client/v4/zones/zid/dns_records/rec-9" {
			writeEnvelope(t, w, http.StatusOK, nil)
			return
		}
		t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		writeEnvelope(t, w, http.StatusNotFound, nil)
	}
	if err := f.client().DeleteDNSRecord(context.Background(), "zid", "rec-9"); err != nil {
		t.Fatalf("DeleteDNSRecord: %v", err)
	}
}

func TestClientErrorBodyPropagation(t *testing.T) {
	f := &fakeCF{t: t}
	f.handler = func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"success":false,"errors":[{"code":9109,"message":"Unauthorized to access requested resource"}],"result":null}`))
	}
	_, err := f.client().GetTunnel(context.Background(), "acct", "tid")
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"403", "9109", "Unauthorized to access requested resource"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestClientSuccessFalseWithErrors(t *testing.T) {
	f := &fakeCF{t: t}
	f.handler = func(w http.ResponseWriter, _ *http.Request) {
		writeEnvelopeErr(t, w)
	}
	err := f.client().DeleteDNSRecord(context.Background(), "zid", "rec")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "could not find record") {
		t.Errorf("error %q missing cloudflare message", err)
	}
}

func writeEnvelopeErr(t *testing.T, w http.ResponseWriter) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success": false,
		"errors":  []map[string]any{{"code": 81053, "message": "could not find record"}},
		"result":  nil,
	})
}
