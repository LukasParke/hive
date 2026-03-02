package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/nats-io/nats.go"

	"github.com/lholliger/hive/internal/store"
)

func CreateStack(nc *nats.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectId")

		var body struct {
			Name           string `json:"name"`
			ComposeContent string `json:"compose_content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		if body.Name == "" || body.ComposeContent == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and compose_content required"})
			return
		}

		s := storeFromRequest(r)
		st := &store.Stack{
			ProjectID:      projectID,
			Name:           body.Name,
			ComposeContent: body.ComposeContent,
			Status:         "pending",
		}
		if err := s.CreateStack(r.Context(), st); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		job, _ := json.Marshal(map[string]string{
			"action":   "stack_deploy",
			"stack_id": st.ID,
			"name":     st.Name,
		})
		nc.Publish("hive.deploy", job)

		writeJSON(w, http.StatusCreated, st)
	}
}

func ListStacks(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	s := storeFromRequest(r)
	stacks, err := s.ListStacks(r.Context(), projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if stacks == nil {
		stacks = []store.Stack{}
	}
	writeJSON(w, http.StatusOK, stacks)
}

func GetStack(w http.ResponseWriter, r *http.Request) {
	stackID := chi.URLParam(r, "stackId")
	s := storeFromRequest(r)
	st, err := s.GetStack(r.Context(), stackID)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "stack not found"})
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func UpdateStack(nc *nats.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stackID := chi.URLParam(r, "stackId")
		s := storeFromRequest(r)

		st, err := s.GetStack(r.Context(), stackID)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "stack not found"})
			return
		}

		var body struct {
			ComposeContent string `json:"compose_content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}

		if body.ComposeContent != "" {
			st.ComposeContent = body.ComposeContent
		}
		st.Status = "updating"

		if err := s.UpdateStack(r.Context(), st); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		job, _ := json.Marshal(map[string]string{
			"action":   "stack_deploy",
			"stack_id": st.ID,
			"name":     st.Name,
		})
		nc.Publish("hive.deploy", job)

		writeJSON(w, http.StatusOK, st)
	}
}

func DeleteStack(nc *nats.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stackID := chi.URLParam(r, "stackId")
		s := storeFromRequest(r)

		st, err := s.GetStack(r.Context(), stackID)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "stack not found"})
			return
		}

		job, _ := json.Marshal(map[string]string{
			"action":   "stack_remove",
			"stack_id": st.ID,
			"name":     st.Name,
		})
		nc.Publish("hive.deploy", job)

		if err := s.DeleteStack(r.Context(), stackID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"deleted": stackID})
	}
}
