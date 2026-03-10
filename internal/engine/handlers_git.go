package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
	gh "github.com/lholliger/hive/internal/github"
	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/pkg/encryption"
)

func (s *Server) apiCreateGitSource(w http.ResponseWriter, r *http.Request) {
	user, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	var req struct {
		Provider     string `json:"provider"`
		ProviderName string `json:"provider_name"`
		Token        string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}
	if req.Provider == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "provider is required", nil)
		return
	}
	if req.Token == "" {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "token is required", nil)
		return
	}

	tokenEnc, err := encryption.Encrypt([]byte(req.Token))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal_error", "failed to encrypt token", nil)
		return
	}

	gs := &store.GitSource{
		OrgID:          user.OrgID,
		Provider:       req.Provider,
		ProviderName:   req.ProviderName,
		TokenEncrypted: tokenEnc,
	}
	if err := s.store.CreateGitSource(r.Context(), gs); handleErr(w, err) {
		return
	}
	s.auditLog(r, "create", "git_source", gs.ID, "")
	writeJSON(w, http.StatusCreated, gs)
}

func (s *Server) apiListGitSources(w http.ResponseWriter, r *http.Request) {
	user, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	sources, err := s.store.ListGitSources(r.Context(), user.OrgID)
	if handleErr(w, err) {
		return
	}
	if sources == nil {
		sources = []store.GitSource{}
	}
	writeJSON(w, http.StatusOK, sources)
}

func (s *Server) apiGetGitSource(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "sourceId")
	gs, err := s.store.GetGitSource(r.Context(), id)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, gs)
}

func (s *Server) apiDeleteGitSource(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	id := chi.URLParam(r, "sourceId")
	if err := s.store.DeleteGitSource(r.Context(), id); handleErr(w, err) {
		return
	}
	s.auditLog(r, "delete", "git_source", id, "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) gitToken(r *http.Request, sourceID string) (string, *store.GitSource, error) {
	gs, err := s.store.GetGitSource(r.Context(), sourceID)
	if err != nil {
		return "", nil, err
	}

	// For GitHub provider, try GitHub App token first (if configured)
	if gs.Provider == "github" {
		if token, err := s.gitHubAppToken(r); err == nil && token != "" {
			return token, gs, nil
		}
	}

	token, err := encryption.Decrypt(gs.TokenEncrypted)
	if err != nil {
		return "", gs, fmt.Errorf("failed to decrypt token: %w", err)
	}
	return string(token), gs, nil
}

// gitHubAppToken generates an installation token from the configured GitHub App.
func (s *Server) gitHubAppToken(r *http.Request) (string, error) {
	user, err := requireViewer(r)
	if err != nil {
		return "", err
	}
	app, err := s.store.GetGitHubAppByOrg(r.Context(), user.OrgID)
	if err != nil || app.InstallationID == 0 {
		return "", fmt.Errorf("no GitHub App installed")
	}
	pemKey, err := encryption.Decrypt(app.PemEncrypted)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt PEM: %w", err)
	}
	return gh.GenerateInstallationToken(app.AppID, pemKey, app.InstallationID)
}

// GitHubAppTokenForClone generates an installation token for use in git clone.
func GitHubAppTokenForClone(ctx context.Context, db *store.Store) (string, error) {
	apps, err := db.ListGitHubApps(ctx)
	if err != nil || len(apps) == 0 {
		return "", fmt.Errorf("no GitHub App configured")
	}
	app := apps[0]
	if app.InstallationID == 0 {
		return "", fmt.Errorf("GitHub App not installed")
	}
	fullApp, err := db.GetGitHubApp(ctx, app.ID)
	if err != nil {
		return "", err
	}
	pemKey, err := encryption.Decrypt(fullApp.PemEncrypted)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt PEM: %w", err)
	}
	return gh.GenerateInstallationToken(fullApp.AppID, pemKey, fullApp.InstallationID)
}

func (s *Server) apiListGitRepos(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	sourceID := chi.URLParam(r, "sourceId")
	token, gs, err := s.gitToken(r, sourceID)
	if handleErr(w, err) {
		return
	}

	var apiURL string
	switch gs.Provider {
	case "github":
		apiURL = "https://api.github.com/user/repos?per_page=100&sort=updated"
	case "gitlab":
		apiURL = "https://gitlab.com/api/v4/projects?membership=true&per_page=100&order_by=updated_at"
	case "gitea":
		apiURL = fmt.Sprintf("%s/api/v1/user/repos?limit=50", gs.ProviderName)
	default:
		writeAPIError(w, http.StatusBadRequest, "bad_request", "unsupported provider", nil)
		return
	}

	body, status, err := gitAPIRequest(apiURL, token, gs.Provider)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "bad_gateway", err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (s *Server) apiListGitBranches(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	sourceID := chi.URLParam(r, "sourceId")
	repo := chi.URLParam(r, "repo")
	token, gs, err := s.gitToken(r, sourceID)
	if handleErr(w, err) {
		return
	}

	var apiURL string
	switch gs.Provider {
	case "github":
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/branches?per_page=100", repo)
	case "gitlab":
		apiURL = fmt.Sprintf("https://gitlab.com/api/v4/projects/%s/repository/branches?per_page=100", url.PathEscape(repo))
	case "gitea":
		apiURL = fmt.Sprintf("%s/api/v1/repos/%s/branches", gs.ProviderName, repo)
	default:
		writeAPIError(w, http.StatusBadRequest, "bad_request", "unsupported provider", nil)
		return
	}

	body, status, err := gitAPIRequest(apiURL, token, gs.Provider)
	if err != nil {
		writeAPIError(w, http.StatusBadGateway, "bad_gateway", err.Error(), nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (s *Server) apiRegisterGitWebhook(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"webhook_id": "",
		"status":     "not_implemented",
	})
}

func (s *Server) apiDetectBuildType(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}
	sourceID := chi.URLParam(r, "sourceId")
	repo := chi.URLParam(r, "repo")
	branch := r.URL.Query().Get("branch")
	if branch == "" {
		branch = "main"
	}
	token, gs, err := s.gitToken(r, sourceID)
	if handleErr(w, err) {
		return
	}

	var apiURL string
	switch gs.Provider {
	case "github":
		apiURL = fmt.Sprintf("https://api.github.com/repos/%s/contents/?ref=%s", repo, url.QueryEscape(branch))
	case "gitlab":
		apiURL = fmt.Sprintf("https://gitlab.com/api/v4/projects/%s/repository/tree?ref=%s", url.PathEscape(repo), url.QueryEscape(branch))
	default:
		writeJSON(w, http.StatusOK, map[string]string{"build_type": "nixpacks"})
		return
	}

	body, _, err := gitAPIRequest(apiURL, token, gs.Provider)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]string{"build_type": "nixpacks"})
		return
	}

	buildType := "nixpacks"
	bodyStr := string(body)
	if containsFile(bodyStr, "Dockerfile") {
		buildType = "dockerfile"
	} else if containsFile(bodyStr, "docker-compose.yml") || containsFile(bodyStr, "docker-compose.yaml") || containsFile(bodyStr, "compose.yaml") {
		buildType = "compose"
	}

	writeJSON(w, http.StatusOK, map[string]string{"build_type": buildType})
}

func gitAPIRequest(apiURL, token, provider string) ([]byte, int, error) {
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, 0, err
	}
	switch provider {
	case "github":
		req.Header.Set("Authorization", "token "+token)
		req.Header.Set("Accept", "application/vnd.github.v3+json")
	case "gitlab":
		req.Header.Set("PRIVATE-TOKEN", token)
	case "gitea":
		req.Header.Set("Authorization", "token "+token)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return body, resp.StatusCode, nil
}

func containsFile(jsonBody, filename string) bool {
	var items []map[string]interface{}
	if err := json.Unmarshal([]byte(jsonBody), &items); err != nil {
		return false
	}
	for _, item := range items {
		if name, ok := item["name"].(string); ok && name == filename {
			return true
		}
		if path, ok := item["path"].(string); ok && path == filename {
			return true
		}
	}
	return false
}
