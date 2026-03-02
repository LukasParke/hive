package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMock(t *testing.T) (*Store, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	return &Store{db: db}, mock
}

func appColumns() []string {
	return []string{
		"id", "project_id", "name", "deploy_type", "image", "git_repo", "git_branch",
		"dockerfile_path", "domain", "port", "replicas", "env_encrypted", "status",
		"cpu_limit", "memory_limit", "health_check_path", "health_check_interval",
		"homepage_labels", "extra_labels", "placement_constraints", "placement_preferences",
		"update_strategy", "update_parallelism", "update_delay", "update_failure_action", "update_order",
		"build_cache_enabled", "auto_deploy_branch", "preview_environments",
		"template_name", "template_version", "created_at", "updated_at",
	}
}

func appGetColumns() []string {
	return []string{
		"id", "project_id", "name", "deploy_type", "image", "git_repo", "git_branch",
		"dockerfile_path", "domain", "port", "replicas", "env_encrypted", "status",
		"cpu_limit", "memory_limit", "health_check_path", "health_check_interval",
		"homepage_labels", "extra_labels", "placement_constraints", "placement_preferences",
		"update_strategy", "update_parallelism", "update_delay", "update_failure_action", "update_order",
		"template_name", "template_version", "created_at", "updated_at",
	}
}

func volumeColumns() []string {
	return []string{
		"id", "project_id", "name", "driver", "driver_opts", "labels", "mount_type",
		"remote_host", "remote_path", "mount_options", "scope", "status",
		"storage_host_id", "local_path", "ceph_pool", "ceph_image", "ceph_fs_name", "created_at",
	}
}

// --- Projects ---

func TestCreateProject(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	mock.ExpectQuery(`INSERT INTO project`).
		WithArgs("my-project", "org-1", "desc").
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).
			AddRow("proj-1", now, now))

	p := &Project{Name: "my-project", OrgID: "org-1", Description: "desc"}
	err := s.CreateProject(context.Background(), p)
	require.NoError(t, err)
	assert.Equal(t, "proj-1", p.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetProject(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	mock.ExpectQuery(`SELECT .+ FROM project WHERE`).
		WithArgs("proj-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "org_id", "description", "created_at", "updated_at"}).
			AddRow("proj-1", "my-project", "org-1", "desc", now, now))

	p, err := s.GetProject(context.Background(), "proj-1")
	require.NoError(t, err)
	assert.Equal(t, "my-project", p.Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestListProjects(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	mock.ExpectQuery(`SELECT .+ FROM project WHERE org_id`).
		WithArgs("org-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "org_id", "description", "created_at", "updated_at"}).
			AddRow("p1", "proj-a", "org-1", "", now, now).
			AddRow("p2", "proj-b", "org-1", "", now, now))

	projects, err := s.ListProjects(context.Background(), "org-1")
	require.NoError(t, err)
	assert.Len(t, projects, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDeleteProject(t *testing.T) {
	s, mock := newMock(t)
	mock.ExpectExec(`DELETE FROM project`).
		WithArgs("proj-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := s.DeleteProject(context.Background(), "proj-1")
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Apps ---

func TestCreateApp(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	mock.ExpectQuery(`INSERT INTO app`).
		WithArgs("proj-1", "my-app", "image", "nginx:latest", "", "", "Dockerfile", "app.local", 8080, 1, []byte(nil), "", "").
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow("app-1", now, now))

	a := &App{ProjectID: "proj-1", Name: "my-app", DeployType: "image", Image: "nginx:latest", DockerfilePath: "Dockerfile", Domain: "app.local", Port: 8080, Replicas: 1}
	err := s.CreateApp(context.Background(), a)
	require.NoError(t, err)
	assert.Equal(t, "app-1", a.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetApp(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	mock.ExpectQuery(`SELECT .+ FROM app WHERE`).
		WithArgs("app-1").
		WillReturnRows(sqlmock.NewRows(appGetColumns()).
			AddRow("app-1", "proj-1", "my-app", "image", "nginx:latest", "", "main", "Dockerfile", "", 3000, 1, nil, "running", 0.0, int64(0), "", 30, []byte("{}"), []byte("{}"), []byte("[]"), []byte("[]"), "rolling", 1, "5s", "rollback", "stop-first", "", "", now, now))

	a, err := s.GetApp(context.Background(), "app-1")
	require.NoError(t, err)
	assert.Equal(t, "my-app", a.Name)
	assert.Equal(t, "running", a.Status)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestListApps(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	mock.ExpectQuery(`SELECT .+ FROM app WHERE project_id`).
		WithArgs("proj-1").
		WillReturnRows(sqlmock.NewRows(appColumns()).
			AddRow("a1", "proj-1", "app-a", "image", "nginx", "", "", "", "", 80, 1, nil, "running", 0.0, int64(0), "", 30, []byte("{}"), []byte("{}"), []byte("[]"), []byte("[]"), "rolling", 1, "5s", "rollback", "stop-first", true, "main", false, "", "", now, now))

	apps, err := s.ListApps(context.Background(), "proj-1")
	require.NoError(t, err)
	assert.Len(t, apps, 1)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateAppStatus(t *testing.T) {
	s, mock := newMock(t)
	mock.ExpectExec(`UPDATE app SET status`).
		WithArgs("deploying", "app-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := s.UpdateAppStatus(context.Background(), "app-1", "deploying")
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateAppEnv(t *testing.T) {
	s, mock := newMock(t)
	env := []byte("encrypted-env")
	mock.ExpectExec(`UPDATE app SET env_encrypted`).
		WithArgs(env, "app-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := s.UpdateAppEnv(context.Background(), "app-1", env)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateAppDomain(t *testing.T) {
	s, mock := newMock(t)
	mock.ExpectExec(`UPDATE app SET domain`).
		WithArgs("new.domain.com", "app-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := s.UpdateAppDomain(context.Background(), "app-1", "new.domain.com")
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDeleteApp(t *testing.T) {
	s, mock := newMock(t)
	mock.ExpectExec(`DELETE FROM app`).
		WithArgs("app-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := s.DeleteApp(context.Background(), "app-1")
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Deployments ---

func TestCreateDeployment(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	mock.ExpectQuery(`INSERT INTO deployment`).
		WithArgs("app-1", "building", "", "", "").
		WillReturnRows(sqlmock.NewRows([]string{"id", "started_at"}).AddRow("dep-1", now))

	d := &Deployment{AppID: "app-1", Status: "building"}
	err := s.CreateDeployment(context.Background(), d)
	require.NoError(t, err)
	assert.Equal(t, "dep-1", d.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateDeployment(t *testing.T) {
	s, mock := newMock(t)
	mock.ExpectExec(`UPDATE deployment SET status`).
		WithArgs("success", "build complete", "dep-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := s.UpdateDeployment(context.Background(), "dep-1", "success", "build complete")
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestListDeployments(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	mock.ExpectQuery(`SELECT .+ FROM deployment WHERE app_id`).
		WithArgs("app-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "app_id", "status", "commit_sha", "image_digest", "logs", "started_at", "finished_at"}).
			AddRow("dep-1", "app-1", "success", "abc123", "sha256:xxx", "ok", now, now))

	deps, err := s.ListDeployments(context.Background(), "app-1")
	require.NoError(t, err)
	assert.Len(t, deps, 1)
	assert.Equal(t, "success", deps[0].Status)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Managed Databases ---

func TestCreateManagedDatabase(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	mock.ExpectQuery(`INSERT INTO managed_database`).
		WithArgs("proj-1", "mydb", "postgres", "16", []byte(nil)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow("db-1", now))

	d := &ManagedDatabase{ProjectID: "proj-1", Name: "mydb", DBType: "postgres", Version: "16"}
	err := s.CreateManagedDatabase(context.Background(), d)
	require.NoError(t, err)
	assert.Equal(t, "db-1", d.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestListManagedDatabases(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	mock.ExpectQuery(`SELECT .+ FROM managed_database WHERE project_id`).
		WithArgs("proj-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "db_type", "version", "status", "connection_encrypted", "created_at"}).
			AddRow("db-1", "proj-1", "mydb", "postgres", "16", "running", nil, now))

	dbs, err := s.ListManagedDatabases(context.Background(), "proj-1")
	require.NoError(t, err)
	assert.Len(t, dbs, 1)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateManagedDatabaseStatus(t *testing.T) {
	s, mock := newMock(t)
	mock.ExpectExec(`UPDATE managed_database SET status`).
		WithArgs("running", "db-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := s.UpdateManagedDatabaseStatus(context.Background(), "db-1", "running")
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Secrets ---

func TestCreateSecret(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	mock.ExpectQuery(`INSERT INTO secret`).
		WithArgs("proj-1", "db-password", "docker-id", "my db pass").
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow("sec-1", now, now))

	sec := &Secret{ProjectID: "proj-1", Name: "db-password", DockerSecretID: "docker-id", Description: "my db pass"}
	err := s.CreateSecret(context.Background(), sec)
	require.NoError(t, err)
	assert.Equal(t, "sec-1", sec.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestListSecrets(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	mock.ExpectQuery(`SELECT .+ FROM secret WHERE project_id`).
		WithArgs("proj-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "docker_secret_id", "description", "created_at", "updated_at"}).
			AddRow("sec-1", "proj-1", "db-password", "did-1", "desc", now, now))

	secrets, err := s.ListSecrets(context.Background(), "proj-1")
	require.NoError(t, err)
	assert.Len(t, secrets, 1)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetSecret(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	mock.ExpectQuery(`SELECT .+ FROM secret WHERE id`).
		WithArgs("sec-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "project_id", "name", "docker_secret_id", "description", "created_at", "updated_at"}).
			AddRow("sec-1", "proj-1", "db-password", "did-1", "desc", now, now))

	sec, err := s.GetSecret(context.Background(), "sec-1")
	require.NoError(t, err)
	assert.Equal(t, "db-password", sec.Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDeleteSecret(t *testing.T) {
	s, mock := newMock(t)
	mock.ExpectExec(`DELETE FROM secret`).
		WithArgs("sec-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := s.DeleteSecret(context.Background(), "sec-1")
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAttachSecret(t *testing.T) {
	s, mock := newMock(t)
	mock.ExpectExec(`INSERT INTO app_secret`).
		WithArgs("app-1", "sec-1", "target.txt", "0", "0", 292).
		WillReturnResult(sqlmock.NewResult(0, 1))

	as := &AppSecret{AppID: "app-1", SecretID: "sec-1", Target: "target.txt", UID: "0", GID: "0", Mode: 292}
	err := s.AttachSecret(context.Background(), as)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDetachSecret(t *testing.T) {
	s, mock := newMock(t)
	mock.ExpectExec(`DELETE FROM app_secret`).
		WithArgs("app-1", "sec-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := s.DetachSecret(context.Background(), "app-1", "sec-1")
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestListAppSecrets(t *testing.T) {
	s, mock := newMock(t)
	mock.ExpectQuery(`SELECT .+ FROM app_secret WHERE app_id`).
		WithArgs("app-1").
		WillReturnRows(sqlmock.NewRows([]string{"app_id", "secret_id", "target", "uid", "gid", "mode"}).
			AddRow("app-1", "sec-1", "password.txt", "0", "0", 292))

	result, err := s.ListAppSecrets(context.Background(), "app-1")
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "password.txt", result[0].Target)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Volumes ---

func TestCreateVolume(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	mock.ExpectQuery(`INSERT INTO volume`).
		WithArgs("proj-1", "data-vol", "local", json.RawMessage("{}"), json.RawMessage("{}"), "volume", "", "", "", "local", "active",
			"", "", "", "", "").
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow("vol-1", now))

	v := &Volume{ProjectID: "proj-1", Name: "data-vol", Driver: "local", DriverOpts: json.RawMessage("{}"), Labels: json.RawMessage("{}"), MountType: "volume", Scope: "local", Status: "active"}
	err := s.CreateVolume(context.Background(), v)
	require.NoError(t, err)
	assert.Equal(t, "vol-1", v.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestListVolumes(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	mock.ExpectQuery(`SELECT .+ FROM volume WHERE project_id`).
		WithArgs("proj-1").
		WillReturnRows(sqlmock.NewRows(volumeColumns()).
			AddRow("vol-1", "proj-1", "data-vol", "local", json.RawMessage("{}"), json.RawMessage("{}"), "volume", "", "", "", "local", "active",
				"", "", "", "", "", now))

	vols, err := s.ListVolumes(context.Background(), "proj-1")
	require.NoError(t, err)
	assert.Len(t, vols, 1)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGetVolume(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	mock.ExpectQuery(`SELECT .+ FROM volume WHERE id`).
		WithArgs("vol-1").
		WillReturnRows(sqlmock.NewRows(volumeColumns()).
			AddRow("vol-1", "proj-1", "data-vol", "local", json.RawMessage("{}"), json.RawMessage("{}"), "nfs", "nas.local", "/share", "vers=4", "local", "active",
				"", "", "", "", "", now))

	v, err := s.GetVolume(context.Background(), "vol-1")
	require.NoError(t, err)
	assert.Equal(t, "nas.local", v.RemoteHost)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateVolumeStatus(t *testing.T) {
	s, mock := newMock(t)
	mock.ExpectExec(`UPDATE volume SET status`).
		WithArgs("active", "vol-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := s.UpdateVolumeStatus(context.Background(), "vol-1", "active")
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDeleteVolume(t *testing.T) {
	s, mock := newMock(t)
	mock.ExpectExec(`DELETE FROM volume`).
		WithArgs("vol-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := s.DeleteVolume(context.Background(), "vol-1")
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestAttachVolume(t *testing.T) {
	s, mock := newMock(t)
	mock.ExpectExec(`INSERT INTO app_volume`).
		WithArgs("app-1", "vol-1", "/data", false).
		WillReturnResult(sqlmock.NewResult(0, 1))

	av := &AppVolume{AppID: "app-1", VolumeID: "vol-1", ContainerPath: "/data", ReadOnly: false}
	err := s.AttachVolume(context.Background(), av)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDetachVolume(t *testing.T) {
	s, mock := newMock(t)
	mock.ExpectExec(`DELETE FROM app_volume`).
		WithArgs("app-1", "vol-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := s.DetachVolume(context.Background(), "app-1", "vol-1")
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestListAppVolumes(t *testing.T) {
	s, mock := newMock(t)
	mock.ExpectQuery(`SELECT .+ FROM app_volume WHERE app_id`).
		WithArgs("app-1").
		WillReturnRows(sqlmock.NewRows([]string{"app_id", "volume_id", "container_path", "read_only"}).
			AddRow("app-1", "vol-1", "/data", false))

	result, err := s.ListAppVolumes(context.Background(), "app-1")
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "/data", result[0].ContainerPath)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Git Sources ---

func TestCreateGitSource(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	mock.ExpectQuery(`INSERT INTO git_source`).
		WithArgs("org-1", "github", []byte("encrypted-token")).
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow("gs-1", now))

	gs := &GitSource{OrgID: "org-1", Provider: "github", TokenEncrypted: []byte("encrypted-token")}
	err := s.CreateGitSource(context.Background(), gs)
	require.NoError(t, err)
	assert.Equal(t, "gs-1", gs.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestListGitSources(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	mock.ExpectQuery(`SELECT .+ FROM git_source WHERE org_id`).
		WithArgs("org-1").
		WillReturnRows(sqlmock.NewRows([]string{"id", "org_id", "provider", "token_encrypted", "created_at"}).
			AddRow("gs-1", "org-1", "github", []byte("token"), now))

	sources, err := s.ListGitSources(context.Background(), "org-1")
	require.NoError(t, err)
	assert.Len(t, sources, 1)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Backup ---

func TestCreateBackupConfig(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	mock.ExpectQuery(`INSERT INTO backup_config`).
		WithArgs("res-1", "0 2 * * *", "my-bucket", "backups/", "database", "").
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow("bc-1", now))

	bc := &BackupConfig{ResourceID: "res-1", Schedule: "0 2 * * *", S3Bucket: "my-bucket", S3Prefix: "backups/"}
	err := s.CreateBackupConfig(context.Background(), bc)
	require.NoError(t, err)
	assert.Equal(t, "bc-1", bc.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Audit Log ---

func TestCreateAuditLog(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	mock.ExpectQuery(`INSERT INTO audit_log`).
		WithArgs("user-1", "org-1", "create", "app", "app-1", "created app").
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow("al-1", now))

	al := &AuditLog{UserID: "user-1", OrgID: "org-1", Action: "create", Resource: "app", ResourceID: "app-1", Details: "created app"}
	err := s.CreateAuditLog(context.Background(), al)
	require.NoError(t, err)
	assert.Equal(t, "al-1", al.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestListAuditLogs(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	mock.ExpectQuery(`SELECT .+ FROM audit_log WHERE org_id`).
		WithArgs("org-1", 50).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "org_id", "action", "resource", "resource_id", "details", "created_at"}).
			AddRow("al-1", "user-1", "org-1", "create", "app", "app-1", "details", now))

	logs, err := s.ListAuditLogs(context.Background(), "org-1", 0)
	require.NoError(t, err)
	assert.Len(t, logs, 1)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestListAuditLogsDefaultLimit(t *testing.T) {
	s, mock := newMock(t)
	mock.ExpectQuery(`SELECT .+ FROM audit_log WHERE org_id`).
		WithArgs("org-1", 50).
		WillReturnRows(sqlmock.NewRows([]string{"id", "user_id", "org_id", "action", "resource", "resource_id", "details", "created_at"}))

	logs, err := s.ListAuditLogs(context.Background(), "org-1", -1)
	require.NoError(t, err)
	assert.Empty(t, logs)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Ceph Clusters ---

func TestCreateCephCluster(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	mock.ExpectQuery(`INSERT INTO ceph_cluster`).
		WithArgs("test-ceph", "", "pending", "node-1", "{}", "", "",
			[]byte(nil), []byte(nil), 3, "").
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at", "updated_at"}).
			AddRow("cc-1", now, now))

	c := &CephCluster{Name: "test-ceph", Status: "pending", BootstrapNodeID: "node-1", ReplicationSize: 3}
	err := s.CreateCephCluster(context.Background(), c)
	require.NoError(t, err)
	assert.Equal(t, "cc-1", c.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func cephClusterColumns() []string {
	return []string{"id", "name", "fsid", "status", "bootstrap_node_id", "mon_hosts",
		"public_network", "cluster_network", "ceph_conf_encrypted", "admin_keyring_encrypted",
		"replication_size", "storage_host_id", "created_at", "updated_at"}
}

func TestGetCephCluster(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	mock.ExpectQuery(`SELECT .+ FROM ceph_cluster WHERE id`).
		WithArgs("cc-1").
		WillReturnRows(sqlmock.NewRows(cephClusterColumns()).
			AddRow("cc-1", "test-ceph", "fsid-123", "healthy", "node-1",
				[]byte(`{10.0.0.1,10.0.0.2}`), "10.0.0.0/24", "", nil, nil, 3, "", now, now))

	c, err := s.GetCephCluster(context.Background(), "cc-1")
	require.NoError(t, err)
	assert.Equal(t, "test-ceph", c.Name)
	assert.Equal(t, "fsid-123", c.FSID)
	assert.Equal(t, "healthy", c.Status)
	assert.Len(t, c.MonHosts, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestListCephClusters(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	listCols := []string{"id", "name", "fsid", "status", "bootstrap_node_id", "mon_hosts",
		"public_network", "cluster_network", "replication_size", "storage_host_id", "created_at", "updated_at"}
	mock.ExpectQuery(`SELECT .+ FROM ceph_cluster ORDER BY`).
		WillReturnRows(sqlmock.NewRows(listCols).
			AddRow("cc-1", "test-ceph", "fsid-123", "healthy", "node-1", []byte(`{10.0.0.1}`), "", "", 3, "", now, now))

	clusters, err := s.ListCephClusters(context.Background())
	require.NoError(t, err)
	assert.Len(t, clusters, 1)
	assert.Equal(t, "test-ceph", clusters[0].Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestUpdateCephClusterStatus(t *testing.T) {
	s, mock := newMock(t)
	mock.ExpectExec(`UPDATE ceph_cluster SET status`).
		WithArgs("healthy", "cc-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := s.UpdateCephClusterStatus(context.Background(), "cc-1", "healthy")
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDeleteCephCluster(t *testing.T) {
	s, mock := newMock(t)
	mock.ExpectExec(`DELETE FROM ceph_cluster`).
		WithArgs("cc-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := s.DeleteCephCluster(context.Background(), "cc-1")
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Ceph OSDs ---

func TestCreateCephOSD(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	mock.ExpectQuery(`INSERT INTO ceph_osd`).
		WithArgs("cc-1", "node-1", "host1", nil, "/dev/sdb", int64(500000000000), "ssd", "pending").
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow("osd-1", now))

	o := &CephOSD{ClusterID: "cc-1", NodeID: "node-1", Hostname: "host1", DevicePath: "/dev/sdb", DeviceSize: 500000000000, DeviceType: "ssd", Status: "pending"}
	err := s.CreateCephOSD(context.Background(), o)
	require.NoError(t, err)
	assert.Equal(t, "osd-1", o.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestListCephOSDs(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	osdID := 0
	cols := []string{"id", "cluster_id", "node_id", "hostname", "osd_id", "device_path", "device_size", "device_type", "status", "created_at"}
	mock.ExpectQuery(`SELECT .+ FROM ceph_osd WHERE cluster_id`).
		WithArgs("cc-1").
		WillReturnRows(sqlmock.NewRows(cols).
			AddRow("osd-1", "cc-1", "node-1", "host1", &osdID, "/dev/sdb", int64(500000000000), "ssd", "active", now))

	osds, err := s.ListCephOSDs(context.Background(), "cc-1")
	require.NoError(t, err)
	assert.Len(t, osds, 1)
	assert.Equal(t, "/dev/sdb", osds[0].DevicePath)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDeleteCephOSD(t *testing.T) {
	s, mock := newMock(t)
	mock.ExpectExec(`DELETE FROM ceph_osd`).
		WithArgs("osd-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := s.DeleteCephOSD(context.Background(), "osd-1")
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Ceph Pools ---

func TestCreateCephPool(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	mock.ExpectQuery(`INSERT INTO ceph_pool`).
		WithArgs("cc-1", "hive-rbd", nil, 32, 3, "replicated", "rbd").
		WillReturnRows(sqlmock.NewRows([]string{"id", "created_at"}).AddRow("pool-1", now))

	p := &CephPool{ClusterID: "cc-1", Name: "hive-rbd", PGNum: 32, Size: 3, Type: "replicated", Application: "rbd"}
	err := s.CreateCephPool(context.Background(), p)
	require.NoError(t, err)
	assert.Equal(t, "pool-1", p.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestListCephPools(t *testing.T) {
	s, mock := newMock(t)
	now := time.Now()
	poolID := 1
	cols := []string{"id", "cluster_id", "name", "pool_id", "pg_num", "size", "type", "application", "created_at"}
	mock.ExpectQuery(`SELECT .+ FROM ceph_pool WHERE cluster_id`).
		WithArgs("cc-1").
		WillReturnRows(sqlmock.NewRows(cols).
			AddRow("pool-1", "cc-1", "hive-rbd", &poolID, 32, 3, "replicated", "rbd", now))

	pools, err := s.ListCephPools(context.Background(), "cc-1")
	require.NoError(t, err)
	assert.Len(t, pools, 1)
	assert.Equal(t, "hive-rbd", pools[0].Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestDeleteCephPool(t *testing.T) {
	s, mock := newMock(t)
	mock.ExpectExec(`DELETE FROM ceph_pool`).
		WithArgs("pool-1").
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := s.DeleteCephPool(context.Background(), "pool-1")
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// --- Helper function tests ---

func TestPqStringArray(t *testing.T) {
	assert.Equal(t, "{}", pqStringArray(nil))
	assert.Equal(t, "{}", pqStringArray([]string{}))
	assert.Equal(t, `{"10.0.0.1"}`, pqStringArray([]string{"10.0.0.1"}))
	assert.Equal(t, `{"10.0.0.1","10.0.0.2"}`, pqStringArray([]string{"10.0.0.1", "10.0.0.2"}))
}

func TestParsePqStringArray(t *testing.T) {
	assert.Nil(t, parsePqStringArray([]byte("{}")))
	assert.Nil(t, parsePqStringArray([]byte("")))
	assert.Equal(t, []string{"10.0.0.1"}, parsePqStringArray([]byte(`{10.0.0.1}`)))
	assert.Equal(t, []string{"10.0.0.1", "10.0.0.2"}, parsePqStringArray([]byte(`{10.0.0.1,10.0.0.2}`)))
}
