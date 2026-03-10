package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/lholliger/hive/internal/catalog"
	"github.com/lholliger/hive/internal/deploy"
	"github.com/lholliger/hive/internal/store"
	"gopkg.in/yaml.v3"
)

func (s *Server) templatesDir() string {
	if s.cfg != nil && s.cfg.DataDir != "" {
		p := filepath.Join(s.cfg.DataDir, "templates")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if _, err := os.Stat("/app/templates"); err == nil {
		return "/app/templates"
	}
	return "templates"
}

func (s *Server) loadCatalog() *catalog.Catalog {
	c, err := catalog.LoadFromDir(s.templatesDir())
	if err != nil {
		s.log.Warnf("failed to load catalog: %v", err)
		return &catalog.Catalog{}
	}
	return c
}

type templateListItem struct {
	catalog.Template
	ID     string `json:"id"`
	Source string `json:"source"`
}

// Template sources
func (s *Server) apiCreateTemplateSource(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	var ts store.TemplateSource
	if err := json.NewDecoder(r.Body).Decode(&ts); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	ts.OrgID = user.OrgID
	if ts.Name == "" || ts.URL == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "name and url are required", nil)
		return
	}
	if err := s.store.CreateTemplateSource(r.Context(), &ts); handleErr(w, err) {
		return
	}
	s.auditLog(r, "create", "template_source", ts.ID, "")
	writeJSON(w, http.StatusCreated, ts)
}

func (s *Server) apiListTemplateSources(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	sources, err := s.store.ListTemplateSources(r.Context(), user.OrgID)
	if handleErr(w, err) {
		return
	}
	if sources == nil {
		sources = []store.TemplateSource{}
	}
	writeJSON(w, http.StatusOK, sources)
}

func (s *Server) apiDeleteTemplateSource(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "sourceId")
	if err := s.store.DeleteTemplateSource(r.Context(), id); handleErr(w, err) {
		return
	}
	s.auditLog(r, "delete", "template_source", id, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) apiSyncTemplateSource(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "sourceId")

	sources, err := s.store.ListTemplateSources(r.Context(), user.OrgID)
	if handleErr(w, err) {
		return
	}
	var source *store.TemplateSource
	for i := range sources {
		if sources[i].ID == id {
			source = &sources[i]
			break
		}
	}
	if source == nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "template source not found", nil)
		return
	}

	resp, err := http.Get(source.URL)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "bad_gateway", fmt.Sprintf("failed to fetch source: %v", err), nil)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "bad_gateway", "failed to read response", nil)
		return
	}

	var templates []catalog.Template
	if err := yaml.Unmarshal(body, &templates); err != nil {
		var single catalog.Template
		if err2 := yaml.Unmarshal(body, &single); err2 != nil {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "failed to parse templates from source", nil)
			return
		}
		templates = []catalog.Template{single}
	}

	imported := 0
	for _, t := range templates {
		if t.Name == "" {
			continue
		}
		if err := catalog.ValidateTemplate(t); err != nil {
			s.log.Warnf("template source sync: skip invalid template %s: %v", t.Name, err)
			continue
		}
		portsJSON, _ := json.Marshal(t.Ports)
		envJSON, _ := json.Marshal(t.Env)
		volsJSON, _ := json.Marshal(t.Volumes)

		existing, _ := s.store.GetCustomTemplateByName(r.Context(), user.OrgID, t.Name)
		if existing != nil {
			existing.Description = t.Description
			existing.Category = t.Category
			existing.Icon = t.Icon
			existing.Image = t.Image
			existing.Ports = string(portsJSON)
			existing.Env = string(envJSON)
			existing.Volumes = string(volsJSON)
			existing.Domain = t.Domain
			existing.Replicas = t.Replicas
			existing.IsStack = t.IsStack
			if err := s.store.UpdateCustomTemplate(r.Context(), existing); err != nil {
				s.log.Warnf("template source sync: update %s: %v", t.Name, err)
				continue
			}
		} else {
			ct := &store.CustomTemplate{
				OrgID:       user.OrgID,
				SourceID:    id,
				Name:        t.Name,
				Description: t.Description,
				Category:    t.Category,
				Icon:        t.Icon,
				Image:       t.Image,
				Ports:       string(portsJSON),
				Env:         string(envJSON),
				Volumes:     string(volsJSON),
				Domain:      t.Domain,
				Replicas:    t.Replicas,
				IsStack:     t.IsStack,
			}
			if err := s.store.CreateCustomTemplate(r.Context(), ct); err != nil {
				s.log.Warnf("template source sync: create %s: %v", t.Name, err)
				continue
			}
		}
		imported++
	}

	_ = s.store.UpdateTemplateSyncTime(r.Context(), id)
	s.auditLog(r, "sync", "template_source", id, fmt.Sprintf("imported %d", imported))
	writeJSON(w, http.StatusOK, map[string]any{"synced": true, "imported": imported})
}

// Custom templates
func (s *Server) apiCreateCustomTemplate(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	var ct store.CustomTemplate
	if err := json.NewDecoder(r.Body).Decode(&ct); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	ct.OrgID = user.OrgID
	if ct.Name == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "name is required", nil)
		return
	}
	if err := s.store.CreateCustomTemplate(r.Context(), &ct); handleErr(w, err) {
		return
	}
	s.auditLog(r, "create", "custom_template", ct.ID, "")
	writeJSON(w, http.StatusCreated, ct)
}

func (s *Server) apiListCustomTemplates(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	templates, err := s.store.ListCustomTemplates(r.Context(), user.OrgID)
	if handleErr(w, err) {
		return
	}
	if templates == nil {
		templates = []store.CustomTemplate{}
	}
	writeJSON(w, http.StatusOK, templates)
}

func (s *Server) apiGetCustomTemplate(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "id")
	ct, err := s.store.GetCustomTemplate(r.Context(), id)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, ct)
}

func (s *Server) apiUpdateCustomTemplate(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "id")
	var ct store.CustomTemplate
	if err := json.NewDecoder(r.Body).Decode(&ct); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	ct.ID = id
	if err := s.store.UpdateCustomTemplate(r.Context(), &ct); handleErr(w, err) {
		return
	}
	s.auditLog(r, "update", "custom_template", id, "")
	writeJSON(w, http.StatusOK, ct)
}

func (s *Server) apiDeleteCustomTemplate(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "id")
	if err := s.store.DeleteCustomTemplate(r.Context(), id); handleErr(w, err) {
		return
	}
	s.auditLog(r, "delete", "custom_template", id, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// Builtin templates from templates/ YAML directory (using catalog package)
func (s *Server) apiListBuiltinTemplates(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	c := s.loadCatalog()
	raw := c.List()
	if raw == nil {
		raw = []catalog.Template{}
	}
	items := make([]templateListItem, len(raw))
	for i, t := range raw {
		items[i] = templateListItem{
			Template: t,
			ID:       t.Name,
			Source:   "builtin",
		}
	}
	writeJSON(w, http.StatusOK, items)
}

func (s *Server) apiGetBuiltinTemplate(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	name := chi.URLParam(r, "name")
	c := s.loadCatalog()
	t, err := c.Get(name)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", err.Error(), nil)
		return
	}
	if err := catalog.ValidateTemplate(*t); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
		return
	}
	item := templateListItem{
		Template: *t,
		ID:       t.Name,
		Source:   "builtin",
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) apiDeployTemplate(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	name := chi.URLParam(r, "name")
	c := s.loadCatalog()
	t, err := c.Get(name)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", err.Error(), nil)
		return
	}

	var req struct {
		ProjectID     string            `json:"project_id"`
		AppName       string            `json:"app_name"`
		Domain        string            `json:"domain"`
		Env           map[string]string `json:"env"`
		StorageHostID string            `json:"storage_host_id"`
		DBStorageMode string            `json:"db_storage_mode"`
		DBNodeID      string            `json:"db_node_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}

	if req.ProjectID == "" {
		projectID, err := s.findOrCreateDefaultProject(r, user.OrgID)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "internal_error", "failed to resolve project", nil)
			return
		}
		req.ProjectID = projectID
	}
	if _, err := s.requireProjectAccess(r.Context(), req.ProjectID, user.OrgID); errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	} else if handleErr(w, err) {
		return
	}

	appName := req.AppName
	if appName == "" {
		appName = t.Name
	}
	domain := req.Domain
	if domain == "" {
		domain = t.Domain
	}

	port := 0
	if len(t.Ports) > 0 {
		_, _ = fmt.Sscanf(strings.Split(t.Ports[0], ":")[0], "%d", &port)
	}
	if port == 0 {
		port = 3000
	}

	if t.IsStack && len(t.Services) > 0 {
		composeContent := buildComposeFromTemplate(t)
		if err := deploy.ValidateCompose(composeContent); err != nil {
			writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid generated compose: "+err.Error(), nil)
			return
		}
		stack := &store.Stack{
			ProjectID:      req.ProjectID,
			Name:           appName,
			Domain:         domain,
			ComposeContent: composeContent,
			Status:         "pending",
		}
		if err := s.store.CreateStack(r.Context(), stack); handleErr(w, err) {
			return
		}

		payload, _ := json.Marshal(map[string]any{
			"job_id":   uuid.New().String(),
			"action":   "stack_deploy",
			"stack_id": stack.ID,
			"name":     stack.Name,
			"compose":  composeContent,
			"domain":   domain,
		})
		_ = s.nc.Publish("hive.deploy", payload)

		s.auditLog(r, "deploy_template", "stack", stack.ID, t.Name)
		writeJSON(w, http.StatusCreated, map[string]any{"stack": stack})
		return
	}

	a := &store.App{
		ProjectID:    req.ProjectID,
		Name:         appName,
		DeployType:   "image",
		Image:        t.Image,
		Domain:       domain,
		Port:         port,
		Replicas:     t.Replicas,
		TemplateName: t.Name,
	}
	if err := s.store.CreateApp(r.Context(), a); handleErr(w, err) {
		return
	}
	d := &store.Deployment{AppID: a.ID, Status: "deploying"}
	if err := s.store.CreateDeployment(r.Context(), d); handleErr(w, err) {
		return
	}
	_ = s.store.UpdateAppStatus(r.Context(), a.ID, "deploying")

	mergedEnv := make(map[string]string)
	for k, v := range t.Env {
		mergedEnv[k] = v
	}
	for k, v := range req.Env {
		mergedEnv[k] = v
	}
	for k, v := range mergedEnv {
		ev := &store.AppEnvVar{AppID: a.ID, Key: k, ValueEncrypted: []byte(v), Source: "template"}
		_ = s.store.CreateAppEnvVar(r.Context(), ev)
	}

	// Create NAS volumes from template and attach to app
	for _, nv := range t.NASVolumes {
		vol := &store.Volume{
			ProjectID:     req.ProjectID,
			Name:          appName + "-" + nv.Name,
			MountType:     "nfs",
			StorageHostID: req.StorageHostID,
			Scope:         "project",
			Status:        "pending",
		}
		if req.StorageHostID != "" {
			if sh, err := s.store.GetStorageHost(r.Context(), req.StorageHostID); err == nil {
				vol.RemoteHost = sh.Address
				vol.RemotePath = sh.DefaultExportPath + "/" + appName + "/" + nv.Name
			}
		}
		if err := s.store.CreateVolume(r.Context(), vol); err == nil {
			if dockerErr := s.ensureDockerVolume(r.Context(), vol, "", ""); dockerErr != nil {
				s.log.Warnf("template deploy: create NAS volume %s: %v", vol.Name, dockerErr)
			} else {
				_ = s.store.UpdateVolumeStatus(r.Context(), vol.ID, "active")
			}
			containerPath := nv.SuggestedPath
			if containerPath == "" {
				containerPath = "/" + nv.Name
			}
			av := &store.AppVolume{
				AppID:         a.ID,
				VolumeID:      vol.ID,
				ContainerPath: containerPath,
			}
			_ = s.store.AttachVolume(r.Context(), av)
		}
	}

	// Provision dependent databases
	for _, dep := range t.DependsOn {
		storageMode := req.DBStorageMode
		if storageMode == "" {
			storageMode = "local"
		}
		db := &store.ManagedDatabase{
			ProjectID:     req.ProjectID,
			Name:          appName + "-" + dep.Type,
			DBType:        dep.Type,
			Version:       dep.Version,
			Status:        "pending",
			StorageMode:   storageMode,
			StorageHostID: req.StorageHostID,
			NodeID:        req.DBNodeID,
		}
		if db.Version == "" {
			db.Version = "latest"
		}
		if err := s.store.CreateManagedDatabase(r.Context(), db); err == nil && s.nc != nil {
			jobData := map[string]string{
				"action":       "provision",
				"db_id":        db.ID,
				"name":         db.Name,
				"db_type":      db.DBType,
				"version":      db.Version,
				"storage_mode": storageMode,
			}
			if req.StorageHostID != "" {
				jobData["storage_host_id"] = req.StorageHostID
			}
			if req.DBNodeID != "" {
				jobData["node_id"] = req.DBNodeID
			}
			job, _ := json.Marshal(jobData)
			_ = s.nc.Publish("hive.deploy", job)
		}
	}

	s.publishDeploy(a, d.ID)
	s.auditLog(r, "deploy_template", "app", a.ID, t.Name)
	writeJSON(w, http.StatusCreated, a)
}

func (s *Server) findOrCreateDefaultProject(r *http.Request, orgID string) (string, error) {
	projects, err := s.store.ListProjects(r.Context(), orgID)
	if err != nil {
		return "", err
	}
	for _, p := range projects {
		if p.Name == "My Apps" {
			return p.ID, nil
		}
	}
	if len(projects) > 0 {
		return projects[0].ID, nil
	}
	p := &store.Project{Name: "My Apps", OrgID: orgID}
	if err := s.store.CreateProject(r.Context(), p); err != nil {
		return "", err
	}
	return p.ID, nil
}

func (s *Server) apiExportAppAsTemplate(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	appID := chi.URLParam(r, "appId")
	a, err := s.store.GetApp(r.Context(), appID)
	if handleErr(w, err) {
		return
	}

	envVars, _ := s.store.ListAppEnvVars(r.Context(), appID)
	envJSON := "{}"
	if len(envVars) > 0 {
		envMap := make(map[string]string)
		for _, ev := range envVars {
			if !ev.IsSecret {
				envMap[ev.Key] = string(ev.ValueEncrypted)
			}
		}
		b, _ := json.Marshal(envMap)
		envJSON = string(b)
	}

	vols, _ := s.store.ListAppVolumes(r.Context(), appID)
	volsJSON := "[]"
	if len(vols) > 0 {
		var volPaths []string
		for _, v := range vols {
			volPaths = append(volPaths, v.ContainerPath)
		}
		b, _ := json.Marshal(volPaths)
		volsJSON = string(b)
	}

	portsJSON, _ := json.Marshal([]string{fmt.Sprintf("%d", a.Port)})

	ct := &store.CustomTemplate{
		OrgID:       user.OrgID,
		Name:        a.Name,
		Description: fmt.Sprintf("Exported from app %s", a.Name),
		Image:       a.Image,
		Ports:       string(portsJSON),
		Env:         envJSON,
		Volumes:     volsJSON,
		Domain:      a.Domain,
		Replicas:    a.Replicas,
	}
	if err := s.store.CreateCustomTemplate(r.Context(), ct); handleErr(w, err) {
		return
	}
	s.auditLog(r, "export_template", "app", a.ID, ct.Name)
	writeJSON(w, http.StatusCreated, ct)
}

func buildComposeFromTemplate(t *catalog.Template) string {
	type composeHealthcheck struct {
		Test        interface{} `yaml:"test,omitempty"`
		Interval    string      `yaml:"interval,omitempty"`
		Timeout     string      `yaml:"timeout,omitempty"`
		Retries     int         `yaml:"retries,omitempty"`
		StartPeriod string      `yaml:"start_period,omitempty"`
	}
	type composeService struct {
		Image       string              `yaml:"image"`
		Ports       []string            `yaml:"ports,omitempty"`
		Environment map[string]string   `yaml:"environment,omitempty"`
		Volumes     []string            `yaml:"volumes,omitempty"`
		Command     interface{}         `yaml:"command,omitempty"`
		Entrypoint  interface{}         `yaml:"entrypoint,omitempty"`
		Healthcheck *composeHealthcheck `yaml:"healthcheck,omitempty"`
		User        string              `yaml:"user,omitempty"`
		CapAdd      []string            `yaml:"cap_add,omitempty"`
		CapDrop     []string            `yaml:"cap_drop,omitempty"`
		Deploy      map[string]any      `yaml:"deploy,omitempty"`
	}
	type composeVolumeDef struct{}
	type composeFile struct {
		Version  string                      `yaml:"version"`
		Services map[string]composeService   `yaml:"services"`
		Volumes  map[string]composeVolumeDef `yaml:"volumes,omitempty"`
	}

	cf := composeFile{
		Version:  "3.8",
		Services: make(map[string]composeService),
		Volumes:  make(map[string]composeVolumeDef),
	}

	for _, svc := range t.Services {
		cs := composeService{
			Image:       svc.Image,
			Ports:       svc.Ports,
			Environment: svc.Env,
			Volumes:     svc.Volumes,
			Command:     svc.Command,
			Entrypoint:  svc.Entrypoint,
			User:        svc.User,
			CapAdd:      svc.CapAdd,
			CapDrop:     svc.CapDrop,
			Deploy: map[string]any{
				"restart_policy": map[string]string{
					"condition": "on-failure",
				},
			},
		}
		if svc.Healthcheck != nil {
			cs.Healthcheck = &composeHealthcheck{
				Test:        svc.Healthcheck.Test,
				Interval:    svc.Healthcheck.Interval,
				Timeout:     svc.Healthcheck.Timeout,
				Retries:     svc.Healthcheck.Retries,
				StartPeriod: svc.Healthcheck.StartPeriod,
			}
		}
		cf.Services[svc.Name] = cs

		for _, v := range svc.Volumes {
			parts := strings.SplitN(v, ":", 2)
			if len(parts) >= 1 && !strings.HasPrefix(parts[0], "/") && !strings.HasPrefix(parts[0], ".") {
				cf.Volumes[parts[0]] = composeVolumeDef{}
			}
		}
	}

	out, _ := yaml.Marshal(cf)
	return string(out)
}
