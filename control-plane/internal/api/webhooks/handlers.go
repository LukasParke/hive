package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/api/common"
	dbgen "github.com/luke/hive/control-plane/internal/db/generated"
	"github.com/luke/hive/control-plane/internal/jobs/riverjobs"
	"github.com/riverqueue/river"
)

// Handler serves git provider webhook endpoints.
type Handler struct {
	Pool        *pgxpool.Pool
	Q           *dbgen.Queries
	RiverClient *river.Client[pgx.Tx]
}

// NewHandler returns a webhook Handler wired to the given dependencies.
func NewHandler(pool *pgxpool.Pool, riverClient *river.Client[pgx.Tx]) *Handler {
	return &Handler{Pool: pool, Q: dbgen.New(pool), RiverClient: riverClient}
}

// GithubWebhook handles GitHub push and PR events.
func (h *Handler) GithubWebhook(w http.ResponseWriter, r *http.Request) {
	event := r.Header.Get("X-GitHub-Event")
	switch event {
	case "push":
		h.handleWebhook(w, r, "github", true)
	case "pull_request":
		h.handlePRWebhook(w, r, "github")
	default:
		common.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "ignored"})
	}
}

// GitlabWebhook handles GitLab push and MR events.
func (h *Handler) GitlabWebhook(w http.ResponseWriter, r *http.Request) {
	event := r.Header.Get("X-Gitlab-Event")
	switch event {
	case "Push Hook":
		h.handleWebhook(w, r, "gitlab", true)
	case "Merge Request Hook":
		h.handlePRWebhook(w, r, "gitlab")
	default:
		common.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "ignored"})
	}
}

// BitbucketWebhook handles Bitbucket push and PR events.
func (h *Handler) BitbucketWebhook(w http.ResponseWriter, r *http.Request) {
	event := strings.TrimSpace(r.Header.Get("X-Event-Key"))
	switch {
	case strings.EqualFold(event, "repo:push"):
		h.handleWebhook(w, r, "bitbucket", true)
	case strings.HasPrefix(strings.ToLower(event), "pullrequest:"):
		h.handlePRWebhook(w, r, "bitbucket")
	default:
		common.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "ignored"})
	}
}

// GiteaWebhook handles Gitea push and PR events.
func (h *Handler) GiteaWebhook(w http.ResponseWriter, r *http.Request) {
	event := strings.TrimSpace(r.Header.Get("X-Gitea-Event"))
	switch {
	case strings.EqualFold(event, "push"):
		h.handleWebhook(w, r, "gitea", true)
	case strings.EqualFold(event, "pull_request"):
		h.handlePRWebhook(w, r, "gitea")
	default:
		common.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "ignored"})
	}
}

func (h *Handler) handleWebhook(w http.ResponseWriter, r *http.Request, providerType string, shouldDeploy bool) {
	if !shouldDeploy {
		common.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "ignored"})
		return
	}
	rawBody, err := ioReadAllLimit(r.Body, 2<<20)
	if err != nil {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	secretsRows, err := h.Pool.Query(r.Context(), `
		select webhook_secret from git_providers
		where type = $1 and enabled = true
	`, providerType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer secretsRows.Close()
	var secrets []string
	for secretsRows.Next() {
		var secret string
		if scanErr := secretsRows.Scan(&secret); scanErr == nil && secret != "" {
			secrets = append(secrets, secret)
		}
	}
	if len(secrets) == 0 || !verifyWebhook(providerType, rawBody, r.Header, secrets) {
		http.Error(w, `{"message":"invalid webhook signature"}`, http.StatusUnauthorized)
		return
	}

	var payload struct {
		Repository struct {
			CloneURL string `json:"clone_url"`
			GitURL   string `json:"git_url"`
			HTTPURL  string `json:"http_url"`
		} `json:"repository"`
		Ref     string `json:"ref"`
		Commits []struct {
			Modified []string `json:"modified"`
		} `json:"commits"`
	}
	if err := json.NewDecoder(bytes.NewReader(rawBody)).Decode(&payload); err != nil {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	repo := payload.Repository.CloneURL
	if repo == "" {
		repo = payload.Repository.HTTPURL
	}
	if repo == "" {
		repo = payload.Repository.GitURL
	}
	if repo == "" {
		http.Error(w, `{"message":"missing repository URL"}`, http.StatusBadRequest)
		return
	}
	branch := strings.TrimPrefix(payload.Ref, "refs/heads/")
	modifiedFiles := make([]string, 0, 8)
	for _, c := range payload.Commits {
		modifiedFiles = append(modifiedFiles, c.Modified...)
	}
	rows, err := h.Pool.Query(r.Context(), `
		select a.id::text, coalesce(a.git_ref, 'main'), coalesce(a.watch_paths, '{}'::text[])
		from applications a
		join git_providers gp on gp.type = $1 and gp.enabled = true
		where a.repository_url = $2 and a.auto_deploy = true
	`, providerType, repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var appID string
		var appBranch string
		var watchPaths []string
		if err := rows.Scan(&appID, &appBranch, &watchPaths); err != nil {
			continue
		}
		if appBranch != "" && branch != "" && appBranch != branch {
			continue
		}
		if !matchesWatchPaths(watchPaths, modifiedFiles) {
			continue
		}
		if _, err := riverjobs.EnqueueBuild(r.Context(), h.RiverClient, h.Pool, appID, "webhook", ""); err == nil {
			count++
		}
	}
	common.WriteJSON(w, http.StatusAccepted, map[string]any{"status": "accepted", "queued": count})
}

type prPayload struct {
	Action      string `json:"action"`
	Number      int32  `json:"number"`
	PullRequest struct {
		Head struct {
			Ref string `json:"ref"`
			Sha string `json:"sha"`
		} `json:"head"`
	} `json:"pull_request"`
	ObjectAttributes struct {
		SourceBranch string `json:"source_branch"`
		LastCommit   struct {
			ID string `json:"id"`
		} `json:"last_commit"`
		Iid    int32  `json:"iid"`
		State  string `json:"state"`
		Action string `json:"action"`
	} `json:"object_attributes"`
	Repository struct {
		CloneURL string `json:"clone_url"`
		HTTPURL  string `json:"http_url"`
		GitURL   string `json:"git_url"`
		Links    struct {
			HTML struct {
				Href string `json:"href"`
			} `json:"html"`
		} `json:"links"`
	} `json:"repository"`
}

func (h *Handler) handlePRWebhook(w http.ResponseWriter, r *http.Request, providerType string) {
	rawBody, err := ioReadAllLimit(r.Body, 2<<20)
	if err != nil {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	secretsRows, err := h.Pool.Query(r.Context(), `
		select webhook_secret from git_providers
		where type = $1 and enabled = true
	`, providerType)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer secretsRows.Close()
	var secrets []string
	for secretsRows.Next() {
		var secret string
		if scanErr := secretsRows.Scan(&secret); scanErr == nil && secret != "" {
			secrets = append(secrets, secret)
		}
	}
	if len(secrets) == 0 || !verifyWebhook(providerType, rawBody, r.Header, secrets) {
		http.Error(w, `{"message":"invalid webhook signature"}`, http.StatusUnauthorized)
		return
	}

	var payload prPayload
	if err := json.NewDecoder(bytes.NewReader(rawBody)).Decode(&payload); err != nil {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}

	action := strings.ToLower(payload.Action)
	if action == "" {
		action = strings.ToLower(payload.ObjectAttributes.Action)
	}

	repo := payload.Repository.CloneURL
	if repo == "" {
		repo = payload.Repository.HTTPURL
	}
	if repo == "" {
		repo = payload.Repository.GitURL
	}
	if repo == "" {
		repo = payload.Repository.Links.HTML.Href
	}
	if repo == "" {
		http.Error(w, `{"message":"missing repository URL"}`, http.StatusBadRequest)
		return
	}

	prNumber := payload.Number
	if prNumber == 0 {
		prNumber = payload.ObjectAttributes.Iid
	}
	branch := payload.PullRequest.Head.Ref
	if branch == "" {
		branch = payload.ObjectAttributes.SourceBranch
	}
	commitSha := payload.PullRequest.Head.Sha
	if commitSha == "" {
		commitSha = payload.ObjectAttributes.LastCommit.ID
	}

	switch action {
	case "opened", "synchronize", "reopened", "open", "update", "created", "updated":
		h.handlePRCreated(r.Context(), w, providerType, repo, prNumber, branch, commitSha)
	case "closed", "merged", "declined", "rejected", "fulfilled", "close", "merge":
		h.handlePRClosed(r.Context(), w, providerType, repo, prNumber)
	default:
		common.WriteJSON(w, http.StatusAccepted, map[string]string{"status": "ignored"})
	}
}

func (h *Handler) handlePRCreated(ctx context.Context, w http.ResponseWriter, providerType, repo string, prNumber int32, branch, commitSha string) {
	rows, err := h.Pool.Query(ctx, `
		select a.id::text, p.organization_id::text
		from applications a
		join projects p on p.id = a.project_id
		join git_providers gp on gp.type = $1 and gp.enabled = true
		where a.repository_url = $2
	`, providerType, repo)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var appID, orgID string
		if err := rows.Scan(&appID, &orgID); err != nil {
			continue
		}
		appUUID, _ := common.ToUUID(appID)
		orgUUID, _ := common.ToUUID(orgID)
		previewID, err := h.Q.CreatePreviewDeployment(ctx, dbgen.CreatePreviewDeploymentParams{
			OrganizationID: orgUUID,
			ApplicationID:  appUUID,
			PrNumber:       prNumber,
			Branch:         branch,
			CommitSha:      pgtype.Text{String: commitSha, Valid: commitSha != ""},
			Status:         "building",
			Url:            pgtype.Text{String: "", Valid: false},
		})
		if err != nil {
			continue
		}
		if h.RiverClient != nil {
			_, _ = h.RiverClient.Insert(ctx, riverjobs.PreviewDeployJobArgs{
				PreviewID:     previewID,
				ApplicationID: appID,
				Branch:        branch,
				CommitSha:     commitSha,
			}, nil)
		}
		count++
	}
	common.WriteJSON(w, http.StatusAccepted, map[string]any{"status": "accepted", "previews": count})
}

func (h *Handler) handlePRClosed(ctx context.Context, w http.ResponseWriter, providerType, repo string, prNumber int32) {
	rows, err := h.Pool.Query(ctx, `
		select pd.id::text, pd.application_id::text, p.organization_id::text
		from preview_deployments pd
		join applications a on a.id = pd.application_id
		join projects p on p.id = a.project_id
		join git_providers gp on gp.type = $1 and gp.enabled = true
		where a.repository_url = $2 and pd.pr_number = $3 and pd.status != 'expired'
	`, providerType, repo, prNumber)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var previewID, appID, orgID string
		if err := rows.Scan(&previewID, &appID, &orgID); err != nil {
			continue
		}
		previewUUID, _ := common.ToUUID(previewID)
		appUUID, _ := common.ToUUID(appID)
		orgUUID, _ := common.ToUUID(orgID)
		_ = h.Q.DeletePreviewDeployment(ctx, dbgen.DeletePreviewDeploymentParams{
			ID:             previewUUID,
			ApplicationID:  appUUID,
			OrganizationID: orgUUID,
		})
		count++
	}
	common.WriteJSON(w, http.StatusAccepted, map[string]any{"status": "accepted", "removed": count})
}

func ioReadAllLimit(r io.Reader, n int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, n))
}

func verifyWebhook(providerType string, body []byte, headers http.Header, secrets []string) bool {
	switch providerType {
	case "github":
		sig := strings.TrimSpace(headers.Get("X-Hub-Signature-256"))
		if !strings.HasPrefix(sig, "sha256=") {
			return false
		}
		given := strings.TrimPrefix(sig, "sha256=")
		for _, secret := range secrets {
			mac := hmac.New(sha256.New, []byte(secret))
			_, _ = mac.Write(body)
			expected := hex.EncodeToString(mac.Sum(nil))
			if hmac.Equal([]byte(expected), []byte(given)) {
				return true
			}
		}
		return false
	case "gitlab":
		token := strings.TrimSpace(headers.Get("X-Gitlab-Token"))
		if token == "" {
			return false
		}
		for _, secret := range secrets {
			if hmac.Equal([]byte(secret), []byte(token)) {
				return true
			}
		}
		return false
	case "bitbucket":
		sig := strings.TrimSpace(headers.Get("X-Hub-Signature"))
		sig = strings.TrimPrefix(sig, "sha256=")
		if sig == "" {
			return false
		}
		for _, secret := range secrets {
			mac := hmac.New(sha256.New, []byte(secret))
			_, _ = mac.Write(body)
			expected := hex.EncodeToString(mac.Sum(nil))
			if hmac.Equal([]byte(expected), []byte(sig)) {
				return true
			}
		}
		return false
	case "gitea":
		sig := strings.TrimSpace(headers.Get("X-Gitea-Signature"))
		if sig == "" {
			return false
		}
		for _, secret := range secrets {
			mac := hmac.New(sha256.New, []byte(secret))
			_, _ = mac.Write(body)
			expected := hex.EncodeToString(mac.Sum(nil))
			if hmac.Equal([]byte(expected), []byte(sig)) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func matchesWatchPaths(patterns, files []string) bool {
	if len(patterns) == 0 {
		return true
	}
	if len(files) == 0 {
		return false
	}
	for _, f := range files {
		for _, p := range patterns {
			if ok, _ := path.Match(p, f); ok {
				return true
			}
			if strings.HasPrefix(f, strings.TrimSuffix(p, "*")) {
				return true
			}
		}
	}
	return false
}
