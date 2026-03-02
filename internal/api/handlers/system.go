package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/nats-io/nats.go"

	"github.com/lholliger/hive/internal/swarm"
	"github.com/lholliger/hive/pkg/config"
)

type SystemStatusResponse struct {
	Status    string `json:"status"`
	Role      string `json:"role"`
	NodeCount int    `json:"node_count"`
	MultiNode bool   `json:"multi_node"`
	NATS      string `json:"nats"`
}

func SystemStatus(nc *nats.Conn, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sc, err := swarm.NewClient(nil)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "docker unavailable"})
			return
		}
		defer sc.Close()

		nodeCount, _ := sc.NodeCount(r.Context())

		resp := SystemStatusResponse{
			Status:    "healthy",
			Role:      string(cfg.Role),
			NodeCount: nodeCount,
			MultiNode: cfg.MultiNode,
			NATS:      nc.Status().String(),
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
