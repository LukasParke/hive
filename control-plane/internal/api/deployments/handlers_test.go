package deployments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	pgxpool "github.com/jackc/pgx/v5/pgxpool"
	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
	"github.com/luke/hive/control-plane/internal/jobs/riverjobs"
	"github.com/luke/hive/control-plane/internal/rbac"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// newDeploymentsRouter wires a real chi router with the same auth middleware
// used in production so JWTs and org headers are exercised end-to-end.
func newDeploymentsRouter(t *testing.T) (http.Handler, *Handler) {
	t.Helper()
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	h := NewHandler(pool, testdb.RiverClient(t))
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Post("/api/v1/applications/{id}/deployments", h.EnqueueDeploy)
		gr.Get("/api/v1/applications/{id}/deployments", h.ListApplicationDeployments)
		gr.Post("/api/v1/applications/{id}/rollback", h.RollbackApplication)
		gr.Get("/api/v1/applications/{id}/logs", h.ApplicationLogs)
		gr.Get("/api/v1/deployments", h.ListDeployments)
		gr.Delete("/api/v1/deployments/{id}", h.DeleteDeployment)
	})
	return r, h
}

func doJSON(router http.Handler, method, path string, headers http.Header, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func seedDeployment(t *testing.T, appID, imageTag, status, trigger string, age time.Duration) string {
	t.Helper()
	p := testdb.Get(t)
	var id string
	err := p.QueryRow(context.Background(), `
		insert into deployments(application_id, image_tag, status, trigger, created_at)
		values ($1::uuid, $2, $3, $4, $5)
		returning id::text
	`, appID, imageTag, status, trigger, time.Now().Add(-age)).Scan(&id)
	if err != nil {
		t.Fatalf("seed deployment: %v", err)
	}
	return id
}

func TestEnqueueDeploy(t *testing.T) {
	router, _ := newDeploymentsRouter(t)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "api", "https://github.com/acme/api.git", nil)

	t.Run("success queues build job and river job", func(t *testing.T) {
		rec := doJSON(router, http.MethodPost, "/api/v1/applications/"+appID+"/deployments", org.Headers, "")
		if rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Status  string `json:"status"`
			BuildID string `json:"buildId"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.BuildID == "" {
			t.Fatalf("response = %s err=%v", rec.Body.String(), err)
		}
		n := testdb.QueryCount(t, `
			select count(*) from build_jobs where id=$1::uuid and application_id=$2::uuid and trigger='api' and status='queued'
		`, resp.BuildID, appID)
		if n != 1 {
			t.Fatalf("build_jobs rows = %d, want 1", n)
		}
		if river := testdb.QueryCount(t, `select count(*) from river_job where state='available'`); river != 1 {
			t.Fatalf("river_job rows = %d, want 1", river)
		}
	})

	t.Run("conflict when active build exists returns 409", func(t *testing.T) {
		rec := doJSON(router, http.MethodPost, "/api/v1/applications/"+appID+"/deployments", org.Headers, "")
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d body=%s, want 409", rec.Code, rec.Body.String())
		}
		var resp map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp["error"] != "build_in_progress" {
			t.Fatalf("error code = %v, want build_in_progress", resp["error"])
		}
	})

	t.Run("unknown application returns 404", func(t *testing.T) {
		otherOrg := testdb.SeedOrg(t)
		rec := doJSON(router, http.MethodPost, "/api/v1/applications/"+testdb.SeedApplication(t, otherOrg.ProjectID, "", "", nil)+"/deployments", org.Headers, "")
		if rec.Code != http.StatusNotFound {
			t.Fatalf("cross-org application status = %d, want 404", rec.Code)
		}
	})

	t.Run("invalid uuid returns 400", func(t *testing.T) {
		rec := doJSON(router, http.MethodPost, "/api/v1/applications/not-a-uuid/deployments", org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("member role forbidden", func(t *testing.T) {
		member := org.AddMember(t, rbac.RoleMember)
		rec := doJSON(router, http.MethodPost, "/api/v1/applications/"+appID+"/deployments", member.Headers, "")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})
}

func TestRollbackApplication(t *testing.T) {
	router, _ := newDeploymentsRouter(t)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "web", "https://github.com/acme/web.git", nil)

	seedDeployment(t, appID, "sha-111", "complete", "api", 2*time.Hour)
	seedDeployment(t, appID, "sha-222", "complete", "api", 1*time.Hour)

	t.Run("happy path redeploys previous image tag", func(t *testing.T) {
		rec := doJSON(router, http.MethodPost, "/api/v1/applications/"+appID+"/rollback", org.Headers, "")
		if rec.Code != http.StatusAccepted {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			BuildID string `json:"buildId"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.BuildID == "" {
			t.Fatalf("response = %s err=%v", rec.Body.String(), err)
		}
		var imageTag string
		if err := testdb.Get(t).QueryRow(context.Background(), `
			select coalesce(image_tag,'') from build_jobs where id=$1::uuid
		`, resp.BuildID).Scan(&imageTag); err != nil {
			t.Fatalf("build row missing: %v", err)
		}
		if imageTag != "sha-111" {
			t.Fatalf("rollback built image_tag=%q, want sha-111 (second newest deployment)", imageTag)
		}
	})

	t.Run("no previous deployment returns 400", func(t *testing.T) {
		soloApp := testdb.SeedApplication(t, org.ProjectID, "solo", "", nil)
		seedDeployment(t, soloApp, "only-one", "complete", "api", time.Hour)
		rec := doJSON(router, http.MethodPost, "/api/v1/applications/"+soloApp+"/rollback", org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
		var resp map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		if resp["error"] != "no_previous_deployment" {
			t.Fatalf("error code = %v, want no_previous_deployment", resp["error"])
		}
	})

	t.Run("invalid uuid returns 400", func(t *testing.T) {
		rec := doJSON(router, http.MethodPost, "/api/v1/applications/bogus/rollback", org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("member role forbidden", func(t *testing.T) {
		member := org.AddMember(t, rbac.RoleMember)
		rec := doJSON(router, http.MethodPost, "/api/v1/applications/"+appID+"/rollback", member.Headers, "")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})

	t.Run("conflict maps to 409", func(t *testing.T) {
		quietApp := testdb.SeedApplication(t, org.ProjectID, "quiet", "", nil)
		seedDeployment(t, quietApp, "a", "complete", "api", 2*time.Hour)
		seedDeployment(t, quietApp, "b", "complete", "api", time.Hour)

		buildID, err := riverjobs.EnqueueBuild(context.Background(), testdb.RiverClient(t), testdb.Get(t), quietApp, "api", "")
		if err != nil {
			t.Fatalf("pre-enqueue active build: %v", err)
		}
		_ = buildID

		rec := doJSON(router, http.MethodPost, "/api/v1/applications/"+quietApp+"/rollback", org.Headers, "")
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d body=%s, want 409", rec.Code, rec.Body.String())
		}
	})
}

func TestListApplicationDeployments(t *testing.T) {
	router, _ := newDeploymentsRouter(t)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "api", "", nil)
	seedDeployment(t, appID, "old", "failed", "webhook", 2*time.Hour)
	seedDeployment(t, appID, "new", "complete", "api", time.Hour)

	t.Run("returns newest first", func(t *testing.T) {
		rec := doJSON(router, http.MethodGet, "/api/v1/applications/"+appID+"/deployments", org.Headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Items []map[string]any `json:"items"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp.Items) != 2 {
			t.Fatalf("items = %d, want 2 (%s)", len(resp.Items), rec.Body.String())
		}
		if resp.Items[0]["imageTag"] != "new" {
			t.Fatalf("first item = %v, want newest deployment 'new'", resp.Items[0])
		}
	})

	t.Run("invalid uuid returns 400", func(t *testing.T) {
		rec := doJSON(router, http.MethodGet, "/api/v1/applications/xyz/deployments", org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("other org sees nothing", func(t *testing.T) {
		otherOrg := testdb.SeedOrg(t)
		rec := doJSON(router, http.MethodGet, "/api/v1/applications/"+appID+"/deployments", otherOrg.Headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), `"items":null`) && !strings.Contains(rec.Body.String(), `"items":[]`) {
			t.Fatalf("expected empty items for foreign org: %s", rec.Body.String())
		}
	})
}

func TestListDeploymentsScopedToOrganization(t *testing.T) {
	router, _ := newDeploymentsRouter(t)
	orgA := testdb.SeedOrg(t)
	orgB := testdb.SeedOrg(t)
	appA := testdb.SeedApplication(t, orgA.ProjectID, "a", "", nil)
	appB := testdb.SeedApplication(t, orgB.ProjectID, "b", "", nil)
	seedDeployment(t, appA, "img-a", "complete", "api", time.Hour)
	seedDeployment(t, appB, "img-b", "complete", "api", time.Hour)

	rec := doJSON(router, http.MethodGet, "/api/v1/deployments", orgA.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0]["applicationName"] != "a" {
		t.Fatalf("items = %v, want only org A's deployment", resp.Items)
	}

	unauth := http.Header{}
	rec = doJSON(router, http.MethodGet, "/api/v1/deployments", unauth, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing auth status = %d, want 401", rec.Code)
	}
}

func TestApplicationLogs(t *testing.T) {
	router, _ := newDeploymentsRouter(t)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "api", "", nil)
	seedDeployment(t, appID, "img-1", "complete", "api", time.Hour)
	seedDeployment(t, appID, "img-2", "failed", "rollback", 30*time.Minute)

	t.Run("formats one line per deployment", func(t *testing.T) {
		rec := doJSON(router, http.MethodGet, "/api/v1/applications/"+appID+"/logs", org.Headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		var resp struct {
			Logs []string `json:"logs"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(resp.Logs) != 2 {
			t.Fatalf("logs = %#v, want 2 lines", resp.Logs)
		}
		for _, line := range resp.Logs {
			if !strings.Contains(line, "deployment=") || !strings.Contains(line, "image=") || !strings.Contains(line, "status=") || !strings.Contains(line, "trigger=") {
				t.Fatalf("malformed log line: %q", line)
			}
		}
	})

	t.Run("invalid uuid returns 400", func(t *testing.T) {
		rec := doJSON(router, http.MethodGet, "/api/v1/applications/nope/logs", org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})
}

func TestDeleteDeployment(t *testing.T) {
	router, _ := newDeploymentsRouter(t)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "api", "", nil)
	depID := seedDeployment(t, appID, "img", "complete", "api", time.Hour)

	t.Run("removes own organization's deployment", func(t *testing.T) {
		rec := doJSON(router, http.MethodDelete, "/api/v1/deployments/"+depID, org.Headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
		}
		if n := testdb.QueryCount(t, `select count(*) from deployments where id=$1::uuid`, depID); n != 0 {
			t.Fatalf("deployment row survived delete")
		}
	})

	t.Run("cannot remove another organization's deployment", func(t *testing.T) {
		otherOrg := testdb.SeedOrg(t)
		foreignDep := seedDeployment(t, testdb.SeedApplication(t, otherOrg.ProjectID, "b", "", nil), "img", "complete", "api", time.Hour)
		rec := doJSON(router, http.MethodDelete, "/api/v1/deployments/"+foreignDep, org.Headers, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("delete of foreign deployment should be a no-op success, got %d", rec.Code)
		}
		if n := testdb.QueryCount(t, `select count(*) from deployments where id=$1::uuid`, foreignDep); n != 1 {
			t.Fatalf("foreign deployment was deleted")
		}
	})

	t.Run("member role forbidden", func(t *testing.T) {
		member := org.AddMember(t, rbac.RoleMember)
		id := seedDeployment(t, appID, "img2", "complete", "api", time.Hour)
		rec := doJSON(router, http.MethodDelete, "/api/v1/deployments/"+id, member.Headers, "")
		if rec.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403", rec.Code)
		}
	})
}

func TestWriteEnqueueConflict(t *testing.T) {
	t.Run("handles sentinel", func(t *testing.T) {
		rec := httptest.NewRecorder()
		if !writeEnqueueConflict(rec, riverjobs.ErrBuildAlreadyQueued) {
			t.Fatal("writeEnqueueConflict should handle ErrBuildAlreadyQueued")
		}
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409", rec.Code)
		}
	})

	t.Run("handles wrapped sentinel", func(t *testing.T) {
		rec := httptest.NewRecorder()
		wrapped := fmt.Errorf("enqueue deploy: %w", riverjobs.ErrBuildAlreadyQueued)
		if !writeEnqueueConflict(rec, wrapped) {
			t.Fatal("wrapped ErrBuildAlreadyQueued should be handled")
		}
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d, want 409", rec.Code)
		}
	})

	t.Run("ignores unrelated errors", func(t *testing.T) {
		rec := httptest.NewRecorder()
		if writeEnqueueConflict(rec, errors.New("boom")) {
			t.Fatal("unrelated error should not be handled")
		}
		if rec.Code != http.StatusOK {
			t.Fatalf("unhandled error wrote response: status %d", rec.Code)
		}
	})
}

func TestIsUniqueViolation(t *testing.T) {
	if riverjobs.IsUniqueViolation(errors.New("plain")) {
		t.Fatal("plain error must not be a unique violation")
	}
}

// renameColumn renames a column so matching statements fail at parse time; it
// restores itself via t.Cleanup.
func renameColumn(t *testing.T, table, from, to string) {
	t.Helper()
	ctx := context.Background()
	p := testdb.Get(t)
	if _, err := p.Exec(ctx, fmt.Sprintf("alter table %s rename column %s to %s", table, from, to)); err != nil {
		t.Fatalf("rename %s.%s: %v", table, from, err)
	}
	t.Cleanup(func() {
		if _, err := p.Exec(ctx, fmt.Sprintf("alter table %s rename column %s to %s", table, to, from)); err != nil {
			t.Fatalf("restore %s.%s: %v", table, to, err)
		}
	})
}

// simpleProtocolRouter builds a router whose handler uses a simple-protocol
// pool: parse-time SQL errors surface synchronously from Query, letting us
// exercise the streaming-query failure branches deterministically.
func simpleProtocolRouter(t *testing.T) http.Handler {
	t.Helper()
	pool := testdb.Get(t)
	testdb.TruncateAll(t)

	cfg, err := pgxpool.ParseConfig(pool.Config().ConnString())
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	cfg.MaxConns = 4
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	handlerPool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open simple protocol pool: %v", err)
	}
	t.Cleanup(handlerPool.Close)

	h := NewHandler(handlerPool, nil)
	r := chi.NewRouter()
	r.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
	r.Post("/api/v1/applications/{id}/deployments", h.EnqueueDeploy)
	r.Get("/api/v1/applications/{id}/deployments", h.ListApplicationDeployments)
	r.Post("/api/v1/applications/{id}/rollback", h.RollbackApplication)
	r.Get("/api/v1/applications/{id}/logs", h.ApplicationLogs)
	r.Get("/api/v1/deployments", h.ListDeployments)
	r.Delete("/api/v1/deployments/{id}", h.DeleteDeployment)
	return r
}

func TestStatementFailuresReturnErrorResponses(t *testing.T) {
	router := simpleProtocolRouter(t)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "api", "", nil)
	seedDeployment(t, appID, "img", "complete", "api", time.Hour)
	depID := seedDeployment(t, appID, "img2", "complete", "api", time.Hour)

	t.Run("enqueue deploy existence query failure", func(t *testing.T) {
		renameColumn(t, "projects", "organization_id", "organization_id_gone")
		rec := doJSON(router, http.MethodPost, "/api/v1/applications/"+appID+"/deployments", org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("list application deployments query failure", func(t *testing.T) {
		renameColumn(t, "deployments", "status", "status_gone")
		rec := doJSON(router, http.MethodGet, "/api/v1/applications/"+appID+"/deployments", org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("application logs query failure", func(t *testing.T) {
		renameColumn(t, "deployments", "trigger", "trigger_gone")
		rec := doJSON(router, http.MethodGet, "/api/v1/applications/"+appID+"/logs", org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("list deployments query failure", func(t *testing.T) {
		renameColumn(t, "applications", "name", "name_gone")
		rec := doJSON(router, http.MethodGet, "/api/v1/deployments", org.Headers, "")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rec.Code)
		}
	})

	t.Run("enqueue deploy insert failure maps to bad request", func(t *testing.T) {
		renameColumn(t, "build_jobs", "trigger", "trigger_gone")
		rec := doJSON(router, http.MethodPost, "/api/v1/applications/"+appID+"/deployments", org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("rollback insert failure maps to bad request", func(t *testing.T) {
		renameColumn(t, "build_jobs", "image_tag", "image_tag_gone")
		rec := doJSON(router, http.MethodPost, "/api/v1/applications/"+appID+"/rollback", org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
		}
	})

	t.Run("delete deployment exec failure", func(t *testing.T) {
		renameColumn(t, "deployments", "application_id", "application_id_gone")
		rec := doJSON(router, http.MethodDelete, "/api/v1/deployments/"+depID, org.Headers, "")
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
	})

	t.Run("deployment row scan failure surfaces as 500", func(t *testing.T) {
		p := testdb.Get(t)
		if _, err := p.Exec(context.Background(), `alter table deployments alter column image_tag drop not null`); err != nil {
			t.Fatalf("drop not null: %v", err)
		}
		t.Cleanup(func() {
			if _, err := p.Exec(context.Background(), `delete from deployments where image_tag is null`); err != nil {
				t.Fatalf("cleanup null rows: %v", err)
			}
			if _, err := p.Exec(context.Background(), `alter table deployments alter column image_tag set not null`); err != nil {
				t.Fatalf("restore not null: %v", err)
			}
		})
		if _, err := p.Exec(context.Background(), `
			insert into deployments(application_id, image_tag, status, trigger, created_at)
			values ($1::uuid, null, 'complete', 'api', now())
		`, appID); err != nil {
			t.Fatalf("seed null image_tag deployment: %v", err)
		}
		rec := doJSON(router, http.MethodGet, "/api/v1/applications/"+appID+"/deployments", org.Headers, "")
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
		}
	})
}

func TestEndpointsRejectUnauthenticatedRequests(t *testing.T) {
	router, _ := newDeploymentsRouter(t)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "api", "", nil)

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{name: "list application deployments", method: http.MethodGet, path: "/api/v1/applications/" + appID + "/deployments"},
		{name: "application logs", method: http.MethodGet, path: "/api/v1/applications/" + appID + "/logs"},
		{name: "delete deployment", method: http.MethodDelete, path: "/api/v1/deployments/00000000-0000-0000-0000-000000000000"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rec := doJSON(router, tt.method, tt.path, http.Header{}, "")
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
		})
	}
}

func TestForbiddenForNonMembers(t *testing.T) {
	router := simpleProtocolRouter(t)
	orgA := testdb.SeedOrg(t)
	orgB := testdb.SeedOrg(t) // authenticated, but not a member of org A
	appA := testdb.SeedApplication(t, orgA.ProjectID, "a", "", nil)
	seedDeployment(t, appA, "img", "complete", "api", time.Hour)
	depID := seedDeployment(t, appA, "img2", "complete", "api", time.Hour)

	// Authenticated as an org B user but presenting org A as the active org.
	intruder := http.Header{}
	intruder.Set("Authorization", "Bearer "+orgB.Token)
	intruder.Set("X-Organization-Id", orgA.OrgID)

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{name: "list application deployments", method: http.MethodGet, path: "/api/v1/applications/" + appA + "/deployments"},
		{name: "application logs", method: http.MethodGet, path: "/api/v1/applications/" + appA + "/logs"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			rec := doJSON(router, tt.method, tt.path, intruder, "")
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d body=%s, want 403", rec.Code, rec.Body.String())
			}
		})
	}

	for _, tt := range []struct{ name, method, path string }{
		{name: "enqueue deploy", method: http.MethodPost, path: "/api/v1/applications/" + appA + "/deployments"},
		{name: "rollback", method: http.MethodPost, path: "/api/v1/applications/" + appA + "/rollback"},
		{name: "delete deployment", method: http.MethodDelete, path: "/api/v1/deployments/" + depID},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := doJSON(router, tt.method, tt.path, intruder, "")
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d body=%s, want 403", rec.Code, rec.Body.String())
			}
		})
	}
}
