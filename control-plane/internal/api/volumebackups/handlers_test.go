package volumebackups

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
	"github.com/luke/hive/control-plane/internal/testdb"
)

// newRouter wires the real auth middleware around every volume backup endpoint
// so JWTs and RBAC are exercised end to end against the shared Postgres pool.
func newRouter(t *testing.T) http.Handler {
	t.Helper()
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	h := NewHandler(pool)
	r := chi.NewRouter()
	r.Group(func(gr chi.Router) {
		gr.Use(apimiddleware.WithAuth(testdb.Auth(t), pool))
		gr.Get("/api/v1/volume-backups", h.ListVolumeBackups)
		gr.Post("/api/v1/volume-backups", h.CreateVolumeBackup)
		gr.Get("/api/v1/volume-backups/{id}", h.GetVolumeBackup)
		gr.Delete("/api/v1/volume-backups/{id}", h.DeleteVolumeBackup)
	})
	return r
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

// seedDestination inserts a backup destination usable as the FK target.
func seedDestination(t *testing.T, name string) string {
	t.Helper()
	var id string
	if err := testdb.Get(t).QueryRow(t.Context(), `
		insert into backup_destinations(name, type, config) values ($1, 'local', '{}'::jsonb) returning id::text
	`, name).Scan(&id); err != nil {
		t.Fatalf("seed destination: %v", err)
	}
	return id
}

func seedBackup(t *testing.T, router http.Handler, headers http.Header, volume, destID string) string {
	t.Helper()
	rec := doJSON(router, http.MethodPost, "/api/v1/volume-backups", headers,
		`{"volumeName":"`+volume+`","destinationId":"`+destID+`"}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("seed volume backup status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || resp.ID == "" {
		t.Fatalf("seed volume backup response = %s err=%v", rec.Body.String(), err)
	}
	return resp.ID
}

func TestCreateVolumeBackup(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	destID := seedDestination(t, "nas")

	id := seedBackup(t, router, org.Headers, "app-data", destID)
	if n := testdb.QueryCount(t, `
		select count(*) from volume_backups
		where id=$1::uuid and organization_id=$2::uuid and volume_name='app-data'
		  and status='queued' and destination_id=$3::uuid
	`, id, org.OrgID, destID); n != 1 {
		t.Fatalf("volume backup rows = %d, want 1", n)
	}
}

func TestListVolumeBackupsOrgScoped(t *testing.T) {
	router := newRouter(t)
	orgA := testdb.SeedOrg(t)
	orgB := testdb.SeedOrg(t)
	dest := seedDestination(t, "nas")
	seedBackup(t, router, orgA.Headers, "vol-a", dest)
	seedBackup(t, router, orgB.Headers, "vol-b", dest)

	rec := doJSON(router, http.MethodGet, "/api/v1/volume-backups", orgB.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Items []struct {
			ID         string `json:"id"`
			VolumeName string `json:"volume_name"`
			Status     string `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].VolumeName != "vol-b" {
		t.Fatalf("org B items = %+v, want only vol-b", resp.Items)
	}

	// Get by ID round-trips the stored fields.
	rec = doJSON(router, http.MethodGet, "/api/v1/volume-backups/"+resp.Items[0].ID, orgB.Headers, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", rec.Code, rec.Body.String())
	}
	var item struct {
		VolumeName string `json:"volume_name"`
		Status     string `json:"status"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &item); err != nil || item.VolumeName != "vol-b" || item.Status != "queued" {
		t.Fatalf("item = %+v err=%v", item, err)
	}
}

func TestDeleteVolumeBackup(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	dest := seedDestination(t, "nas")
	id := seedBackup(t, router, org.Headers, "vol-a", dest)

	rec := doJSON(router, http.MethodDelete, "/api/v1/volume-backups/"+id, org.Headers, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body=%s, want 204", rec.Code, rec.Body.String())
	}
	if n := testdb.QueryCount(t, `select count(*) from volume_backups where id=$1::uuid`, id); n != 0 {
		t.Fatalf("backup rows after delete = %d, want 0", n)
	}
}

func TestVolumeBackupValidationTable(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)

	missing := "11111111-1111-1111-1111-111111111111"

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"create bad json", http.MethodPost, "/api/v1/volume-backups", `{`},
		{"create bad destination", http.MethodPost, "/api/v1/volume-backups", `{"destinationId":"nope"}`},
		{"create fk violation", http.MethodPost, "/api/v1/volume-backups",
			`{"volumeName":"v","destinationId":"` + missing + `"}`},
		{"get bad uuid", http.MethodGet, "/api/v1/volume-backups/nope", ""},
		{"delete bad uuid", http.MethodDelete, "/api/v1/volume-backups/nope", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(router, tc.method, tc.path, org.Headers, tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s %s status = %d body=%s, want 400", tc.method, tc.path, rec.Code, rec.Body.String())
			}
		})
	}

	// Get of a valid but unknown ID is a 404, not a validation failure.
	rec := doJSON(router, http.MethodGet, "/api/v1/volume-backups/"+missing, org.Headers, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get not found status = %d body=%s, want 404", rec.Code, rec.Body.String())
	}
}

func TestVolumeBackupCrossOrgScoping(t *testing.T) {
	router := newRouter(t)
	orgA := testdb.SeedOrg(t)
	orgB := testdb.SeedOrg(t)
	dest := seedDestination(t, "nas")
	id := seedBackup(t, router, orgA.Headers, "vol-a", dest)

	rec := doJSON(router, http.MethodGet, "/api/v1/volume-backups/"+id, orgB.Headers, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-org get status = %d, want 404", rec.Code)
	}

	// Delete matches zero rows for org B: silent no-op 204, row untouched.
	rec = doJSON(router, http.MethodDelete, "/api/v1/volume-backups/"+id, orgB.Headers, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("cross-org delete status = %d, want silent no-op 204", rec.Code)
	}
	if n := testdb.QueryCount(t, `select count(*) from volume_backups where id=$1::uuid`, id); n != 1 {
		t.Fatalf("backup rows after cross-org delete = %d, want 1 (no-op)", n)
	}
}

func TestVolumeBackupForeignMemberForbidden(t *testing.T) {
	router := newRouter(t)
	orgA := testdb.SeedOrg(t)
	orgB := testdb.SeedOrg(t)

	foreignHeaders := orgB.Headers.Clone()
	foreignHeaders.Set("X-Organization-Id", orgA.OrgID)

	cases := []struct {
		name   string
		method string
		path   string
	}{
		{"list", http.MethodGet, "/api/v1/volume-backups"},
		{"create", http.MethodPost, "/api/v1/volume-backups"},
		{"get", http.MethodGet, "/api/v1/volume-backups/11111111-1111-1111-1111-111111111111"},
		{"delete", http.MethodDelete, "/api/v1/volume-backups/11111111-1111-1111-1111-111111111111"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(router, tc.method, tc.path, foreignHeaders, `{}`)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("status = %d body=%s, want 403", rec.Code, rec.Body.String())
			}
		})
	}
}

// renameColumn renames a table column so statements touching it fail at plan
// time, exercising the handlers' database-error branches; it restores itself
// via t.Cleanup.
func renameColumn(t *testing.T, table, from, to string) {
	t.Helper()
	ctx := context.Background()
	p := testdb.Get(t)
	if _, err := p.Exec(ctx, "alter table "+table+" rename column "+from+" to "+to); err != nil {
		t.Fatalf("rename %s.%s: %v", table, from, err)
	}
	t.Cleanup(func() {
		if _, err := p.Exec(ctx, "alter table "+table+" rename column "+to+" to "+from); err != nil {
			t.Fatalf("restore %s.%s: %v", table, to, err)
		}
	})
}

// bracedOrgHeader wraps the organization id in braces: Postgres accepts the
// form in ::uuid casts but common.ToUUID rejects it, so RBAC passes while the
// handler's own conversion fails with 400 invalid organization id.
func bracedOrgHeader(headers http.Header) http.Header {
	h := headers.Clone()
	h.Set("X-Organization-Id", "{"+h.Get("X-Organization-Id")+"}")
	return h
}

func TestInvalidOrgHeaderReturns400(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	missing := "11111111-1111-1111-1111-111111111111"

	cases := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{"list", http.MethodGet, "/api/v1/volume-backups", ""},
		{"create", http.MethodPost, "/api/v1/volume-backups", `{"volumeName":"v","destinationId":"11111111-1111-1111-1111-111111111111"}`},
		{"get", http.MethodGet, "/api/v1/volume-backups/" + missing, ""},
		{"delete", http.MethodDelete, "/api/v1/volume-backups/" + missing, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := doJSON(router, tc.method, tc.path, bracedOrgHeader(org.Headers), tc.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s %s status = %d body=%s, want 400", tc.method, tc.path, rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "invalid organization id") {
				t.Fatalf("body = %s, want invalid organization id", rec.Body.String())
			}
		})
	}
}

func TestListVolumeBackupsQueryFailureReturns500(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	renameColumn(t, "volume_backups", "volume_name", "volume_name_gone")
	rec := doJSON(router, http.MethodGet, "/api/v1/volume-backups", org.Headers, "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s, want 500", rec.Code, rec.Body.String())
	}
}

func TestCreateVolumeBackupExecFailureReturns400(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	dest := seedDestination(t, "nas")
	renameColumn(t, "volume_backups", "volume_name", "volume_name_gone")
	rec := doJSON(router, http.MethodPost, "/api/v1/volume-backups", org.Headers,
		`{"volumeName":"vol-fail","destinationId":"`+dest+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
}

func TestDeleteVolumeBackupExecFailureReturns204NoOpCheck(t *testing.T) {
	router := newRouter(t)
	org := testdb.SeedOrg(t)
	dest := seedDestination(t, "nas")
	id := seedBackup(t, router, org.Headers, "vol-del", dest)
	// DeleteVolumeBackup is :exec without RowsAffected checks and its WHERE
	// clause touches no renamed column, so force the failure via the table.
	ctx := context.Background()
	if _, err := testdb.Get(t).Exec(ctx, `alter table volume_backups rename to volume_backups_gone`); err != nil {
		t.Fatalf("rename table: %v", err)
	}
	t.Cleanup(func() {
		if _, err := testdb.Get(t).Exec(ctx, `alter table volume_backups_gone rename to volume_backups`); err != nil {
			t.Fatalf("restore table: %v", err)
		}
	})
	rec := doJSON(router, http.MethodDelete, "/api/v1/volume-backups/"+id, org.Headers, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d body=%s, want 400", rec.Code, rec.Body.String())
	}
}
