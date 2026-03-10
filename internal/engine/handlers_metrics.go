package engine

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/lholliger/hive/internal/store"
)

func (s *Server) apiGetLatestMetricsSnapshots(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	snaps, err := s.store.GetLatestMetricsSnapshots(r.Context())
	if handleErr(w, err) {
		return
	}
	if snaps == nil {
		snaps = []store.NodeMetricsSnapshot{}
	}
	writeJSON(w, http.StatusOK, snaps)
}

func (s *Server) apiGetNodeMetricsHistory(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	nodeID := chi.URLParam(r, "nodeId")
	if nodeID == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "node_id is required", nil)
		return
	}
	since := time.Now().Add(-24 * time.Hour)
	if v := r.URL.Query().Get("since"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			since = t
		}
	}
	snaps, err := s.store.GetNodeMetricsHistory(r.Context(), nodeID, since)
	if handleErr(w, err) {
		return
	}
	if snaps == nil {
		snaps = []store.NodeMetricsSnapshot{}
	}
	writeJSON(w, http.StatusOK, snaps)
}

// apiProxyPrometheus forwards query params to Prometheus. Set HIVE_PROMETHEUS_URL.
func (s *Server) apiProxyPrometheus(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	promURL := r.URL.Query().Get("prometheus_url")
	if promURL == "" {
		promURL = os.Getenv("HIVE_PROMETHEUS_URL")
	}
	if promURL == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "prometheus_url not configured", nil)
		return
	}
	query := r.URL.Query().Get("query")
	if query == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "query is required", nil)
		return
	}
	target, _ := url.Parse(promURL)
	target.Path = strings.TrimSuffix(target.Path, "/") + "/api/v1/query"
	target.RawQuery = url.Values{"query": {query}}.Encode()
	req, err := http.NewRequestWithContext(r.Context(), "GET", target.String(), nil)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "bad_gateway", err.Error(), nil)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
