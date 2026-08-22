package webhooks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/testdb"
)

const webhookSecret = "whsec_test_123" //nolint:gosec // test fixture

func newTestHandler(t *testing.T) *Handler {
	t.Helper()
	testdb.Get(t)
	testdb.TruncateAll(t)
	return NewHandler(testdb.Get(t), testdb.RiverClient(t))
}

func newWebhookRouter(t *testing.T, h *Handler) http.Handler {
	t.Helper()
	r := chi.NewRouter()
	r.Post("/webhooks/github", h.GithubWebhook)
	r.Post("/webhooks/gitlab", h.GitlabWebhook)
	r.Post("/webhooks/bitbucket", h.BitbucketWebhook)
	r.Post("/webhooks/gitea", h.GiteaWebhook)
	return r
}

func githubSig(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func pushPayload(repo, ref string, modified ...string) []byte {
	commits := []map[string]any{}
	if len(modified) > 0 {
		commits = append(commits, map[string]any{"modified": modified})
	}
	body, _ := json.Marshal(map[string]any{
		"repository": map[string]any{"clone_url": repo},
		"ref":        ref,
		"commits":    commits,
	})
	return body
}

// providerCase describes how each provider's endpoint is driven.
type providerCase struct {
	provider   string
	path       string
	pushHeader string
	sigPrefix  string // "" plain token (gitlab); "BARE" hex digest (gitea); else prefixed digest
	pushValue  string
	sigHeader  string
}

var providerCases = []providerCase{
	{provider: "github", path: "/webhooks/github", pushHeader: "X-GitHub-Event", pushValue: "push", sigHeader: "X-Hub-Signature-256", sigPrefix: "sha256="},
	{provider: "gitlab", path: "/webhooks/gitlab", pushHeader: "X-Gitlab-Event", pushValue: "Push Hook", sigHeader: "X-Gitlab-Token"},
	{provider: "bitbucket", path: "/webhooks/bitbucket", pushHeader: "X-Event-Key", pushValue: "repo:push", sigHeader: "X-Hub-Signature", sigPrefix: "sha256="},
	{provider: "gitea", path: "/webhooks/gitea", pushHeader: "X-Gitea-Event", pushValue: "push", sigHeader: "X-Gitea-Signature", sigPrefix: "BARE"},
}

func signFor(pc providerCase, secret string, body []byte) string {
	switch pc.sigPrefix {
	case "":
		return secret // plain token comparison (GitLab)
	case "BARE":
		return strings.TrimPrefix(githubSig(secret, body), "sha256=")
	default:
		return githubSig(secret, body)
	}
}

func TestVerifyWebhook(t *testing.T) {
	body := []byte(`{"ok":true}`)
	valid := githubSig(webhookSecret, body)

	tests := []struct {
		name     string
		provider string
		headers  map[string]string
		want     bool
	}{
		{name: "github valid", provider: "github", headers: map[string]string{"X-Hub-Signature-256": valid}, want: true},
		{name: "github tampered body", provider: "github", headers: map[string]string{"X-Hub-Signature-256": githubSig(webhookSecret, []byte(`{"ok":false}`))}, want: false},
		{name: "github wrong prefix", provider: "github", headers: map[string]string{"X-Hub-Signature-256": "sha1=abc"}, want: false},
		{name: "github missing header", provider: "github", want: false},
		{name: "gitlab token match", provider: "gitlab", headers: map[string]string{"X-Gitlab-Token": webhookSecret}, want: true},
		{name: "gitlab token mismatch", provider: "gitlab", headers: map[string]string{"X-Gitlab-Token": "nope"}, want: false},
		{name: "gitlab missing token", provider: "gitlab", want: false},
		{name: "bitbucket valid", provider: "bitbucket", headers: map[string]string{"X-Hub-Signature": valid}, want: true},
		{name: "bitbucket signature mismatch", provider: "bitbucket", headers: map[string]string{"X-Hub-Signature": githubSig("other-secret", body)}, want: false},
		{name: "bitbucket missing", provider: "bitbucket", want: false},
		{name: "gitea valid", provider: "gitea", headers: map[string]string{"X-Gitea-Signature": strings.TrimPrefix(valid, "sha256=")}, want: true},
		{name: "gitea signature mismatch", provider: "gitea", headers: map[string]string{"X-Gitea-Signature": strings.Repeat("0", 64)}, want: false},
		{name: "gitea missing", provider: "gitea", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := http.Header{}
			for k, v := range tt.headers {
				h.Set(k, v)
			}
			if got := verifyWebhook(tt.provider, body, h, []string{webhookSecret}); got != tt.want {
				t.Fatalf("verifyWebhook(%s) = %v, want %v", tt.provider, got, tt.want)
			}
		})
	}

	t.Run("multiple secrets tried in order", func(t *testing.T) {
		h := http.Header{}
		h.Set("X-Hub-Signature-256", valid)
		if !verifyWebhook("github", body, h, []string{"first-secret", webhookSecret}) {
			t.Fatal("second secret should verify")
		}
	})

	t.Run("unknown provider rejected", func(t *testing.T) {
		if verifyWebhook("azure", body, http.Header{}, []string{webhookSecret}) {
			t.Fatal("unknown provider must fail verification")
		}
	})

	t.Run("empty secrets list rejected", func(t *testing.T) {
		h := http.Header{}
		h.Set("X-Hub-Signature-256", valid)
		if verifyWebhook("github", body, h, nil) {
			t.Fatal("empty secrets must fail verification")
		}
	})
}

func TestMatchesWatchPathsEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		files    []string
		want     bool
	}{
		{name: "patterns but no files", patterns: []string{"ui/**"}, files: nil, want: false},
		{name: "exact glob segment match", patterns: []string{"apps/api/**"}, files: []string{"apps/api/main.go"}, want: true},
		{name: "prefix match without glob", patterns: []string{"docs"}, files: []string{"docs/readme.md"}, want: true},
		{name: "glob no match", patterns: []string{"*.md"}, files: []string{"main.go"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchesWatchPaths(tt.patterns, tt.files); got != tt.want {
				t.Fatalf("matchesWatchPaths(%v,%v)=%v want %v", tt.patterns, tt.files, got, tt.want)
			}
		})
	}
}

func TestPushEventEnqueuesBuildPerProvider(t *testing.T) {
	for _, pc := range providerCases {
		t.Run(pc.provider, func(t *testing.T) {
			h := newTestHandler(t)
			router := newWebhookRouter(t, h)
			testdb.SeedGitProvider(t, pc.provider, webhookSecret)
			org := testdb.SeedOrg(t)
			appID := testdb.SeedApplication(t, org.ProjectID, "", "https://github.com/acme/api.git", nil)

			body := pushPayload("https://github.com/acme/api.git", "refs/heads/main")
			req := httptest.NewRequest(http.MethodPost, pc.path, strings.NewReader(string(body)))
			req.Header.Set(pc.pushHeader, pc.pushValue)
			req.Header.Set(pc.sigHeader, signFor(pc, webhookSecret, body))
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusAccepted {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			var resp map[string]any
			_ = json.Unmarshal(rec.Body.Bytes(), &resp)
			if resp["queued"] != float64(1) {
				t.Fatalf("queued = %v, want 1 (%v)", resp["queued"], resp)
			}
			n := testdb.QueryCount(t,
				`select count(*) from build_jobs where application_id=$1::uuid and trigger='webhook' and status='queued'`, appID)
			if n != 1 {
				t.Fatalf("build_jobs rows = %d, want 1", n)
			}
			n = testdb.QueryCount(t, `select count(*) from river_job where state='available'`)
			if n != 1 {
				t.Fatalf("river_job rows = %d, want 1", n)
			}
		})
	}
}

func TestPushEventBranchAndWatchPathFiltering(t *testing.T) {
	h := newTestHandler(t)
	router := newWebhookRouter(t, h)
	testdb.SeedGitProvider(t, "github", webhookSecret)
	org := testdb.SeedOrg(t)

	mainApp := testdb.SeedApplication(t, org.ProjectID, "main-app", "https://github.com/acme/api.git", nil)
	devApp := testdb.SeedApplicationWithRef(t, org.ProjectID, "dev-app", "https://github.com/acme/api.git", "develop")
	uiApp := testdb.SeedApplicationWatchPaths(t, org.ProjectID, "ui-app", "https://github.com/acme/api.git", []string{"ui*"})
	noAutoDeploy := testdb.SeedApplicationNoAutoDeploy(t, org.ProjectID, "manual-app", "https://github.com/acme/api.git")

	post := func(body []byte) map[string]any {
		req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(string(body)))
		req.Header.Set("X-GitHub-Event", "push")
		req.Header.Set("X-Hub-Signature-256", githubSig(webhookSecret, body))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return resp
	}

	completeBuilds := func() {
		t.Helper()
		if _, err := testdb.Get(t).Exec(context.Background(), `update build_jobs set status='complete' where status='queued'`); err != nil {
			t.Fatalf("complete builds: %v", err)
		}
	}
	totalFor := func(appID string) int {
		t.Helper()
		return testdb.QueryCount(t, `select count(*) from build_jobs where application_id=$1::uuid`, appID)
	}

	resp := post(pushPayload("https://github.com/acme/api.git", "refs/heads/main"))
	if resp["queued"] != float64(1) {
		t.Fatalf("main push queued = %v, want 1 (mainApp only)", resp["queued"])
	}
	if got := totalFor(mainApp); got != 1 {
		t.Fatalf("main-app build rows = %d, want 1", got)
	}
	completeBuilds()

	resp = post(pushPayload("https://github.com/acme/api.git", "refs/heads/develop"))
	if resp["queued"] != float64(1) {
		t.Fatalf("develop push queued = %v, want 1 (devApp only)", resp["queued"])
	}
	if got := totalFor(devApp); got != 1 {
		t.Fatalf("dev-app build rows = %d, want 1", got)
	}
	completeBuilds()

	resp = post(pushPayload("https://github.com/acme/api.git", "refs/heads/main", "ui/src/app.tsx"))
	if resp["queued"] != float64(2) {
		t.Fatalf("watch-path push queued = %v, want 2 (mainApp + uiApp)", resp["queued"])
	}
	if got := totalFor(uiApp); got != 1 {
		t.Fatalf("ui-app build rows = %d, want 1", got)
	}
	completeBuilds()

	resp = post(pushPayload("https://github.com/acme/api.git", "refs/heads/main", "unrelated/x.go"))
	if resp["queued"] != float64(1) {
		t.Fatalf("filtered push queued = %v, want 1 (only mainApp)", resp["queued"])
	}
	if got := totalFor(noAutoDeploy); got != 0 {
		t.Fatalf("auto_deploy=false app must never be queued, got %d builds", got)
	}
	if got := testdb.QueryCount(t, `select count(*) from build_jobs where trigger='webhook'`); got != 5 {
		t.Fatalf("total webhook builds = %d, want 5", got)
	}
}

func TestPushEventDuplicateDeliveryNotDoubleQueued(t *testing.T) {
	h := newTestHandler(t)
	router := newWebhookRouter(t, h)
	testdb.SeedGitProvider(t, "github", webhookSecret)
	org := testdb.SeedOrg(t)
	testdb.SeedApplication(t, org.ProjectID, "", "https://github.com/acme/api.git", nil)

	deliver := func() map[string]any {
		body := pushPayload("https://github.com/acme/api.git", "refs/heads/main")
		req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(string(body)))
		req.Header.Set("X-GitHub-Event", "push")
		req.Header.Set("X-Hub-Signature-256", githubSig(webhookSecret, body))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		var resp map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return resp
	}

	first := deliver()
	if first["queued"] != float64(1) {
		t.Fatalf("first delivery queued = %v, want 1", first["queued"])
	}
	second := deliver()
	if second["queued"] != float64(0) {
		t.Fatalf("duplicate delivery queued = %v, want 0 (active-build unique index)", second["queued"])
	}
}

func TestPushEventErrorBranches(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T) http.Handler
		event      string
		sigHeader  string
		signature  string
		body       string
		wantStatus int
	}{
		{
			name: "ignored event accepted",
			setup: func(t *testing.T) http.Handler {
				h := newTestHandler(t)
				return newWebhookRouter(t, h)
			},
			event: "ping", signature: "", body: "{}",
			wantStatus: http.StatusAccepted,
		},
		{
			name: "no configured secret -> unauthorized",
			setup: func(t *testing.T) http.Handler {
				h := newTestHandler(t)
				return newWebhookRouter(t, h)
			},
			event: "push", signature: "sha256=deadbeef", body: "{}",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "bad signature -> unauthorized",
			setup: func(t *testing.T) http.Handler {
				h := newTestHandler(t)
				testdb.SeedGitProvider(t, "github", webhookSecret)
				return newWebhookRouter(t, h)
			},
			event: "push", signature: "sha256=0000000000000000000000000000000000000000000000000000000000000000", body: "{}",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "valid signature invalid json -> bad request",
			setup: func(t *testing.T) http.Handler {
				h := newTestHandler(t)
				testdb.SeedGitProvider(t, "github", webhookSecret)
				return newWebhookRouter(t, h)
			},
			event: "push", signature: "SIGN_ME", body: "{not-json",
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "missing repository url -> bad request",
			setup: func(t *testing.T) http.Handler {
				h := newTestHandler(t)
				testdb.SeedGitProvider(t, "github", webhookSecret)
				return newWebhookRouter(t, h)
			},
			event: "push", signature: "SIGN_ME", body: `{"repository":{"clone_url":""},"ref":"refs/heads/main"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "unknown repository queues nothing",
			setup: func(t *testing.T) http.Handler {
				h := newTestHandler(t)
				testdb.SeedGitProvider(t, "github", webhookSecret)
				org := testdb.SeedOrg(t)
				testdb.SeedApplication(t, org.ProjectID, "", "https://github.com/acme/other.git", nil)
				return newWebhookRouter(t, h)
			},
			event: "push", signature: "SIGN_ME",
			body:       string(pushPayload("https://github.com/acme/unknown.git", "refs/heads/main")),
			wantStatus: http.StatusAccepted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := tt.setup(t)
			body := tt.body
			if tt.signature == "SIGN_ME" {
				tt.signature = githubSig(webhookSecret, []byte(body))
			}
			req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(body))
			req.Header.Set("X-GitHub-Event", tt.event)
			if tt.signature != "" {
				req.Header.Set("X-Hub-Signature-256", tt.signature)
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantStatus == http.StatusAccepted && tt.event == "push" {
				var resp map[string]any
				_ = json.Unmarshal(rec.Body.Bytes(), &resp)
				if _, ok := resp["queued"]; !ok {
					t.Fatalf("expected queued field in response: %s", rec.Body.String())
				}
			}
		})
	}

	t.Run("fallback clone url resolution uses http_url then git_url", func(t *testing.T) {
		h := newTestHandler(t)
		router := newWebhookRouter(t, h)
		testdb.SeedGitProvider(t, "github", webhookSecret)
		org := testdb.SeedOrg(t)
		appHTTP := testdb.SeedApplication(t, org.ProjectID, "", "https://github.com/acme/http-url.git", nil)
		appGit := testdb.SeedApplication(t, org.ProjectID, "", "git@github.com:acme/git-url.git", nil)

		deliver := func(payload map[string]any) int {
			body, _ := json.Marshal(payload)
			req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(string(body)))
			req.Header.Set("X-GitHub-Event", "push")
			req.Header.Set("X-Hub-Signature-256", githubSig(webhookSecret, body))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			return rec.Code
		}
		if code := deliver(map[string]any{
			"repository": map[string]any{"http_url": "https://github.com/acme/http-url.git"},
			"ref":        "refs/heads/main",
		}); code != http.StatusAccepted {
			t.Fatalf("http_url fallback status = %d", code)
		}
		if n := testdb.QueryCount(t, `select count(*) from build_jobs where application_id=$1::uuid`, appHTTP); n != 1 {
			t.Fatalf("http_url app builds = %d, want 1", n)
		}

		if code := deliver(map[string]any{
			"repository": map[string]any{"git_url": "git@github.com:acme/git-url.git"},
			"ref":        "refs/heads/main",
		}); code != http.StatusAccepted {
			t.Fatalf("git_url fallback status = %d", code)
		}
		if n := testdb.QueryCount(t, `select count(*) from build_jobs where application_id=$1::uuid`, appGit); n != 1 {
			t.Fatalf("git_url app builds = %d, want 1", n)
		}
	})
}

func TestPRClosedRemovesPreviewDeployment(t *testing.T) {
	h := newTestHandler(t)
	router := newWebhookRouter(t, h)
	testdb.SeedGitProvider(t, "github", webhookSecret)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "", "https://github.com/acme/web.git", nil)

	if _, err := testdb.Get(t).Exec(context.Background(), `
		insert into preview_deployments(organization_id, application_id, pr_number, branch, status)
		values ($1::uuid, $2::uuid, 7, 'feature', 'ready')
	`, org.OrgID, appID); err != nil {
		t.Fatalf("seed preview: %v", err)
	}

	rec := deliverPR(t, router, "/webhooks/github", providerCases[0], "closed", 7, "https://github.com/acme/web.git", webhookSecret)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if n := testdb.QueryCount(t, `select count(*) from preview_deployments where application_id=$1::uuid and pr_number=7`, appID); n != 0 {
		t.Fatalf("preview rows after close = %d, want 0", n)
	}
}

func prBody(action string, number int, repo string, branch, sha string) []byte {
	body, _ := json.Marshal(map[string]any{
		"action": action,
		"number": number,
		"pull_request": map[string]any{
			"head": map[string]any{"ref": branch, "sha": sha},
		},
		"repository": map[string]any{"clone_url": repo},
	})
	return body
}

func deliverPR(t *testing.T, router http.Handler, path string, pc providerCase, action string, number int, repo string, secret string) *httptest.ResponseRecorder {
	t.Helper()
	body := prBody(action, number, repo, "feature", "abc123")
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
	req.Header.Set(pc.pushHeader, prHeaderValue(pc, action))
	req.Header.Set(pc.sigHeader, signFor(pc, secret, body))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func prHeaderValue(pc providerCase, action string) string {
	switch pc.provider {
	case "gitlab":
		return "Merge Request Hook"
	case "bitbucket":
		return "pullrequest:" + action
	default:
		return "pull_request"
	}
}

func TestPROpenedCreatesPreviewDeploymentPerProvider(t *testing.T) {
	for _, pc := range providerCases {
		t.Run(pc.provider, func(t *testing.T) {
			h := newTestHandler(t)
			router := newWebhookRouter(t, h)
			testdb.SeedGitProvider(t, pc.provider, webhookSecret)
			org := testdb.SeedOrg(t)
			appID := testdb.SeedApplication(t, org.ProjectID, "", "https://github.com/acme/web.git", nil)

			action := "opened"
			if pc.provider == "gitlab" {
				action = "open"
			}
			rec := deliverPR(t, router, pc.path, pc, action, 7, "https://github.com/acme/web.git", webhookSecret)

			if rec.Code != http.StatusAccepted {
				t.Fatalf("%s status = %d body=%s", pc.provider, rec.Code, rec.Body.String())
			}
			var resp map[string]any
			_ = json.Unmarshal(rec.Body.Bytes(), &resp)
			if resp["previews"] != float64(1) {
				t.Fatalf("%s previews = %v, want 1", pc.provider, resp["previews"])
			}
			n := testdb.QueryCount(t,
				`select count(*) from preview_deployments where application_id=$1::uuid and pr_number=7 and branch='feature' and commit_sha='abc123' and status='building'`,
				appID)
			if n != 1 {
				t.Fatalf("%s preview rows = %d, want 1", pc.provider, n)
			}
		})
	}
}

func TestPRErrorBranches(t *testing.T) {
	setup := func(t *testing.T, seedRepo bool) (http.Handler, string) {
		h := newTestHandler(t)
		testdb.SeedGitProvider(t, "github", webhookSecret)
		repo := "https://github.com/acme/web.git"
		if seedRepo {
			org := testdb.SeedOrg(t)
			testdb.SeedApplication(t, org.ProjectID, "", repo, nil)
		}
		return newWebhookRouter(t, h), repo
	}

	t.Run("invalid json with valid signature", func(t *testing.T) {
		router, _ := setup(t, true)
		body := "{oops"
		req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(body))
		req.Header.Set("X-GitHub-Event", "pull_request")
		req.Header.Set("X-Hub-Signature-256", githubSig(webhookSecret, []byte(body)))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("missing repository url", func(t *testing.T) {
		router, _ := setup(t, true)
		rec := deliverPR(t, router, "/webhooks/github", providerCases[0], "opened", 1, "", webhookSecret)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
		}
	})

	t.Run("bad signature", func(t *testing.T) {
		router, repo := setup(t, true)
		body := prBody("opened", 3, repo, "x", "y")
		req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(string(body)))
		req.Header.Set("X-GitHub-Event", "pull_request")
		req.Header.Set("X-Hub-Signature-256", "sha256=bad")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
	})

	t.Run("ignored action accepted", func(t *testing.T) {
		router, repo := setup(t, true)
		rec := deliverPR(t, router, "/webhooks/github", providerCases[0], "labeled", 3, repo, webhookSecret)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202", rec.Code)
		}
		var resp map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp["status"] != "ignored" {
			t.Fatalf("response = %v, want ignored", resp)
		}
	})

	t.Run("closed with no matching preview removes nothing", func(t *testing.T) {
		router, repo := setup(t, true)
		rec := deliverPR(t, router, "/webhooks/github", providerCases[0], "closed", 99, repo, webhookSecret)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202", rec.Code)
		}
		var resp map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp["removed"] != float64(0) {
			t.Fatalf("removed = %v, want 0", resp["removed"])
		}
	})
}

// errReader is an io.Reader whose reads always fail.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("read failure") }

func closedPoolHandler(t *testing.T) *Handler {
	t.Helper()
	shared := testdb.Get(t)
	p, err := pgxpool.New(context.Background(), shared.Config().ConnString())
	if err != nil {
		t.Fatalf("open secondary pool: %v", err)
	}
	p.Close() // queries against a closed pool fail immediately
	return NewHandler(p, nil)
}

func TestWebhookEndpointIgnoresUninterestingEventsPerProvider(t *testing.T) {
	tests := []struct {
		path   string
		header string
		value  string
	}{
		{path: "/webhooks/github", header: "X-GitHub-Event", value: "issues"},
		{path: "/webhooks/gitlab", header: "X-Gitlab-Event", value: "Note Hook"},
		{path: "/webhooks/bitbucket", header: "X-Event-Key", value: "issue:created"},
		{path: "/webhooks/gitea", header: "X-Gitea-Event", value: "issue"},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			h := newTestHandler(t)
			router := newWebhookRouter(t, h)
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader("{}"))
			req.Header.Set(tt.header, tt.value)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("status = %d, want 202", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "ignored") {
				t.Fatalf("body = %s, want ignored", rec.Body.String())
			}
		})
	}

	t.Run("handleWebhook with deploy disabled short-circuits", func(t *testing.T) {
		h := newTestHandler(t)
		rec := httptest.NewRecorder()
		h.handleWebhook(rec, httptest.NewRequest(http.MethodPost, "/", nil), "github", false)
		if rec.Code != http.StatusAccepted || !strings.Contains(rec.Body.String(), "ignored") {
			t.Fatalf("status=%d body=%s, want ignored 202", rec.Code, rec.Body.String())
		}
	})
}

func TestPushEventBodyReadFailureReturns400(t *testing.T) {
	h := newTestHandler(t)
	router := newWebhookRouter(t, h)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", nil)
	req.Body = io.NopCloser(errReader{})
	req.Header.Set("X-GitHub-Event", "push")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPRBodyReadFailureReturns400(t *testing.T) {
	h := newTestHandler(t)
	router := newWebhookRouter(t, h)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", nil)
	req.Body = io.NopCloser(errReader{})
	req.Header.Set("X-GitHub-Event", "pull_request")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestPushEventDatabaseErrorsReturn500(t *testing.T) {
	h := closedPoolHandler(t)
	router := newWebhookRouter(t, h)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader("{}"))
	req.Header.Set("X-GitHub-Event", "push")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("secrets query failure status = %d, want 500", rec.Code)
	}
}

func TestPRDatabaseErrorsReturn500(t *testing.T) {
	h := closedPoolHandler(t)
	router := newWebhookRouter(t, h)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader("{}"))
	req.Header.Set("X-GitHub-Event", "pull_request")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("secrets query failure status = %d, want 500", rec.Code)
	}
}

// gitlabObjectAttributesPayload mirrors GitLab's real Merge Request Hook
// shape, which relies on object_attributes fallbacks for every field.
func gitlabObjectAttributesPayload(repo string) []byte {
	body, _ := json.Marshal(map[string]any{
		"object_attributes": map[string]any{
			"action":        "open",
			"iid":           42,
			"source_branch": "feature-x",
			"last_commit":   map[string]any{"id": "deadbeef"},
		},
		"repository": map[string]any{"clone_url": repo},
	})
	return body
}

func TestGitLabObjectAttributesFallbacksUsed(t *testing.T) {
	h := newTestHandler(t)
	router := newWebhookRouter(t, h)
	testdb.SeedGitProvider(t, "gitlab", webhookSecret)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "", "https://gitlab.com/acme/svc.git", nil)

	body := gitlabObjectAttributesPayload("https://gitlab.com/acme/svc.git")
	req := httptest.NewRequest(http.MethodPost, "/webhooks/gitlab", strings.NewReader(string(body)))
	req.Header.Set("X-Gitlab-Event", "Merge Request Hook")
	req.Header.Set("X-Gitlab-Token", webhookSecret)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["previews"] != float64(1) {
		t.Fatalf("previews = %v (%s)", resp["previews"], rec.Body.String())
	}
	n := testdb.QueryCount(t, `
		select count(*) from preview_deployments
		where application_id=$1::uuid and pr_number=42 and branch='feature-x' and commit_sha='deadbeef'
	`, appID)
	if n != 1 {
		t.Fatalf("object_attributes fallback preview rows = %d, want 1", n)
	}
}

func TestPRClosedDatabaseErrorReturns500(t *testing.T) {
	testdb.Get(t)
	testdb.TruncateAll(t)
	testdb.SeedGitProvider(t, "github", webhookSecret)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "", "https://github.com/acme/web.git", nil)
	if _, err := testdb.Get(t).Exec(context.Background(), `
		insert into preview_deployments(organization_id, application_id, pr_number, branch, status)
		values ($1::uuid, $2::uuid, 7, 'feature', 'ready')
	`, org.OrgID, appID); err != nil {
		t.Fatalf("seed preview: %v", err)
	}

	h := closedPoolHandler(t)
	router := newWebhookRouter(t, h)
	rec := deliverPR(t, router, "/webhooks/github", providerCases[0], "closed", 7, "https://github.com/acme/web.git", webhookSecret)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("closed-pool PR close status = %d, want 500", rec.Code)
	}
}
