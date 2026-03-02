package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/nats-io/nats.go"

	"github.com/lholliger/hive/internal/api/middleware"
	"github.com/lholliger/hive/internal/catalog"
	"github.com/lholliger/hive/internal/store"
)

var appCatalog *catalog.Catalog

func init() {
	var err error
	appCatalog, err = catalog.LoadFromDir("/app/templates")
	if err != nil {
		appCatalog, err = catalog.LoadFromDir("templates")
		if err != nil {
			appCatalog, _ = catalog.LoadFromDir("")
		}
	}
}

func ListCatalog(w http.ResponseWriter, r *http.Request) {
	templates := appCatalog.List()
	if templates == nil {
		templates = []catalog.Template{}
	}
	writeJSON(w, http.StatusOK, templates)
}

func DeployCatalogApp(nc *nats.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		templateName := chi.URLParam(r, "templateId")

		tmpl, err := appCatalog.Get(templateName)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}

		var body struct {
			ProjectID string            `json:"project_id"`
			Domain    string            `json:"domain"`
			Env       map[string]string `json:"env"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		if body.ProjectID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project_id required"})
			return
		}

		env := make(map[string]string)
		for k, v := range tmpl.Env {
			env[k] = v
		}
		for k, v := range body.Env {
			env[k] = v
		}
		domain := body.Domain
		if domain == "" {
			domain = tmpl.Domain
		}

		db := middleware.GetStore(r.Context())
		if db == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store unavailable"})
			return
		}

		port := 0
		if len(tmpl.Ports) > 0 {
			for i := 0; i < len(tmpl.Ports[0]); i++ {
				c := tmpl.Ports[0][i]
				if c >= '0' && c <= '9' {
					port = port*10 + int(c-'0')
				} else {
					break
				}
			}
		}
		if port == 0 {
			port = 3000
		}

		envJSON, _ := json.Marshal(env)
		app := &store.App{
			ProjectID:       body.ProjectID,
			Name:            tmpl.Name,
			DeployType:      "image",
			Image:           tmpl.Image,
			Domain:          domain,
			Port:            port,
			Replicas:        tmpl.Replicas,
			EnvEncrypted:    envJSON,
			Status:          "deploying",
			TemplateName:    tmpl.Name,
			TemplateVersion: "",
		}
		if app.Replicas == 0 {
			app.Replicas = 1
		}

		if err := db.CreateApp(r.Context(), app); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create app: " + err.Error()})
			return
		}

		dep := &store.Deployment{AppID: app.ID, Status: "deploying"}
		_ = db.CreateDeployment(r.Context(), dep)

		job, _ := json.Marshal(map[string]string{
			"action":        "deploy",
			"name":          tmpl.Name,
			"image":         tmpl.Image,
			"domain":        domain,
			"app_id":        app.ID,
			"deployment_id": dep.ID,
			"env":           string(envJSON),
		})
		if err := nc.Publish("hive.deploy", job); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to publish deploy job"})
			return
		}

		writeJSON(w, http.StatusAccepted, app)
	}
}
