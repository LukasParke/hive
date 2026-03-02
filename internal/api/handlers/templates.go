package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/nats-io/nats.go"
	"gopkg.in/yaml.v3"

	"github.com/lholliger/hive/internal/api/middleware"
	"github.com/lholliger/hive/internal/catalog"
	"github.com/lholliger/hive/internal/store"
)

// TemplateListItem is a unified view of built-in or custom template for listing.
type TemplateListItem struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Category    string            `json:"category"`
	Icon        string            `json:"icon"`
	Image       string            `json:"image"`
	Version     string            `json:"version"`
	Ports       []string          `json:"ports"`
	Env         map[string]string `json:"env"`
	Volumes     []string          `json:"volumes"`
	Domain      string            `json:"domain"`
	Replicas    int               `json:"replicas"`
	IsStack     bool              `json:"is_stack"`
	Source      string            `json:"source"` // "builtin" or "custom"
}

// TemplateDetail extends TemplateListItem with compose_content for stacks.
type TemplateDetail struct {
	TemplateListItem
	ComposeContent string `json:"compose_content,omitempty"`
}

func ListAllTemplates(w http.ResponseWriter, r *http.Request) {
	orgID := middleware.GetOrgID(r.Context())
	s := storeFromRequest(r)
	if s == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store unavailable"})
		return
	}

	var list []TemplateListItem

	// Built-in templates from catalog
	builtins := appCatalog.List()
	if builtins == nil {
		builtins = []catalog.Template{}
	}
	for _, t := range builtins {
		list = append(list, catalogToListItem(t.Name, "", &t, "builtin"))
	}

	// Custom templates
	customs, err := s.ListCustomTemplates(r.Context(), orgID)
	if err == nil {
		for _, ct := range customs {
			list = append(list, customToListItem(&ct))
		}
	}

	writeJSON(w, http.StatusOK, list)
}

func GetTemplate(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name required"})
		return
	}

	orgID := middleware.GetOrgID(r.Context())
	s := storeFromRequest(r)

	// Check custom templates first
	if s != nil {
		ct, err := s.GetCustomTemplateByName(r.Context(), orgID, name)
		if err == nil && ct != nil {
			detail := TemplateDetail{
				TemplateListItem: customToListItem(ct),
				ComposeContent:   ct.ComposeContent,
			}
			writeJSON(w, http.StatusOK, detail)
			return
		}
	}

	// Fall back to built-in
	tmpl, err := appCatalog.Get(name)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}

	detail := TemplateDetail{
		TemplateListItem: catalogToListItem(tmpl.Name, "", tmpl, "builtin"),
		ComposeContent:   "", // built-in single apps don't use compose_content
	}
	writeJSON(w, http.StatusOK, detail)
}

func DeployTemplate(nc *nats.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		templateName := chi.URLParam(r, "name")
		if templateName == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "template name required"})
			return
		}

		var body struct {
			ProjectID string            `json:"project_id"`
			Domain    string            `json:"domain"`
			Env       map[string]string `json:"env"`
			Volumes   []string          `json:"volumes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		if body.ProjectID == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "project_id required"})
			return
		}

		s := storeFromRequest(r)
		if s == nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store unavailable"})
			return
		}

		orgID := middleware.GetOrgID(r.Context())

		// Try custom template first
		ct, err := s.GetCustomTemplateByName(r.Context(), orgID, templateName)
		if err == nil && ct != nil {
			deployCustomTemplate(w, r, nc, s, ct, body.ProjectID, body.Domain, body.Env, body.Volumes)
			return
		}

		// Built-in template
		tmpl, err := appCatalog.Get(templateName)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}

		deployBuiltinTemplate(w, r, nc, s, tmpl, body.ProjectID, body.Domain, body.Env)
	}
}

func deployBuiltinTemplate(w http.ResponseWriter, r *http.Request, nc *nats.Conn, s *store.Store, tmpl *catalog.Template, projectID, domain string, envOverrides map[string]string) {
	env := make(map[string]string)
	for k, v := range tmpl.Env {
		env[k] = v
	}
	for k, v := range envOverrides {
		env[k] = v
	}
	if domain == "" {
		domain = tmpl.Domain
	}

	port := parsePort(tmpl.Ports)
	if port == 0 {
		port = 3000
	}

	envJSON, _ := json.Marshal(env)
	app := &store.App{
		ProjectID:      projectID,
		Name:           tmpl.Name,
		DeployType:     "image",
		Image:          tmpl.Image,
		Domain:         domain,
		Port:           port,
		Replicas:       tmpl.Replicas,
		EnvEncrypted:   envJSON,
		Status:         "deploying",
		TemplateName:    tmpl.Name,
		TemplateVersion: "", // built-in has no version
	}
	if app.Replicas == 0 {
		app.Replicas = 1
	}

	if err := s.CreateApp(r.Context(), app); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create app: " + err.Error()})
		return
	}

	dep := &store.Deployment{AppID: app.ID, Status: "deploying"}
	_ = s.CreateDeployment(r.Context(), dep)

	job, _ := json.Marshal(map[string]string{
		"action":        "deploy",
		"name":          tmpl.Name,
		"image":         tmpl.Image,
		"domain":        domain,
		"app_id":        app.ID,
		"deployment_id": dep.ID,
		"env":           string(envJSON),
	})
	nc.Publish("hive.deploy", job)

	writeJSON(w, http.StatusAccepted, app)
}

func deployCustomTemplate(w http.ResponseWriter, r *http.Request, nc *nats.Conn, s *store.Store, ct *store.CustomTemplate, projectID, domain string, envOverrides map[string]string, volumes []string) {
	// Parse env from JSON
	var env map[string]string
	if ct.Env != "" {
		_ = json.Unmarshal([]byte(ct.Env), &env)
	}
	if env == nil {
		env = make(map[string]string)
	}
	for k, v := range envOverrides {
		env[k] = v
	}
	if domain == "" {
		domain = ct.Domain
	}

	port := 3000
	var ports []string
	if ct.Ports != "" {
		_ = json.Unmarshal([]byte(ct.Ports), &ports)
		if len(ports) > 0 {
			port = parsePort(ports)
		}
	}
	if port == 0 {
		port = 3000
	}

	envJSON, _ := json.Marshal(env)
	app := &store.App{
		ProjectID:       projectID,
		Name:            ct.Name,
		DeployType:      "image",
		Image:           ct.Image,
		Domain:          domain,
		Port:            port,
		Replicas:        ct.Replicas,
		EnvEncrypted:    envJSON,
		Status:          "deploying",
		TemplateName:    ct.Name,
		TemplateVersion: ct.Version,
	}
	if app.Replicas == 0 {
		app.Replicas = 1
	}

	if ct.IsStack && ct.ComposeContent != "" {
		// Deploy as stack
		st := &store.Stack{
			ProjectID:      projectID,
			Name:           ct.Name,
			ComposeContent: ct.ComposeContent,
			Status:         "pending",
		}
		if err := s.CreateStack(r.Context(), st); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create stack: " + err.Error()})
			return
		}
		job, _ := json.Marshal(map[string]string{
			"action":   "stack_deploy",
			"stack_id": st.ID,
			"name":     st.Name,
		})
		nc.Publish("hive.deploy", job)
		writeJSON(w, http.StatusAccepted, map[string]interface{}{"stack": st})
		return
	}

	// Single service deploy
	if err := s.CreateApp(r.Context(), app); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create app: " + err.Error()})
		return
	}

	dep := &store.Deployment{AppID: app.ID, Status: "deploying"}
	_ = s.CreateDeployment(r.Context(), dep)

	job, _ := json.Marshal(map[string]string{
		"action":        "deploy",
		"name":          ct.Name,
		"image":         ct.Image,
		"domain":        domain,
		"app_id":        app.ID,
		"deployment_id": dep.ID,
		"env":           string(envJSON),
	})
	nc.Publish("hive.deploy", job)

	writeJSON(w, http.StatusAccepted, app)
}

func ExportAppAsTemplate(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	if appID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "app_id required"})
		return
	}

	s := storeFromRequest(r)
	if s == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store unavailable"})
		return
	}

	orgID := middleware.GetOrgID(r.Context())
	if orgID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no active organization"})
		return
	}

	app, err := s.GetApp(r.Context(), appID)
	if err != nil || app == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}

	port := app.Port
	if port == 0 {
		port = 3000
	}
	portsJSON, _ := json.Marshal([]string{fmt.Sprintf("%d:%d", port, port)})

	envStr := "{}"
	if len(app.EnvEncrypted) > 0 {
		envStr = string(app.EnvEncrypted)
	}

	ct := &store.CustomTemplate{
		OrgID:       orgID,
		Name:        app.Name,
		Description: "Exported from app " + app.Name,
		Category:    "custom",
		Image:       app.Image,
		Version:     "1.0.0",
		Ports:       string(portsJSON),
		Env:         envStr,
		Volumes:     "[]",
		Domain:      app.Domain,
		Replicas:    app.Replicas,
		IsStack:     false,
	}
	if ct.Replicas == 0 {
		ct.Replicas = 1
	}

	if err := s.CreateCustomTemplate(r.Context(), ct); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create template: " + err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, ct)
}

func CreateTemplateSource(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
		URL  string `json:"url"`
		Type string `json:"type"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if body.Name == "" || body.URL == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and url required"})
		return
	}
	if body.Type == "" {
		body.Type = "git"
	}

	orgID := middleware.GetOrgID(r.Context())
	if orgID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no active organization"})
		return
	}

	s := storeFromRequest(r)
	if s == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store unavailable"})
		return
	}

	ts := &store.TemplateSource{
		OrgID: orgID,
		Name:  body.Name,
		URL:   body.URL,
		Type:  body.Type,
	}
	if err := s.CreateTemplateSource(r.Context(), ts); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, ts)
}

func ListTemplateSources(w http.ResponseWriter, r *http.Request) {
	orgID := middleware.GetOrgID(r.Context())
	if orgID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no active organization"})
		return
	}

	s := storeFromRequest(r)
	if s == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store unavailable"})
		return
	}

	sources, err := s.ListTemplateSources(r.Context(), orgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if sources == nil {
		sources = []store.TemplateSource{}
	}

	writeJSON(w, http.StatusOK, sources)
}

func DeleteTemplateSource(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "sourceId")
	if sourceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source_id required"})
		return
	}

	s := storeFromRequest(r)
	if s == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store unavailable"})
		return
	}

	if err := s.DeleteTemplateSource(r.Context(), sourceID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"deleted": sourceID})
}

func SyncTemplateSource(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "sourceId")
	if sourceID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "source_id required"})
		return
	}

	orgID := middleware.GetOrgID(r.Context())
	if orgID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no active organization"})
		return
	}

	s := storeFromRequest(r)
	if s == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store unavailable"})
		return
	}

	// Get source - we need a GetTemplateSource method
	sources, err := s.ListTemplateSources(r.Context(), orgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var ts *store.TemplateSource
	for i := range sources {
		if sources[i].ID == sourceID {
			ts = &sources[i]
			break
		}
	}
	if ts == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "template source not found"})
		return
	}

	// Clone repo and parse YAML templates
	tmpDir, err := os.MkdirTemp("", "hive-template-sync-*")
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create temp dir"})
		return
	}
	defer os.RemoveAll(tmpDir)

	cloneDir := filepath.Join(tmpDir, "repo")
	cmd := exec.Command("git", "clone", "--depth", "1", ts.URL, cloneDir)
	if out, err := cmd.CombinedOutput(); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "git clone failed: " + string(out)})
		return
	}

	// Walk and parse YAML files
	imported := 0
	filepath.Walk(cloneDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var tmpl catalog.Template
		if err := yaml.Unmarshal(data, &tmpl); err != nil {
			return nil
		}
		if tmpl.Name == "" {
			return nil
		}
		envJSON := "{}"
		if len(tmpl.Env) > 0 {
			b, _ := json.Marshal(tmpl.Env)
			envJSON = string(b)
		}
		portsJSON := "[]"
		if len(tmpl.Ports) > 0 {
			b, _ := json.Marshal(tmpl.Ports)
			portsJSON = string(b)
		}
		volumesJSON := "[]"
		if len(tmpl.Volumes) > 0 {
			b, _ := json.Marshal(tmpl.Volumes)
			volumesJSON = string(b)
		}
		ct := &store.CustomTemplate{
			OrgID:       orgID,
			SourceID:    sourceID,
			Name:        tmpl.Name,
			Description: tmpl.Description,
			Category:    tmpl.Category,
			Icon:        tmpl.Icon,
			Image:       tmpl.Image,
			Version:     "1.0.0",
			Ports:       portsJSON,
			Env:         envJSON,
			Volumes:     volumesJSON,
			Domain:      tmpl.Domain,
			Replicas:    tmpl.Replicas,
			IsStack:     tmpl.IsStack,
		}
		if ct.Replicas == 0 {
			ct.Replicas = 1
		}
		if err := s.CreateCustomTemplate(r.Context(), ct); err != nil {
			return nil
		}
		imported++
		return nil
	})

	s.UpdateTemplateSyncTime(r.Context(), sourceID)

	writeJSON(w, http.StatusOK, map[string]interface{}{"synced": true, "imported": imported})
}

func ListCustomTemplates(w http.ResponseWriter, r *http.Request) {
	orgID := middleware.GetOrgID(r.Context())
	if orgID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no active organization"})
		return
	}

	s := storeFromRequest(r)
	if s == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store unavailable"})
		return
	}

	templates, err := s.ListCustomTemplates(r.Context(), orgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if templates == nil {
		templates = []store.CustomTemplate{}
	}

	writeJSON(w, http.StatusOK, templates)
}

func UpdateCustomTemplate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "templateId")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "template_id required"})
		return
	}

	var body struct {
		Name           string `json:"name"`
		Description    string `json:"description"`
		Category       string `json:"category"`
		Icon           string `json:"icon"`
		Image          string `json:"image"`
		Version        string `json:"version"`
		Ports          string `json:"ports"`
		Env            string `json:"env"`
		Volumes        string `json:"volumes"`
		Domain         string `json:"domain"`
		Replicas       int    `json:"replicas"`
		IsStack        bool   `json:"is_stack"`
		ComposeContent string `json:"compose_content"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	s := storeFromRequest(r)
	if s == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store unavailable"})
		return
	}

	ct, err := s.GetCustomTemplate(r.Context(), id)
	if err != nil || ct == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "template not found"})
		return
	}

	if body.Name != "" {
		ct.Name = body.Name
	}
	if body.Description != "" {
		ct.Description = body.Description
	}
	if body.Category != "" {
		ct.Category = body.Category
	}
	if body.Icon != "" {
		ct.Icon = body.Icon
	}
	if body.Image != "" {
		ct.Image = body.Image
	}
	if body.Version != "" {
		ct.Version = body.Version
	}
	if body.Ports != "" {
		ct.Ports = body.Ports
	}
	if body.Env != "" {
		ct.Env = body.Env
	}
	if body.Volumes != "" {
		ct.Volumes = body.Volumes
	}
	if body.Domain != "" {
		ct.Domain = body.Domain
	}
	if body.Replicas > 0 {
		ct.Replicas = body.Replicas
	}
	ct.IsStack = body.IsStack
	if body.ComposeContent != "" {
		ct.ComposeContent = body.ComposeContent
	}

	if err := s.UpdateCustomTemplate(r.Context(), ct); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, ct)
}

func DeleteCustomTemplate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "templateId")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "template_id required"})
		return
	}

	s := storeFromRequest(r)
	if s == nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store unavailable"})
		return
	}

	if err := s.DeleteCustomTemplate(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
}

func CheckTemplateUpdates(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name required"})
		return
	}

	// Stub: check not fully implemented - could query registry for newer tags
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"update_available": false,
		"current_version":  "latest",
		"latest_version":   "latest",
	})
}

func catalogToListItem(name, version string, t *catalog.Template, source string) TemplateListItem {
	if version == "" {
		version = "latest"
	}
	volumes := t.Volumes
	if volumes == nil {
		volumes = []string{}
	}
	return TemplateListItem{
		ID:          name,
		Name:        name,
		Description: t.Description,
		Category:    t.Category,
		Icon:        t.Icon,
		Image:       t.Image,
		Version:     version,
		Ports:       t.Ports,
		Env:         t.Env,
		Volumes:     volumes,
		Domain:      t.Domain,
		Replicas:    t.Replicas,
		IsStack:     t.IsStack,
		Source:      source,
	}
}

func customToListItem(ct *store.CustomTemplate) TemplateListItem {
	var ports []string
	if ct.Ports != "" {
		_ = json.Unmarshal([]byte(ct.Ports), &ports)
	}
	var env map[string]string
	if ct.Env != "" {
		_ = json.Unmarshal([]byte(ct.Env), &env)
	}
	var volumes []string
	if ct.Volumes != "" {
		_ = json.Unmarshal([]byte(ct.Volumes), &volumes)
	}
	if env == nil {
		env = map[string]string{}
	}
	return TemplateListItem{
		ID:          ct.ID,
		Name:        ct.Name,
		Description: ct.Description,
		Category:    ct.Category,
		Icon:        ct.Icon,
		Image:       ct.Image,
		Version:     ct.Version,
		Ports:       ports,
		Env:         env,
		Volumes:     volumes,
		Domain:      ct.Domain,
		Replicas:    ct.Replicas,
		IsStack:     ct.IsStack,
		Source:      "custom",
	}
}

func parsePort(ports []string) int {
	if len(ports) == 0 {
		return 0
	}
	p := ports[0]
	port := 0
	for _, c := range p {
		if c >= '0' && c <= '9' {
			port = port*10 + int(c-'0')
		} else {
			break
		}
	}
	return port
}