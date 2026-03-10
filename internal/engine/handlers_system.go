package engine

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/lholliger/hive/internal/store"
)

func (s *Server) apiSystemLogs(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}

	since := time.Now().Add(-24 * time.Hour)
	until := time.Now()
	search := r.URL.Query().Get("search")
	level := r.URL.Query().Get("level")
	limit := 500

	if q := r.URL.Query().Get("since"); q != "" {
		if t, err := time.Parse(time.RFC3339, q); err == nil {
			since = t
		}
	}
	if q := r.URL.Query().Get("until"); q != "" {
		if t, err := time.Parse(time.RFC3339, q); err == nil {
			until = t
		}
	}
	if q := r.URL.Query().Get("limit"); q != "" {
		_, _ = fmt.Sscanf(q, "%d", &limit)
	}

	entries, err := s.store.QuerySystemLogEntries(r.Context(), since, until, search, level, limit)
	if handleErr(w, err) {
		return
	}
	if entries == nil {
		entries = []store.LogEntry{}
	}
	writeJSON(w, http.StatusOK, entries)
}

func (s *Server) apiConnectivityCheck(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}

	type portResult struct {
		Port   int    `json:"port"`
		Open   bool   `json:"open"`
		Detail string `json:"detail,omitempty"`
	}

	checkPort := func(port int) portResult {
		addr := fmt.Sprintf(":%d", port)
		ln, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err != nil {
			return portResult{Port: port, Open: false, Detail: err.Error()}
		}
		_ = ln.Close()
		return portResult{Port: port, Open: true}
	}

	results := []portResult{
		checkPort(80),
		checkPort(443),
	}

	writeJSON(w, http.StatusOK, map[string]any{"results": results})
}
