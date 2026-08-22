package riverjobs

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/riverqueue/river"

	"github.com/luke/hive/control-plane/internal/testdb"
)

func TestDeployWorkerInvalidDeploymentID(t *testing.T) {
	testdb.Get(t)
	w := &DeployWorker{}
	err := w.Work(context.Background(), &river.Job[DeployJobArgs]{
		Args: DeployJobArgs{DeploymentID: "nope"},
	})
	if err == nil || !strings.Contains(err.Error(), `invalid deployment id "nope"`) {
		t.Fatalf("expected invalid deployment id error, got %v", err)
	}
}

func TestDeployWorkerMissingDeployment(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	w := &DeployWorker{Pool: pool, Swarm: newFakeSwarm()}
	err := w.Work(workCtx(t), &river.Job[DeployJobArgs]{
		Args: DeployJobArgs{DeploymentID: uuid.NewString()},
	})
	if !isJobCancel(t, err) {
		t.Fatalf("expected JobCancel for missing deployment, got %v", err)
	}
}

func TestDeployWorkerHappyPath(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "web", "", nil)
	depID := seedDeployment(t, appID, "127.0.0.1:5000/p/web:abc123", "pending", "manual")

	// One plain env var and one secret-backed one.
	if _, err := pool.Exec(context.Background(), `
		insert into app_env_vars(application_id, key, value, is_secret, secret_version) values
			($1::uuid, 'LOG_LEVEL', 'debug', false, 1),
			($1::uuid, 'API_TOKEN', null, true, 3)
	`, appID); err != nil {
		t.Fatalf("seed env vars: %v", err)
	}

	swarm := newFakeSwarm()
	fanout := &fakeFanout{}
	w := &DeployWorker{Pool: pool, Swarm: swarm, Fanout: fanout}
	err := w.Work(workCtx(t), &river.Job[DeployJobArgs]{Args: DeployJobArgs{DeploymentID: depID}})
	if err != nil {
		t.Fatalf("Work: %v", err)
	}

	var status string
	if err := pool.QueryRow(context.Background(),
		`select status from deployments where id=$1::uuid`, depID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "deployed" {
		t.Fatalf("deployment status = %q, want deployed", status)
	}

	swarm.mu.Lock()
	if len(swarm.createdSpecs) != 1 {
		t.Fatalf("created services = %d, want 1", len(swarm.createdSpecs))
	}
	spec := swarm.createdSpecs[0]
	swarm.mu.Unlock()

	env := map[string]bool{}
	for _, e := range spec.TaskTemplate.ContainerSpec.Env {
		env[e] = true
	}
	if !env["LOG_LEVEL=debug"] {
		t.Fatalf("plain env var missing from service env: %v", spec.TaskTemplate.ContainerSpec.Env)
	}
	if len(spec.TaskTemplate.ContainerSpec.Secrets) != 1 {
		t.Fatalf("secret refs = %+v, want exactly the API_TOKEN secret", spec.TaskTemplate.ContainerSpec.Secrets)
	} else if spec.TaskTemplate.ContainerSpec.Secrets[0].SecretName == "" {
		t.Fatalf("secret ref has no name: %+v", spec.TaskTemplate.ContainerSpec.Secrets[0])
	}
	if spec.Labels["hive.app.id"] != appID {
		t.Fatalf("service label hive.app.id = %q, want %q", spec.Labels["hive.app.id"], appID)
	}

	msgs := fanout.got()
	if len(msgs) != 1 || msgs[0][0] != "deployment:"+appID || msgs[0][1] != "deployed" {
		t.Fatalf("fanout messages = %v, want [deployment:%s deployed]", msgs, appID)
	}
}

func TestDeployWorkerAppliesDomains(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "routed", "", nil)
	depID := seedDeployment(t, appID, "img", "pending", "manual")
	if _, err := pool.Exec(context.Background(),
		`insert into domains(application_id, hostname, tls_enabled) values ($1::uuid, $2, true)`,
		appID, "routed.example.com"); err != nil {
		t.Fatalf("seed domain: %v", err)
	}

	swarm := newFakeSwarm()
	w := &DeployWorker{Pool: pool, Swarm: swarm}
	if err := w.Work(workCtx(t), &river.Job[DeployJobArgs]{Args: DeployJobArgs{DeploymentID: depID}}); err != nil {
		t.Fatalf("Work: %v", err)
	}

	swarm.mu.Lock()
	updated := append([]string(nil), swarm.updatedIDs...)
	swarm.mu.Unlock()
	if len(updated) == 0 {
		t.Fatal("expected a domain-label UpdateService call for the routed domain")
	}
}

func TestDeployWorkerCreateServiceFailureMarksFailed(t *testing.T) {
	pool := testdb.Get(t)
	testdb.TruncateAll(t)
	fixture := testdb.SeedOrg(t)
	appID := testdb.SeedApplication(t, fixture.ProjectID, "broken", "", nil)
	depID := seedDeployment(t, appID, "img", "pending", "manual")

	swarm := newFakeSwarm()
	swarm.createErr = errors.New("swarm unavailable")
	w := &DeployWorker{Pool: pool, Swarm: swarm}
	err := w.Work(workCtx(t), &river.Job[DeployJobArgs]{Args: DeployJobArgs{DeploymentID: depID}})
	if err == nil || !strings.Contains(err.Error(), "swarm unavailable") {
		t.Fatalf("expected create failure to propagate, got %v", err)
	}

	var status string
	if err := pool.QueryRow(context.Background(),
		`select status from deployments where id=$1::uuid`, depID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "failed" {
		t.Fatalf("deployment status = %q, want failed", status)
	}
}
