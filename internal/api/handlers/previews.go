package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/nats-io/nats.go"

	"github.com/lholliger/hive/internal/preview"
	"github.com/lholliger/hive/internal/store"
)

func ListPreviews(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	s := storeFromRequest(r)
	previews, err := s.ListPreviewDeployments(r.Context(), appID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if previews == nil {
		previews = []store.PreviewDeployment{}
	}
	writeJSON(w, http.StatusOK, previews)
}

func DeletePreview(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	previewID := chi.URLParam(r, "previewId")
	s := storeFromRequest(r)

	pd, err := s.GetPreviewDeployment(r.Context(), previewID)
	if err != nil || pd == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "preview not found"})
		return
	}
	if pd.AppID != appID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "preview not found"})
		return
	}

	mgr := preview.New(s, nil, nil)
	if err := mgr.Destroy(r.Context(), previewID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": previewID})
}

func CreatePreview(nc *nats.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		appID := chi.URLParam(r, "appId")
		s := storeFromRequest(r)

		var body struct {
			Branch   string `json:"branch"`
			PRNumber int    `json:"pr_number"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		if body.Branch == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "branch required"})
			return
		}

		app, err := s.GetApp(r.Context(), appID)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
			return
		}

		mgr := preview.New(s, nc, nil)
		pd, err := mgr.Create(r.Context(), app, body.Branch, body.PRNumber)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, pd)
	}
}
