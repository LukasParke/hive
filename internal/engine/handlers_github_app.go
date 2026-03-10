package engine

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/pkg/encryption"
)

func (s *Server) apiGitHubAppStatus(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}

	app, err := s.store.GetGitHubAppByOrg(r.Context(), user.OrgID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"configured": false,
			"slug":       "",
			"installed":  false,
			"html_url":   "",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"configured":      true,
		"slug":            app.AppSlug,
		"installed":       app.InstallationID > 0,
		"installation_id": app.InstallationID,
		"html_url":        app.HTMLURL,
		"app_id":          app.AppID,
	})
}

func (s *Server) apiGitHubAppManifest(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}

	baseURL := s.cfg.WebhookBaseURL
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	manifest := map[string]interface{}{
		"name":         "Hive",
		"url":          baseURL,
		"public":       false,
		"redirect_url": baseURL + "/git?setup=complete",
		"hook_attributes": map[string]string{
			"url": baseURL + "/api/v1/webhooks/github",
		},
		"callback_urls": []string{
			baseURL + "/git",
		},
		"setup_url": baseURL + "/git?setup=install",
		"default_permissions": map[string]string{
			"contents":      "read",
			"metadata":      "read",
			"pull_requests": "read",
		},
		"default_events": []string{
			"push",
			"pull_request",
		},
	}

	manifestJSON, _ := json.Marshal(manifest)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"manifest":     string(manifestJSON),
		"redirect_url": "https://github.com/settings/apps/new",
	})
}

func (s *Server) apiGitHubAppComplete(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Code == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "code is required", nil)
		return
	}

	convURL := fmt.Sprintf("https://api.github.com/app-manifests/%s/conversions", req.Code)
	convReq, _ := http.NewRequestWithContext(r.Context(), "POST", convURL, nil)
	convReq.Header.Set("Accept", "application/vnd.github+json")

	resp, err := http.DefaultClient.Do(convReq)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "bad_gateway", "failed to contact GitHub: "+err.Error(), nil)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode != 201 {
		writeJSON(w, resp.StatusCode, map[string]string{"error": "GitHub returned: " + string(body)})
		return
	}

	var ghResp struct {
		ID            int    `json:"id"`
		Slug          string `json:"slug"`
		PEM           string `json:"pem"`
		WebhookSecret string `json:"webhook_secret"`
		ClientID      string `json:"client_id"`
		ClientSecret  string `json:"client_secret"`
		HTMLURL       string `json:"html_url"`
	}
	if err := json.Unmarshal(body, &ghResp); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "failed to parse GitHub response", nil)
		return
	}

	pemEnc, err := encryption.Encrypt([]byte(ghResp.PEM))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "failed to encrypt PEM", nil)
		return
	}
	secretEnc, err := encryption.Encrypt([]byte(ghResp.ClientSecret))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "failed to encrypt client secret", nil)
		return
	}

	app := &store.GitHubApp{
		OrgID:                 user.OrgID,
		AppID:                 ghResp.ID,
		AppSlug:               ghResp.Slug,
		PemEncrypted:          pemEnc,
		WebhookSecret:         ghResp.WebhookSecret,
		ClientID:              ghResp.ClientID,
		ClientSecretEncrypted: secretEnc,
		HTMLURL:               ghResp.HTMLURL,
	}

	if err := s.store.CreateGitHubApp(r.Context(), app); handleErr(w, err) {
		return
	}

	s.auditLog(r, "create", "github_app", app.ID, "")
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"id":       app.ID,
		"app_id":   app.AppID,
		"slug":     app.AppSlug,
		"html_url": app.HTMLURL,
	})
}

func (s *Server) apiGitHubAppInstallation(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}

	var req struct {
		InstallationID int64 `json:"installation_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.InstallationID == 0 {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "installation_id is required", nil)
		return
	}

	app, err := s.store.GetGitHubAppByOrg(r.Context(), user.OrgID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "no GitHub App configured", nil)
		return
	}

	if err := s.store.UpdateGitHubAppInstallation(r.Context(), app.ID, req.InstallationID); handleErr(w, err) {
		return
	}

	s.auditLog(r, "update", "github_app", app.ID, "installation")
	writeJSON(w, http.StatusOK, map[string]string{"status": "installed"})
}

func (s *Server) apiGitHubAppDelete(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}

	app, err := s.store.GetGitHubAppByOrg(r.Context(), user.OrgID)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found", "no GitHub App configured", nil)
		return
	}

	if err := s.store.DeleteGitHubApp(r.Context(), app.ID); handleErr(w, err) {
		return
	}

	s.auditLog(r, "delete", "github_app", app.ID, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// apiGitHubWebhook handles incoming GitHub webhook payloads (push events).
// This is a public endpoint -- no session auth, verified via webhook signature.
func (s *Server) apiGitHubWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 5<<20))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "failed to read body", nil)
		return
	}

	sig := r.Header.Get("X-Hub-Signature-256")
	event := r.Header.Get("X-GitHub-Event")

	if event == "ping" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "pong"})
		return
	}
	if event != "push" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored", "event": event})
		return
	}

	// Parse push payload to get app ID for signature verification
	var payload struct {
		Ref        string `json:"ref"`
		Repository struct {
			FullName string `json:"full_name"`
			CloneURL string `json:"clone_url"`
		} `json:"repository"`
		Installation struct {
			ID    int64 `json:"id"`
			AppID int   `json:"app_id"`
		} `json:"installation"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid payload", nil)
		return
	}

	// Find the GitHub App to verify the signature
	apps, err := s.store.ListGitHubApps(r.Context())
	if err != nil || len(apps) == 0 {
		writeJSON(w, http.StatusOK, map[string]string{"status": "no_app_configured"})
		return
	}

	verified := false
	for _, app := range apps {
		if app.WebhookSecret == "" {
			continue
		}
		if verifyWebhookSignature(body, sig, app.WebhookSecret) {
			verified = true
			break
		}
	}

	if !verified && sig != "" {
		writeAPIError(w, http.StatusForbidden, "forbidden", "invalid signature", nil)
		return
	}

	branch := ""
	if strings.HasPrefix(payload.Ref, "refs/heads/") {
		branch = strings.TrimPrefix(payload.Ref, "refs/heads/")
	}
	if branch == "" {
		writeJSON(w, http.StatusOK, map[string]string{"status": "not_a_branch_push"})
		return
	}

	repo := payload.Repository.FullName
	cloneURL := payload.Repository.CloneURL
	s.log.Infof("github webhook: push to %s branch %s", repo, branch)

	// Find apps that match this repo + branch
	allApps, err := s.store.ListAllApps(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "failed to list apps", nil)
		return
	}

	triggered := 0
	for _, a := range allApps {
		if a.DeployType != "git" {
			continue
		}
		appRepo := normalizeGitRepo(a.GitRepo)
		if appRepo != repo && appRepo != cloneURL {
			continue
		}
		appBranch := a.GitBranch
		if appBranch == "" {
			appBranch = "main"
		}
		if appBranch != branch {
			continue
		}

		// Trigger build
		s.log.Infof("github webhook: triggering build for app %s (%s)", a.Name, a.ID)
		if s.nc != nil {
			jobPayload, _ := json.Marshal(map[string]string{
				"action":      "build",
				"app_id":      a.ID,
				"name":        a.Name,
				"deploy_type": a.DeployType,
				"git_repo":    a.GitRepo,
				"git_branch":  a.GitBranch,
				"dockerfile":  a.DockerfilePath,
			})
			_ = s.nc.Publish("hive.build", jobPayload)
			triggered++
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"status":    "processed",
		"repo":      repo,
		"branch":    branch,
		"triggered": triggered,
	})
}

func verifyWebhookSignature(payload []byte, signature, secret string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	sig, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := mac.Sum(nil)
	return hmac.Equal(sig, expected)
}

func normalizeGitRepo(repo string) string {
	repo = strings.TrimSuffix(repo, ".git")
	repo = strings.TrimPrefix(repo, "https://github.com/")
	repo = strings.TrimPrefix(repo, "http://github.com/")
	repo = strings.TrimPrefix(repo, "git@github.com:")
	return repo
}
