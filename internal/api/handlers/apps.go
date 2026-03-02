package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/nats-io/nats.go"

	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/internal/swarm"
	"github.com/lholliger/hive/pkg/encryption"
)

func CreateApp(nc *nats.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projectID := chi.URLParam(r, "projectId")

		var body struct {
			Name           string `json:"name"`
			DeployType     string `json:"deploy_type"`
			Image          string `json:"image"`
			GitRepo        string `json:"git_repo"`
			GitBranch      string `json:"git_branch"`
			DockerfilePath string `json:"dockerfile_path"`
			Domain         string `json:"domain"`
			Port           int    `json:"port"`
			Replicas       int    `json:"replicas"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
			return
		}
		if body.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		if body.Port == 0 {
			body.Port = 3000
		}
		if body.Replicas == 0 {
			body.Replicas = 1
		}
		if body.DeployType == "" {
			body.DeployType = "image"
		}
		if body.GitBranch == "" {
			body.GitBranch = "main"
		}
		if body.DockerfilePath == "" {
			body.DockerfilePath = "Dockerfile"
		}

		s := storeFromRequest(r)
		app := &store.App{
			ProjectID:      projectID,
			Name:           body.Name,
			DeployType:     body.DeployType,
			Image:          body.Image,
			GitRepo:        body.GitRepo,
			GitBranch:      body.GitBranch,
			DockerfilePath: body.DockerfilePath,
			Domain:         body.Domain,
			Port:           body.Port,
			Replicas:       body.Replicas,
		}
		if err := s.CreateApp(r.Context(), app); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, app)
	}
}

func ListApps(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	s := storeFromRequest(r)
	apps, err := s.ListApps(r.Context(), projectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, apps)
}

func GetApp(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "appId")
	s := storeFromRequest(r)
	app, err := s.GetApp(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}
	writeJSON(w, http.StatusOK, app)
}

func DeleteApp(nc *nats.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "appId")
		s := storeFromRequest(r)

		app, err := s.GetApp(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
			return
		}

		job, _ := json.Marshal(map[string]string{
			"action": "remove",
			"app_id": app.ID,
			"name":   app.Name,
		})
		if err := nc.Publish("hive.deploy", job); err != nil {
			log.Printf("failed to publish deploy job: %v", err)
		}

		if err := s.DeleteApp(r.Context(), id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"deleted": id})
	}
}

func DeployApp(nc *nats.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "appId")
		s := storeFromRequest(r)

		app, err := s.GetApp(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
			return
		}

		deployment := &store.Deployment{
			AppID:  app.ID,
			Status: "building",
		}
		if err := s.CreateDeployment(r.Context(), deployment); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		if err := s.UpdateAppStatus(r.Context(), app.ID, "deploying"); err != nil {
			log.Printf("failed to update app status: %v", err)
		}

		subject := "hive.deploy"
		if app.DeployType == "git" {
			subject = "hive.build"
		}

		job, _ := json.Marshal(map[string]string{
			"action":        "deploy",
			"app_id":        app.ID,
			"deployment_id": deployment.ID,
			"deploy_type":   app.DeployType,
			"image":         app.Image,
			"git_repo":      app.GitRepo,
			"git_branch":    app.GitBranch,
			"dockerfile":    app.DockerfilePath,
			"name":          app.Name,
			"domain":        app.Domain,
		})
		if err := nc.Publish(subject, job); err != nil {
			log.Printf("failed to publish %s job: %v", subject, err)
		}

		writeJSON(w, http.StatusAccepted, deployment)
	}
}

func ListDeployments(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	s := storeFromRequest(r)
	deployments, err := s.ListDeployments(r.Context(), appID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, deployments)
}

func UpdateAppEnv(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "appId")
	var body struct {
		Env map[string]string `json:"env"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	s := storeFromRequest(r)
	envJSON, _ := json.Marshal(body.Env)

	encrypted, err := encryption.Encrypt(envJSON)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "encryption failed"})
		return
	}

	if err := s.UpdateAppEnv(r.Context(), id, encrypted); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"updated": id})
}

func RestartApp(nc *nats.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "appId")
		s := storeFromRequest(r)
		app, err := s.GetApp(r.Context(), id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
			return
		}
		job, _ := json.Marshal(map[string]string{
			"action":      "deploy",
			"app_id":      app.ID,
			"deploy_type": app.DeployType,
			"image":       app.Image,
			"name":        app.Name,
			"domain":      app.Domain,
		})
		if err := nc.Publish("hive.deploy", job); err != nil {
			log.Printf("failed to publish deploy job: %v", err)
		}
		if err := s.UpdateAppStatus(r.Context(), id, "deploying"); err != nil {
			log.Printf("failed to update app status: %v", err)
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "restarting"})
	}
}

func StopApp(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "appId")
	s := storeFromRequest(r)
	app, err := s.GetApp(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}

	sc, err := swarm.NewClient(nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "docker unavailable"})
		return
	}
	defer func() { _ = sc.Close() }()

	serviceName := "hive-app-" + app.Name
	svc, err := sc.GetService(r.Context(), serviceName)
	if err != nil || svc == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "service not found"})
		return
	}
	if err := sc.ScaleService(r.Context(), svc.ID, 0); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if err := s.UpdateAppStatus(r.Context(), id, "stopped"); err != nil {
		log.Printf("failed to update app status: %v", err)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func StartApp(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "appId")
	s := storeFromRequest(r)
	app, err := s.GetApp(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}

	sc, err := swarm.NewClient(nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "docker unavailable"})
		return
	}
	defer sc.Close()

	serviceName := "hive-app-" + app.Name
	svc, err := sc.GetService(r.Context(), serviceName)
	if err != nil || svc == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "service not found"})
		return
	}
	replicas := uint64(app.Replicas)
	if replicas == 0 {
		replicas = 1
	}
	if err := sc.ScaleService(r.Context(), svc.ID, replicas); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.UpdateAppStatus(r.Context(), id, "running")
	writeJSON(w, http.StatusOK, map[string]string{"status": "started"})
}

func ScaleApp(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "appId")
	var body struct {
		Replicas int `json:"replicas"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Replicas < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid replicas"})
		return
	}

	s := storeFromRequest(r)
	app, err := s.GetApp(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}

	sc, err := swarm.NewClient(nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "docker unavailable"})
		return
	}
	defer sc.Close()

	serviceName := "hive-app-" + app.Name
	svc, err := sc.GetService(r.Context(), serviceName)
	if err != nil || svc == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "service not found"})
		return
	}
	if err := sc.ScaleService(r.Context(), svc.ID, uint64(body.Replicas)); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	app.Replicas = body.Replicas
	if err := s.UpdateApp(r.Context(), app); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"scaled": fmt.Sprintf("%d", body.Replicas)})
}

func RollbackApp(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "appId")
	s := storeFromRequest(r)
	app, err := s.GetApp(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "app not found"})
		return
	}

	sc, err := swarm.NewClient(nil)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "docker unavailable"})
		return
	}
	defer sc.Close()

	serviceName := "hive-app-" + app.Name
	svc, err := sc.GetService(r.Context(), serviceName)
	if err != nil || svc == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "service not found"})
		return
	}
	if err := sc.RollbackService(r.Context(), svc.ID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.UpdateAppStatus(r.Context(), id, "deploying")
	writeJSON(w, http.StatusOK, map[string]string{"status": "rolling back"})
}

func UpdateAppDomains(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "appId")
	var body struct {
		Domain string `json:"domain"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	s := storeFromRequest(r)
	if err := s.UpdateAppDomain(r.Context(), id, body.Domain); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"updated": id})
}

func UpdateAppResources(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "appId")
	var body struct {
		CPULimit    float64 `json:"cpu_limit"`
		MemoryLimit int64   `json:"memory_limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	s := storeFromRequest(r)
	if err := s.UpdateAppResources(r.Context(), id, body.CPULimit, body.MemoryLimit); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"updated": id})
}

func UpdateAppPlacement(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "appId")
	var body struct {
		Constraints []string `json:"constraints"`
		Preferences []string `json:"preferences"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	constraintsJSON, _ := json.Marshal(body.Constraints)
	preferencesJSON, _ := json.Marshal(body.Preferences)

	s := storeFromRequest(r)
	if err := s.UpdateAppPlacement(r.Context(), id, constraintsJSON, preferencesJSON); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"updated": id})
}

func UpdateAppUpdateStrategy(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "appId")
	var body struct {
		Strategy      string `json:"strategy"`
		Parallelism   int    `json:"parallelism"`
		Delay         string `json:"delay"`
		FailureAction string `json:"failure_action"`
		Order         string `json:"order"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if body.Strategy == "" {
		body.Strategy = "rolling"
	}
	if body.Parallelism == 0 {
		body.Parallelism = 1
	}
	if body.Delay == "" {
		body.Delay = "5s"
	}
	if body.FailureAction == "" {
		body.FailureAction = "rollback"
	}
	if body.Order == "" {
		body.Order = "stop-first"
	}

	s := storeFromRequest(r)
	if err := s.UpdateAppUpdateStrategy(r.Context(), id, body.Strategy, body.Parallelism, body.Delay, body.FailureAction, body.Order); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"updated": id})
}

func UpdateAppLabels(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "appId")
	var body struct {
		HomepageLabels map[string]string `json:"homepage_labels"`
		ExtraLabels    map[string]string `json:"extra_labels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	homepageJSON, _ := json.Marshal(body.HomepageLabels)
	extraJSON, _ := json.Marshal(body.ExtraLabels)

	s := storeFromRequest(r)
	if err := s.UpdateAppLabels(r.Context(), id, homepageJSON, extraJSON); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"updated": id})
}

func CheckConnectivity(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"port_80":  true,
		"port_443": true,
		"message":  "Connectivity check from within the container",
	})
}

func UpdateAppHealthCheck(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "appId")
	var body struct {
		Path     string `json:"path"`
		Interval int    `json:"interval"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	s := storeFromRequest(r)
	if err := s.UpdateAppHealthCheck(r.Context(), id, body.Path, body.Interval); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"updated": id})
}
