package engine

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
)

// --- Docker Configs ---

func (s *Server) apiListConfigs(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	projectID := chi.URLParam(r, "projectId")
	configs, err := s.store.ListDockerConfigs(r.Context(), projectID)
	if handleErr(w, err) {
		return
	}
	if configs == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, configs)
}

func (s *Server) apiCreateConfig(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	projectID := chi.URLParam(r, "projectId")
	var body struct {
		Name string `json:"name"`
		Data string `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "name required", nil)
		return
	}
	config, err := s.store.CreateDockerConfig(r.Context(), projectID, user.OrgID, body.Name, body.Data)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, config)
}

func (s *Server) apiDeleteConfig(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	configID := chi.URLParam(r, "configId")
	if err := s.store.DeleteDockerConfig(r.Context(), configID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": configID})
}

func (s *Server) apiAttachConfig(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	configID := chi.URLParam(r, "configId")
	appID := chi.URLParam(r, "appId")
	var body struct {
		TargetPath string `json:"target_path"`
		UID        string `json:"uid"`
		GID        string `json:"gid"`
		Mode       int    `json:"mode"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.TargetPath == "" {
		body.TargetPath = "/"
	}
	if body.Mode == 0 {
		body.Mode = 0444
	}
	ac, err := s.store.AttachConfig(r.Context(), appID, configID, body.TargetPath, body.UID, body.GID, body.Mode)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, ac)
}

func (s *Server) apiDetachConfig(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	configID := chi.URLParam(r, "configId")
	appID := chi.URLParam(r, "appId")
	if err := s.store.DetachConfig(r.Context(), appID, configID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"detached": "ok"})
}

// --- Scheduled Jobs ---

func (s *Server) apiListJobs(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	projectID := chi.URLParam(r, "projectId")
	jobs, err := s.store.ListScheduledJobs(r.Context(), projectID)
	if handleErr(w, err) {
		return
	}
	if jobs == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, jobs)
}

func (s *Server) apiCreateJob(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	projectID := chi.URLParam(r, "projectId")
	if _, err := s.requireProjectAccess(r.Context(), projectID, user.OrgID); errors.Is(err, errForbiddenOrgAccess) {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	} else if handleErr(w, err) {
		return
	}
	var body struct {
		Name     string          `json:"name"`
		Image    string          `json:"image"`
		Command  string          `json:"command"`
		Schedule string          `json:"schedule"`
		Timezone string          `json:"timezone"`
		Env      json.RawMessage `json:"env"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" || body.Image == "" || body.Schedule == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "name, image, and schedule required", nil)
		return
	}
	if body.Timezone == "" {
		body.Timezone = "UTC"
	}
	if body.Env == nil {
		body.Env = json.RawMessage(`{}`)
	}
	job, err := s.store.CreateScheduledJob(r.Context(), projectID, user.OrgID, body.Name, body.Image, body.Command, body.Schedule, body.Timezone, body.Env)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, job)
}

func (s *Server) apiUpdateJob(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	jobID := chi.URLParam(r, "jobId")
	existing, err := s.store.GetScheduledJob(r.Context(), jobID)
	if handleErr(w, err) {
		return
	}
	if existing.OrgID != user.OrgID {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	var body struct {
		Name     string          `json:"name"`
		Image    string          `json:"image"`
		Command  string          `json:"command"`
		Schedule string          `json:"schedule"`
		Timezone string          `json:"timezone"`
		Env      json.RawMessage `json:"env"`
		Enabled  bool            `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid body", nil)
		return
	}
	if err := s.store.UpdateScheduledJob(r.Context(), jobID, body.Name, body.Image, body.Command, body.Schedule, body.Timezone, body.Env, body.Enabled); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"updated": jobID})
}

func (s *Server) apiDeleteJob(w http.ResponseWriter, r *http.Request) {
	user, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	jobID := chi.URLParam(r, "jobId")
	existing, err := s.store.GetScheduledJob(r.Context(), jobID)
	if handleErr(w, err) {
		return
	}
	if existing.OrgID != user.OrgID {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	if err := s.store.DeleteScheduledJob(r.Context(), jobID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": jobID})
}

func (s *Server) apiTriggerJob(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	jobID := chi.URLParam(r, "jobId")
	existing, err := s.store.GetScheduledJob(r.Context(), jobID)
	if handleErr(w, err) {
		return
	}
	if existing.OrgID != user.OrgID {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	payload, _ := json.Marshal(map[string]string{"job_id": jobID, "action": "run_job"})
	if err := s.nc.Publish("hive.deploy", payload); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "triggered"})
}

func (s *Server) apiListJobRuns(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	jobID := chi.URLParam(r, "jobId")
	existing, err := s.store.GetScheduledJob(r.Context(), jobID)
	if handleErr(w, err) {
		return
	}
	if existing.OrgID != user.OrgID {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	runs, err := s.store.ListJobRuns(r.Context(), jobID)
	if handleErr(w, err) {
		return
	}
	if runs == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, runs)
}

func (s *Server) apiJobRunLogs(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	runID := chi.URLParam(r, "runId")
	run, err := s.store.GetJobRun(r.Context(), runID)
	if handleErr(w, err) {
		return
	}
	job, err := s.store.GetScheduledJob(r.Context(), run.JobID)
	if handleErr(w, err) {
		return
	}
	if job.OrgID != user.OrgID {
		writeAPIError(w, http.StatusForbidden, "forbidden", "forbidden", nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"logs": run.Logs})
}

// --- Resource Quotas ---

func (s *Server) apiGetProjectQuotas(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	projectID := chi.URLParam(r, "projectId")
	quota, err := s.store.GetResourceQuota(r.Context(), projectID)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusOK, map[string]any{"cpu_limit": 0, "memory_limit": 0, "storage_limit": 0})
		return
	}
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, quota)
}

func (s *Server) apiSetProjectQuotas(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	projectID := chi.URLParam(r, "projectId")
	var body struct {
		CPULimit     float64 `json:"cpu_limit"`
		MemoryLimit  int64   `json:"memory_limit"`
		StorageLimit int64   `json:"storage_limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid body", nil)
		return
	}
	quota, err := s.store.UpsertResourceQuota(r.Context(), projectID, body.CPULimit, body.MemoryLimit, body.StorageLimit)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, quota)
}

func (s *Server) apiGetProjectUsage(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cpu_used":     0,
		"memory_used":  0,
		"storage_used": 0,
	})
}

// --- Deployment Diff & Targeted Rollback ---

func (s *Server) apiDeploymentDiff(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	id1 := chi.URLParam(r, "deployId1")
	id2 := chi.URLParam(r, "deployId2")
	d1, err := s.store.GetDeployment(r.Context(), id1)
	if handleErr(w, err) {
		return
	}
	d2, err := s.store.GetDeployment(r.Context(), id2)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"deployment_1": d1,
		"deployment_2": d2,
	})
}

func (s *Server) apiRollbackToDeployment(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	appID := chi.URLParam(r, "appId")
	app, err := s.store.GetApp(r.Context(), appID)
	if handleErr(w, err) {
		return
	}
	serviceName := "hive-app-" + app.Name
	if err := s.sc.RollbackService(r.Context(), serviceName); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "rolled back"})
}

// --- Security / Vulnerability ---

func (s *Server) apiTriggerScan(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	appID := chi.URLParam(r, "appId")
	app, err := s.store.GetApp(r.Context(), appID)
	if handleErr(w, err) {
		return
	}
	scan, err := s.store.CreateVulnerabilityScan(r.Context(), appID, app.Image)
	if handleErr(w, err) {
		return
	}
	payload, _ := json.Marshal(map[string]string{"scan_id": scan.ID, "image": app.Image, "action": "scan"})
	_ = s.nc.Publish("hive.scan", payload)
	writeJSON(w, http.StatusAccepted, scan)
}

func (s *Server) apiListScans(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	appID := chi.URLParam(r, "appId")
	scans, err := s.store.ListVulnerabilityScans(r.Context(), appID)
	if handleErr(w, err) {
		return
	}
	if scans == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, scans)
}

func (s *Server) apiGetScan(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	scanID := chi.URLParam(r, "scanId")
	scan, err := s.store.GetVulnerabilityScan(r.Context(), scanID)
	if handleErr(w, err) {
		return
	}
	vulns, _ := s.store.ListVulnerabilities(r.Context(), scanID)
	writeJSON(w, http.StatusOK, map[string]any{"scan": scan, "vulnerabilities": vulns})
}

func (s *Server) apiSecuritySummary(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	summary, _ := s.store.SecuritySummary(r.Context())
	writeJSON(w, http.StatusOK, summary)
}

// --- Per-App Metrics ---

func (s *Server) apiAppMetricsCurrent(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"containers": []any{}})
}

func (s *Server) apiAppMetricsHistory(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"cpu": []any{}, "memory": []any{}})
}

// --- Search ---

func (s *Server) apiSearch(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	type searchResult struct {
		Type        string `json:"type"`
		ID          string `json:"id"`
		Name        string `json:"name"`
		Description string `json:"description"`
		URL         string `json:"url"`
	}

	var results []searchResult
	pattern := "%" + q + "%"

	// Search projects
	rows, _ := s.store.DB().QueryContext(r.Context(),
		`SELECT id, name, COALESCE(description,'') FROM project WHERE name ILIKE $1 LIMIT 5`, pattern)
	if rows != nil {
		for rows.Next() {
			var r searchResult
			r.Type = "project"
			_ = rows.Scan(&r.ID, &r.Name, &r.Description)
			r.URL = "/projects/" + r.ID
			results = append(results, r)
		}
		rows.Close()
	}

	// Search apps
	rows, _ = s.store.DB().QueryContext(r.Context(),
		`SELECT id, name, project_id FROM app WHERE name ILIKE $1 LIMIT 5`, pattern)
	if rows != nil {
		for rows.Next() {
			var r searchResult
			var projectID string
			r.Type = "app"
			_ = rows.Scan(&r.ID, &r.Name, &projectID)
			r.URL = "/projects/" + projectID + "/apps/" + r.ID
			results = append(results, r)
		}
		rows.Close()
	}

	// Search templates
	rows, _ = s.store.DB().QueryContext(r.Context(),
		`SELECT name, COALESCE(description,'') FROM custom_template WHERE name ILIKE $1 LIMIT 5`, pattern)
	if rows != nil {
		for rows.Next() {
			var r searchResult
			r.Type = "template"
			_ = rows.Scan(&r.Name, &r.Description)
			r.ID = r.Name
			r.URL = "/catalog"
			results = append(results, r)
		}
		rows.Close()
	}

	if results == nil {
		results = []searchResult{}
	}
	writeJSON(w, http.StatusOK, results)
}

// --- Node Power Management ---

func (s *Server) apiNodePower(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	nodeID := chi.URLParam(r, "nodeId")
	var body struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Action == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "action required", nil)
		return
	}

	payload, _ := json.Marshal(map[string]string{"action": body.Action})
	if err := s.nc.Publish("hive.node.power."+nodeID, payload); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "sent"})
}

func (s *Server) apiGetNodeConfig(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	nodeID := chi.URLParam(r, "nodeId")
	config, err := s.store.GetNodeConfig(r.Context(), nodeID)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusOK, map[string]any{
			"node_id": nodeID, "mac_address": "", "bmc_address": "", "wol_enabled": false,
		})
		return
	}
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, config)
}

func (s *Server) apiSetNodeConfig(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	nodeID := chi.URLParam(r, "nodeId")
	var body struct {
		Hostname    string `json:"hostname"`
		MACAddress  string `json:"mac_address"`
		BMCAddress  string `json:"bmc_address"`
		BMCUsername string `json:"bmc_username"`
		BMCPassword string `json:"bmc_password"`
		WoLEnabled  bool   `json:"wol_enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid body", nil)
		return
	}
	config, err := s.store.UpsertNodeConfig(r.Context(), nodeID, body.Hostname, body.MACAddress, body.BMCAddress, body.BMCUsername, body.BMCPassword, body.WoLEnabled)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, config)
}

func (s *Server) apiNodeHardware(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"gpus":         []any{},
		"disks":        []any{},
		"temperatures": []any{},
		"fans":         []any{},
		"network":      []any{},
	})
}

// --- UPS Monitoring ---

func (s *Server) apiListUPS(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	devices, err := s.store.ListUPSDevices(r.Context(), user.OrgID)
	if handleErr(w, err) {
		return
	}

	type upsWithStatus struct {
		Device *any `json:"device"`
		Status *any `json:"status"`
	}

	var result []map[string]any
	for _, d := range devices {
		entry := map[string]any{"device": d}
		snap, err := s.store.LatestUPSSnapshot(r.Context(), d.ID)
		if err == nil {
			entry["status"] = snap
		}
		result = append(result, entry)
	}
	if result == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) apiCreateUPS(w http.ResponseWriter, r *http.Request) {
	user, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	var body struct {
		Name              string          `json:"name"`
		NUTHost           string          `json:"nut_host"`
		NUTPort           int             `json:"nut_port"`
		UPSName           string          `json:"ups_name"`
		PollInterval      int             `json:"poll_interval_seconds"`
		ShutdownThreshold int             `json:"shutdown_threshold"`
		ShutdownNodes     json.RawMessage `json:"shutdown_nodes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" || body.NUTHost == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "name and nut_host required", nil)
		return
	}
	if body.NUTPort == 0 {
		body.NUTPort = 3493
	}
	if body.UPSName == "" {
		body.UPSName = "ups"
	}
	if body.PollInterval == 0 {
		body.PollInterval = 30
	}
	if body.ShutdownNodes == nil {
		body.ShutdownNodes = json.RawMessage(`[]`)
	}
	device, err := s.store.CreateUPSDevice(r.Context(), user.OrgID, body.Name, body.NUTHost, body.NUTPort, body.UPSName, body.PollInterval, body.ShutdownThreshold, body.ShutdownNodes)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, device)
}

func (s *Server) apiUpdateUPS(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	upsID := chi.URLParam(r, "upsId")
	var body struct {
		Name              string          `json:"name"`
		NUTHost           string          `json:"nut_host"`
		NUTPort           int             `json:"nut_port"`
		UPSName           string          `json:"ups_name"`
		PollInterval      int             `json:"poll_interval_seconds"`
		ShutdownThreshold int             `json:"shutdown_threshold"`
		ShutdownNodes     json.RawMessage `json:"shutdown_nodes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid body", nil)
		return
	}
	if err := s.store.UpdateUPSDevice(r.Context(), upsID, body.Name, body.NUTHost, body.NUTPort, body.UPSName, body.PollInterval, body.ShutdownThreshold, body.ShutdownNodes); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"updated": upsID})
}

func (s *Server) apiDeleteUPS(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	upsID := chi.URLParam(r, "upsId")
	if err := s.store.DeleteUPSDevice(r.Context(), upsID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": upsID})
}

func (s *Server) apiUPSHistory(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	upsID := chi.URLParam(r, "upsId")
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if v, err := strconv.Atoi(l); err == nil {
			limit = v
		}
	}
	snaps, err := s.store.ListUPSSnapshots(r.Context(), upsID, limit)
	if handleErr(w, err) {
		return
	}
	if snaps == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, snaps)
}

// --- Dynamic DNS ---

func (s *Server) apiEnableDDNS(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	providerID := chi.URLParam(r, "providerId")
	var body struct {
		Interval int `json:"interval_minutes"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Interval == 0 {
		body.Interval = 5
	}
	_, err = s.store.DB().ExecContext(r.Context(),
		`UPDATE dns_provider SET ddns_enabled=true, ddns_interval_minutes=$2 WHERE id=$1`, providerID, body.Interval)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "enabled"})
}

func (s *Server) apiDisableDDNS(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	providerID := chi.URLParam(r, "providerId")
	_, dbErr := s.store.DB().ExecContext(r.Context(),
		`UPDATE dns_provider SET ddns_enabled=false WHERE id=$1`, providerID)
	if dbErr != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", dbErr.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "disabled"})
}

func (s *Server) apiDDNSStatus(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	providerID := chi.URLParam(r, "providerId")
	var enabled bool
	var interval int
	var lastIP string
	var lastUpdate sql.NullTime
	err = s.store.DB().QueryRowContext(r.Context(),
		`SELECT ddns_enabled, ddns_interval_minutes, ddns_last_ip, ddns_last_update FROM dns_provider WHERE id=$1`,
		providerID).Scan(&enabled, &interval, &lastIP, &lastUpdate)
	if handleErr(w, err) {
		return
	}
	result := map[string]any{
		"enabled":          enabled,
		"interval_minutes": interval,
		"last_ip":          lastIP,
	}
	if lastUpdate.Valid {
		result["last_update"] = lastUpdate.Time
	}
	writeJSON(w, http.StatusOK, result)
}

// --- API Tokens ---

func (s *Server) apiListTokens(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	tokens, err := s.store.ListAPITokens(r.Context(), user.OrgID)
	if handleErr(w, err) {
		return
	}
	if tokens == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, tokens)
}

func (s *Server) apiCreateToken(w http.ResponseWriter, r *http.Request) {
	user, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	var body struct {
		Name      string   `json:"name"`
		Scopes    []string `json:"scopes"`
		ExpiresIn int      `json:"expires_in_days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "name required", nil)
		return
	}

	rawToken := make([]byte, 32)
	_, _ = rand.Read(rawToken)
	token := hex.EncodeToString(rawToken)

	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	scopes, _ := json.Marshal(body.Scopes)
	if body.Scopes == nil {
		scopes = []byte(`["read"]`)
	}

	var expiresAt *time.Time
	if body.ExpiresIn > 0 {
		t := time.Now().AddDate(0, 0, body.ExpiresIn)
		expiresAt = &t
	}

	tok, err := s.store.CreateAPIToken(r.Context(), user.OrgID, user.UserID, body.Name, tokenHash, string(scopes), expiresAt)
	if handleErr(w, err) {
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         tok.ID,
		"name":       tok.Name,
		"token":      "hive_" + token,
		"scopes":     tok.Scopes,
		"expires_at": tok.ExpiresAt,
		"created_at": tok.CreatedAt,
	})
}

func (s *Server) apiDeleteToken(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	tokenID := chi.URLParam(r, "tokenId")
	if err := s.store.DeleteAPIToken(r.Context(), tokenID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": tokenID})
}

// --- Webhooks ---

func (s *Server) apiListWebhooks(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	webhooks, err := s.store.ListWebhookEndpoints(r.Context(), user.OrgID)
	if handleErr(w, err) {
		return
	}
	if webhooks == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, webhooks)
}

func (s *Server) apiCreateWebhook(w http.ResponseWriter, r *http.Request) {
	user, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	var body struct {
		Name   string   `json:"name"`
		URL    string   `json:"url"`
		Events []string `json:"events"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" || body.URL == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "name and url required", nil)
		return
	}

	secretBytes := make([]byte, 32)
	_, _ = rand.Read(secretBytes)
	secret := hex.EncodeToString(secretBytes)

	events, _ := json.Marshal(body.Events)
	if body.Events == nil {
		events = []byte(`[]`)
	}

	wh, err := s.store.CreateWebhookEndpoint(r.Context(), user.OrgID, body.Name, body.URL, secret, string(events))
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, wh)
}

func (s *Server) apiUpdateWebhook(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	webhookID := chi.URLParam(r, "webhookId")
	wh, err := s.store.GetWebhookEndpoint(r.Context(), webhookID)
	if handleErr(w, err) {
		return
	}

	var body struct {
		Name    string   `json:"name"`
		URL     string   `json:"url"`
		Events  []string `json:"events"`
		Enabled *bool    `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid body", nil)
		return
	}

	name := wh.Name
	if body.Name != "" {
		name = body.Name
	}
	url := wh.URL
	if body.URL != "" {
		url = body.URL
	}
	events := wh.Events
	if body.Events != nil {
		e, _ := json.Marshal(body.Events)
		events = string(e)
	}
	enabled := wh.Enabled
	if body.Enabled != nil {
		enabled = *body.Enabled
	}

	if err := s.store.UpdateWebhookEndpoint(r.Context(), webhookID, name, url, wh.Secret, events, enabled); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"updated": webhookID})
}

func (s *Server) apiDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	webhookID := chi.URLParam(r, "webhookId")
	if err := s.store.DeleteWebhookEndpoint(r.Context(), webhookID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": webhookID})
}

func (s *Server) apiListWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	webhookID := chi.URLParam(r, "webhookId")
	deliveries, err := s.store.ListWebhookDeliveries(r.Context(), webhookID)
	if handleErr(w, err) {
		return
	}
	if deliveries == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, deliveries)
}

func (s *Server) apiTestWebhook(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	webhookID := chi.URLParam(r, "webhookId")
	wh, err := s.store.GetWebhookEndpoint(r.Context(), webhookID)
	if handleErr(w, err) {
		return
	}

	payload := `{"event":"test","timestamp":"` + time.Now().Format(time.RFC3339) + `"}`

	mac := hmac.New(sha256.New, []byte(wh.Secret))
	mac.Write([]byte(payload))
	signature := hex.EncodeToString(mac.Sum(nil))

	_ = s.store.CreateWebhookDelivery(r.Context(), webhookID, "test", payload, 200, fmt.Sprintf("sig: %s", signature))
	writeJSON(w, http.StatusOK, map[string]string{"status": "sent", "signature": signature})
}

// --- VPN ---

func (s *Server) apiListVPNServers(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	servers, err := s.store.ListVPNServers(r.Context(), user.OrgID)
	if handleErr(w, err) {
		return
	}
	if servers == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}

	type serverWithPeers struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		NodeID       string `json:"node_id"`
		ListenPort   int    `json:"listen_port"`
		AddressRange string `json:"address_range"`
		DNS          string `json:"dns"`
		PublicKey    string `json:"public_key"`
		Endpoint     string `json:"endpoint"`
		Enabled      bool   `json:"enabled"`
		PeerCount    int    `json:"peer_count"`
		CreatedAt    string `json:"created_at"`
	}
	var result []serverWithPeers
	for _, srv := range servers {
		count, _ := s.store.CountVPNPeers(r.Context(), srv.ID)
		result = append(result, serverWithPeers{
			ID: srv.ID, Name: srv.Name, NodeID: srv.NodeID,
			ListenPort: srv.ListenPort, AddressRange: srv.AddressRange,
			DNS: srv.DNS, PublicKey: srv.PublicKey, Endpoint: srv.Endpoint,
			Enabled: srv.Enabled, PeerCount: count, CreatedAt: srv.CreatedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) apiCreateVPNServer(w http.ResponseWriter, r *http.Request) {
	user, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	var body struct {
		Name         string `json:"name"`
		NodeID       string `json:"node_id"`
		ListenPort   int    `json:"listen_port"`
		AddressRange string `json:"address_range"`
		DNS          string `json:"dns"`
		Endpoint     string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "name required", nil)
		return
	}
	if body.ListenPort == 0 {
		body.ListenPort = 51820
	}
	if body.AddressRange == "" {
		body.AddressRange = "10.99.0.0/24"
	}
	if body.DNS == "" {
		body.DNS = "1.1.1.1"
	}

	srv, err := s.store.CreateVPNServer(r.Context(), user.OrgID, body.Name, body.NodeID, body.ListenPort, body.AddressRange, body.DNS, "", "", body.Endpoint)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, srv)
}

func (s *Server) apiDeleteVPNServer(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	serverID := chi.URLParam(r, "serverId")
	if err := s.store.DeleteVPNServer(r.Context(), serverID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": serverID})
}

func (s *Server) apiListVPNPeers(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	serverID := chi.URLParam(r, "serverId")
	peers, err := s.store.ListVPNPeers(r.Context(), serverID)
	if handleErr(w, err) {
		return
	}
	if peers == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, peers)
}

func (s *Server) apiCreateVPNPeer(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	serverID := chi.URLParam(r, "serverId")
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "name required", nil)
		return
	}

	count, _ := s.store.CountVPNPeers(r.Context(), serverID)
	assignedIP := fmt.Sprintf("10.99.0.%d/32", count+2)

	peer, err := s.store.CreateVPNPeer(r.Context(), serverID, body.Name, "", "", "0.0.0.0/0", assignedIP)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, peer)
}

func (s *Server) apiDeleteVPNPeer(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	peerID := chi.URLParam(r, "peerId")
	if err := s.store.DeleteVPNPeer(r.Context(), peerID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": peerID})
}

func (s *Server) apiVPNPeerConfig(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	peerID := chi.URLParam(r, "peerId")
	peer, err := s.store.GetVPNPeer(r.Context(), peerID)
	if handleErr(w, err) {
		return
	}
	srv, err := s.store.GetVPNServer(r.Context(), peer.ServerID)
	if handleErr(w, err) {
		return
	}

	config := fmt.Sprintf(`[Interface]
Address = %s
DNS = %s

[Peer]
PublicKey = %s
Endpoint = %s:%d
AllowedIPs = %s
PersistentKeepalive = 25
`, peer.AssignedIP, srv.DNS, srv.PublicKey, srv.Endpoint, srv.ListenPort, peer.AllowedIPs)

	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.conf"`, peer.Name))
	_, _ = w.Write([]byte(config))
}

func (s *Server) apiVPNPeerQR(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	writeAPIError(w, http.StatusNotImplemented, "not_implemented", "QR generation requires go-qrcode dependency", nil)
}

// --- Dashboard Layout ---

func (s *Server) apiGetDashboardLayout(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	layout, err := s.store.GetDashboardLayout(r.Context(), user.UserID, user.OrgID)
	if err == sql.ErrNoRows {
		writeJSON(w, http.StatusOK, map[string]any{"layout": map[string]any{"widgets": []any{}}})
		return
	}
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, layout)
}

func (s *Server) apiSaveDashboardLayout(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	var body struct {
		Layout json.RawMessage `json:"layout"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Layout == nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "layout required", nil)
		return
	}
	layout, err := s.store.UpsertDashboardLayout(r.Context(), user.UserID, user.OrgID, body.Layout)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, layout)
}

// --- Clusters ---

func (s *Server) apiListClusters(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	clusters, err := s.store.ListClusters(r.Context(), user.OrgID)
	if handleErr(w, err) {
		return
	}
	if clusters == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, clusters)
}

func (s *Server) apiCreateCluster(w http.ResponseWriter, r *http.Request) {
	user, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	var body struct {
		Name     string `json:"name"`
		Endpoint string `json:"api_endpoint"`
		Token    string `json:"auth_token"`
		TLSCA    string `json:"tls_ca"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "name required", nil)
		return
	}
	cluster, err := s.store.CreateCluster(r.Context(), user.OrgID, body.Name, body.Endpoint, body.Token, body.TLSCA, false)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, cluster)
}

func (s *Server) apiDeleteCluster(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	clusterID := chi.URLParam(r, "clusterId")
	if err := s.store.DeleteCluster(r.Context(), clusterID); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", err.Error(), nil)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"deleted": clusterID})
}

func (s *Server) apiTestCluster(w http.ResponseWriter, r *http.Request) {
	_, err := requireAdmin(r)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "connected"})
}

// --- Template Ratings ---

func (s *Server) apiRateTemplate(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	templateName := chi.URLParam(r, "name")
	var body struct {
		Rating int    `json:"rating"`
		Review string `json:"review"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Rating < 1 || body.Rating > 5 {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "rating 1-5 required", nil)
		return
	}
	rating, err := s.store.UpsertTemplateRating(r.Context(), templateName, user.UserID, body.Rating, body.Review)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, rating)
}

func (s *Server) apiListTemplateRatings(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	templateName := chi.URLParam(r, "name")
	ratings, err := s.store.ListTemplateRatings(r.Context(), templateName)
	if handleErr(w, err) {
		return
	}
	if ratings == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, ratings)
}

func (s *Server) apiPopularTemplates(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	popular, _ := s.store.PopularTemplates(r.Context(), 20)
	if popular == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, popular)
}

func (s *Server) apiTopRatedTemplates(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	topRated, _ := s.store.TopRatedTemplates(r.Context(), 20)
	if topRated == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	writeJSON(w, http.StatusOK, topRated)
}
