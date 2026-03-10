package engine

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/lholliger/hive/internal/bespoke"
)

type bespokeResponse struct {
	bespoke.AppClass
	TemplateAvailable bool `json:"template_available"`
}

func (s *Server) apiListBespokeApps(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}

	cat := s.loadCatalog()
	items := bespoke.List()
	resp := make([]bespokeResponse, 0, len(items))
	for _, item := range items {
		_, catErr := cat.Get(item.TemplateName)
		resp = append(resp, bespokeResponse{
			AppClass:          item,
			TemplateAvailable: catErr == nil,
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) apiGetBespokeApp(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}

	slug := chi.URLParam(r, "slug")
	item, ok := bespoke.Get(slug)
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found", "bespoke app not found", nil)
		return
	}

	cat := s.loadCatalog()
	_, catErr := cat.Get(item.TemplateName)
	writeJSON(w, http.StatusOK, bespokeResponse{
		AppClass:          item,
		TemplateAvailable: catErr == nil,
	})
}
