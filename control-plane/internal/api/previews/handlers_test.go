package previews

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	dbgen "github.com/luke/hive/control-plane/internal/db/generated"

	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
	"github.com/luke/hive/control-plane/internal/testdb"
)

func newPreviewsRouter(t *testing.T, withRiver bool) http.Handler {
	t.Helper()
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	var h *Handler
	if withRiver {
		h = NewHandler(pool, testdb.RiverClient(t))
	} else {
		h = &Handler{Pool: pool, Q: dbgen.New(pool)}
	}
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Get("/api/v1/applications/{id}/previews", h.ListPreviewDeployments)
		gr.Post("/api/v1/applications/{id}/previews", h.CreatePreviewDeployment)
		gr.Get("/api/v1/applications/{id}/previews/{previewId}", h.GetPreviewDeployment)
		gr.Delete("/api/v1/applications/{id}/previews/{previewId}", h.DeletePreviewDeployment)
	})
	return r
}

func doJSON(t *testing.T, router http.Handler, method, path string, headers http.Header, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, vs := range headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func seedPreview(t *testing.T, orgID, appID string, pr int32) string {
	t.Helper()
	var id string
	if err := testdb.Get(t).QueryRow(context.Background(), `
		insert into preview_deployments(organization_id, application_id, pr_number, branch, status)
		values ($1::uuid, $2::uuid, $3, 'feature/x', 'ready') returning id::text
	`, orgID, appID, pr).Scan(&id); err != nil {
		t.Fatalf("seed preview: %v", err)
	}
	return id
}

func TestCreatePreviewDeploymentHappyPath(t *testing.T) {
	router := newPreviewsRouter(t, true)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "web", "https://github.com/acme/web.git", nil)

	body := `{"prNumber":42,"branch":"feature/x","commitSha":"abc123"}`
	rec := doJSON(t, router, http.MethodPost, "/api/v1/applications/"+appID+"/previews", org.Headers, body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.ID == "" {
		t.Fatalf("decode id: %v %s", err, rec.Body.String())
	}

	var branch, status string
	var pr int32
	if err := testdb.Get(t).QueryRow(context.Background(), `
		select branch, status, pr_number from preview_deployments where id=$1::uuid
	`, resp.ID).Scan(&branch, &status, &pr); err != nil {
		t.Fatalf("preview row missing: %v", err)
	}
	if branch != "feature/x" || status != "building" || pr != 42 {
		t.Fatalf("row = (%s %s %d)", branch, status, pr)
	}
	if n := testdb.QueryCount(t, `select count(*) from river_job`); n != 1 {
		t.Fatalf("river jobs = %d, want 1", n)
	}
}

func TestCreatePreviewDeploymentWithoutRiverStillPersists(t *testing.T) {
	router := newPreviewsRouter(t, false)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "noriver", "", nil)

	rec := doJSON(t, router, http.MethodPost, "/api/v1/applications/"+appID+"/previews", org.Headers,
		`{"prNumber":7,"branch":"main"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if n := testdb.QueryCount(t, `select count(*) from river_job`); n != 0 {
		t.Fatalf("river jobs = %d, want 0 without client", n)
	}
}

func TestCreatePreviewDeploymentValidation(t *testing.T) {
	router := newPreviewsRouter(t, false)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "app", "", nil)
	foreignApp := testdb.SeedApplication(t, testdb.SeedOrg(t).ProjectID, "foreign", "", nil)

	cases := []struct {
		name   string
		path   string
		body   string
		status int
	}{
		{"malformed json", appID, `{broken`, http.StatusBadRequest},
		{"invalid application id", "not-a-uuid", `{"prNumber":1}`, http.StatusBadRequest},
		{"unknown application", "00000000-0000-0000-0000-000000000000", `{"prNumber":1,"branch":"x"}`, http.StatusNotFound},
		{"cross-org application forbidden", foreignApp, `{"prNumber":1}`, http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, router, http.MethodPost, "/api/v1/applications/"+tc.path+"/previews", org.Headers, tc.body)
			if rec.Code != tc.status {
				t.Fatalf("status = %d body=%s, want %d", rec.Code, rec.Body.String(), tc.status)
			}
		})
	}
}

func TestListPreviewDeployments(t *testing.T) {
	router := newPreviewsRouter(t, false)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "listed", "", nil)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/applications/"+appID+"/previews", org.Headers, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"items":null`) {
		t.Fatalf("empty list status = %d body=%s", rec.Code, rec.Body.String())
	}

	seedPreview(t, org.OrgID, appID, 1)
	seedPreview(t, org.OrgID, appID, 2)

	rec = doJSON(t, router, http.MethodGet, "/api/v1/applications/"+appID+"/previews", org.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			ID       string `json:"id"`
			PrNumber int32  `json:"prNumber"`
			Status   string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("items = %d, want 2 (%s)", len(resp.Items), rec.Body.String())
	}
}

func TestListPreviewDeploymentValidation(t *testing.T) {
	router := newPreviewsRouter(t, false)
	org := testdb.SeedOrg(t)

	cases := []struct {
		name   string
		path   string
		status int
	}{
		{"invalid application id", "zzz", http.StatusBadRequest},
		{"unknown application", "00000000-0000-0000-0000-000000000000", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, router, http.MethodGet, "/api/v1/applications/"+tc.path+"/previews", org.Headers, "")
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
		})
	}
}

func TestGetPreviewDeploymentFoundAndMissing(t *testing.T) {
	router := newPreviewsRouter(t, false)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "get", "", nil)
	previewID := seedPreview(t, org.OrgID, appID, 9)

	rec := doJSON(t, router, http.MethodGet, "/api/v1/applications/"+appID+"/previews/"+previewID, org.Headers, "")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"pr_number":9`) {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	cases := []struct {
		name       string
		appPath    string
		previewID  string
		wantStatus int
	}{
		{"invalid preview id", appID, "nope", http.StatusBadRequest},
		{"invalid application id", "junk", "nope", http.StatusBadRequest},
		{"unknown application", "00000000-0000-0000-0000-000000000000",
			"00000000-0000-0000-0000-000000000000", http.StatusNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(t, router, http.MethodGet,
				"/api/v1/applications/"+tc.appPath+"/previews/"+tc.previewID, org.Headers, "")
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
		})
	}
}

func TestDeletePreviewDeploymentRemovesRow(t *testing.T) {
	router := newPreviewsRouter(t, false)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "del", "", nil)
	previewID := seedPreview(t, org.OrgID, appID, 5)

	rec := doJSON(t, router, http.MethodDelete, "/api/v1/applications/"+appID+"/previews/"+previewID, org.Headers, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d body=%s, want 204", rec.Code, rec.Body.String())
	}
	if n := testdb.QueryCount(t, `select count(*) from preview_deployments where id=$1::uuid`, previewID); n != 0 {
		t.Fatal("preview row not deleted")
	}

	rec = doJSON(t, router, http.MethodDelete, "/api/v1/applications/"+appID+"/previews/"+previewID, org.Headers, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("repeat delete status = %d, want 204 (idempotent exec)", rec.Code)
	}
}

func TestDeletePreviewDeploymentValidation(t *testing.T) {
	router := newPreviewsRouter(t, false)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "delv", "", nil)

	rec := doJSON(t, router, http.MethodDelete, "/api/v1/applications/"+appID+"/previews/junk", org.Headers, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid preview id status = %d, want 400", rec.Code)
	}
	rec = doJSON(t, router, http.MethodDelete, "/api/v1/applications/junk/previews/11111111-1111-4111-8111-111111111111", org.Headers, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid application id status = %d, want 400", rec.Code)
	}

	foreignApp := testdb.SeedApplication(t, testdb.SeedOrg(t).ProjectID, "foreign2", "", nil)
	rec = doJSON(t, router, http.MethodDelete, "/api/v1/applications/"+foreignApp+"/previews/11111111-1111-4111-8111-111111111111", org.Headers, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-org delete status = %d, want 404 (unknown application)", rec.Code)
	}
}

func outsiderHeaders(t *testing.T, orgID string) http.Header {
	t.Helper()
	svc := testdb.Auth(t)
	email := fmt.Sprintf("outsider-%s@test.local", strings.ReplaceAll(uuid.NewString(), "-", "")[:12])
	if _, err := svc.Register(context.Background(), email, "sup3rsecret!", "Outsider"); err != nil {
		t.Fatalf("register outsider: %v", err)
	}
	token, _, err := svc.Login(context.Background(), email, "sup3rsecret!")
	if err != nil {
		t.Fatalf("login outsider: %v", err)
	}
	h := http.Header{}
	h.Set("Authorization", "Bearer "+token)
	h.Set("X-Organization-Id", orgID)
	return h
}

func TestPreviewsDenyNonMembers(t *testing.T) {
	router := newPreviewsRouter(t, false)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "denyapp", "", nil)
	previewID := seedPreview(t, org.OrgID, appID, 1)
	headers := outsiderHeaders(t, org.OrgID)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/applications/" + appID + "/previews"},
		{http.MethodPost, "/api/v1/applications/" + appID + "/previews"},
		{http.MethodGet, "/api/v1/applications/" + appID + "/previews/" + previewID},
		{http.MethodDelete, "/api/v1/applications/" + appID + "/previews/" + previewID},
	} {
		rec := doJSON(t, router, tc.method, tc.path, headers, `{"prNumber":1,"branch":"x"}`)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("%s %s outsider status = %d body=%s, want 403", tc.method, tc.path, rec.Code, rec.Body.String())
		}
	}
}

// TestPreviewDeploymentsDBFailures renames the backing table so statements
// touching it fail at prepare time on a fresh (uncached) pool.
func TestPreviewDeploymentsDBFailures(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	org := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, org.ProjectID, "brokenapp", "", nil)

	if _, err := pool.Exec(context.Background(), `alter table preview_deployments rename to preview_deployments_gone`); err != nil {
		t.Fatalf("rename: %v", err)
	}
	cfg, err := pgxpool.ParseConfig(pool.Config().ConnConfig.ConnString())
	if err != nil {
		t.Fatalf("parse conn string: %v", err)
	}
	fresh, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	defer fresh.Close()
	r := chi.NewRouter()
	h := NewHandler(fresh, nil)
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), fresh))
		gr.Get("/api/v1/applications/{id}/previews", h.ListPreviewDeployments)
		gr.Post("/api/v1/applications/{id}/previews", h.CreatePreviewDeployment)
		gr.Delete("/api/v1/applications/{id}/previews/{previewId}", h.DeletePreviewDeployment)
	})

	listRec := doJSON(t, r, http.MethodGet, "/api/v1/applications/"+appID+"/previews", org.Headers, "")
	createRec := doJSON(t, r, http.MethodPost, "/api/v1/applications/"+appID+"/previews", org.Headers,
		`{"prNumber":1,"branch":"x"}`)
	deleteRec := doJSON(t, r, http.MethodDelete, "/api/v1/applications/"+appID+"/previews/00000000-0000-0000-0000-000000000000", org.Headers, "")

	if _, err := pool.Exec(context.Background(), `alter table preview_deployments_gone rename to preview_deployments`); err != nil {
		t.Fatalf("restore: %v", err)
	}

	if listRec.Code != http.StatusInternalServerError || !strings.Contains(listRec.Body.String(), "failed to list preview deployments") {
		t.Fatalf("list status = %d body=%s, want 500", listRec.Code, listRec.Body.String())
	}
	if createRec.Code != http.StatusBadRequest || !strings.Contains(createRec.Body.String(), "failed to create preview deployment") {
		t.Fatalf("create status = %d body=%s, want 400", createRec.Code, createRec.Body.String())
	}
	if deleteRec.Code != http.StatusBadRequest || !strings.Contains(deleteRec.Body.String(), "failed to delete preview deployment") {
		t.Fatalf("delete status = %d body=%s, want 400", deleteRec.Code, deleteRec.Body.String())
	}
}
