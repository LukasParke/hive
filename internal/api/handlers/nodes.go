package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/lholliger/hive/internal/swarm"
)

func ListNodes(w http.ResponseWriter, r *http.Request) {
	sc, err := swarm.NewClient(nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "docker unavailable"})
		return
	}
	defer sc.Close()

	nodes, err := sc.ListNodes(r.Context())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	tokens := struct {
		Worker  string `json:"worker"`
		Manager string `json:"manager"`
	}{}
	wt, mt, err := sc.GetSwarmJoinTokens(r.Context())
	if err == nil {
		tokens.Worker = wt
		tokens.Manager = mt
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"nodes":       nodes,
		"join_tokens": tokens,
	})
}

func GetNode(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "nodeId")
	sc, err := swarm.NewClient(nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "docker unavailable"})
		return
	}
	defer sc.Close()

	node, err := sc.GetNode(r.Context(), nodeID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "node not found"})
		return
	}
	writeJSON(w, http.StatusOK, node)
}

func NodeStats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func UpdateNodeLabels(w http.ResponseWriter, r *http.Request) {
	nodeID := chi.URLParam(r, "nodeId")

	var body struct {
		Labels map[string]string `json:"labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	sc, err := swarm.NewClient(nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "docker unavailable"})
		return
	}
	defer sc.Close()

	if err := sc.UpdateNodeLabels(r.Context(), nodeID, body.Labels); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"updated": nodeID})
}
