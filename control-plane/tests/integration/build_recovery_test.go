//go:build integration
// +build integration

package integration

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/swarm"
	dockerclient "github.com/moby/moby/client"
)

// TestBuildkitRecovery proves a git build survives a BuildKit outage: the
// first solve attempt fails while buildkit is scaled to zero, River retries
// the job (MaxAttempts=3), and once buildkit is back the build completes and
// the image is queryable in the registry catalog.
func TestBuildkitRecovery(t *testing.T) {
	baseURL := getenv("HIVE_API_BASE_URL", "http://127.0.0.1:3000")
	auth := bootstrapAuthContext(t, baseURL)
	cli := dindClient(t)
	pool := testDB(t)
	stackName := getenv("STACK_NAME", "hive")
	ctx := context.Background()

	// BuildKit runs as a privileged host-level container (seeded by the CI
	// prereqs script or hivectl). Wait until it accepts connections from
	// the control-plane's network position; skip when the deployment has no
	// reachable builder. Derive host:port from the control-plane's
	// BUILDKIT_ADDR (default tcp://buildkit:1234).
	bkHost, bkPort := "buildkit", "1234"
	if addr := getenv("BUILDKIT_ADDR", ""); strings.HasPrefix(addr, "tcp://") {
		rest := strings.TrimPrefix(addr, "tcp://")
		if h, p, err := net.SplitHostPort(rest); err == nil {
			bkHost, bkPort = h, p
		}
	}
	cpContainer := pickRunningContainer(t, cli, stackName+"_control-plane")
	pollUntil(t, 120*time.Second, 3*time.Second, "buildkit reachable on tcp/"+bkPort, func() error {
		_, err := containerExec(ctx, cli, cpContainer, []string{"nc", "-z", "-w", "2", bkHost, bkPort})
		return err
	})

	// Spin up an isolated git daemon on hive_internal serving a tiny repo
	// with a Dockerfile, so the build never depends on external hosts.
	gitHost := fmt.Sprintf("hive-ci-gitdaemon-%d", time.Now().UnixNano())

	appName := "recovery-app-" + fmt.Sprintf("%d", time.Now().UnixNano())
	projectRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/projects", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]any{"name": "recovery-project-" + fmt.Sprintf("%d", time.Now().UnixNano())}, &projectRes, http.StatusCreated); err != nil {
		t.Fatalf("create project failed: %v", err)
	}
	projectID := asString(projectRes["id"])

	appRes := map[string]any{}
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/applications", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]any{
		"projectId":     projectID,
		"name":          appName,
		"sourceType":    "git",
		"repositoryUrl": fmt.Sprintf("git://%s/repo.git", gitHost),
		"gitRef":        "main",
	}, &appRes, http.StatusCreated); err != nil {
		t.Fatalf("create git app failed: %v", err)
	}
	appID := asString(appRes["id"])

	// The git daemon deliberately does NOT exist yet at this point, so the
	// first attempt is guaranteed to fail at the clone stage. Starting it
	// afterwards exercises River's retry path recovering from an outage.
	if err := authedPostJSONWithHeaders(baseURL+"/api/v1/applications/"+appID+"/deploy", auth.AccessToken, map[string]string{
		"X-Organization-Id": auth.OrganizationID,
	}, map[string]any{}, &map[string]any{}, http.StatusAccepted); err != nil {
		t.Fatalf("enqueue deploy failed: %v", err)
	}

	var buildID string
	pollUntil(t, 30*time.Second, time.Second, "build job enqueued", func() error {
		return pool.QueryRow(ctx,
			`select id::text from build_jobs where application_id = $1::uuid order by created_at desc limit 1`, appID,
		).Scan(&buildID)
	})

	// Wait for the first attempt to fail at clone (river records attempts
	// on the river_job row), then bring the git daemon online so the retry
	// succeeds.
	pollUntil(t, 120*time.Second, 2*time.Second, "first build attempt failed and retry scheduled", func() error {
		var attempts int
		var state string
		err := pool.QueryRow(ctx,
			`select attempt, state::text from river_job where kind = 'build' and args->>'build_id' = $1 order by id desc limit 1`,
			buildID).Scan(&attempts, &state)
		if err != nil {
			return err
		}
		if attempts < 1 {
			return fmt.Errorf("no failed attempt recorded yet (attempts=%d state=%s)", attempts, state)
		}
		return nil
	})
	// NOW start the git daemon so the retried attempt can clone and build.
	startGitDaemon(t, cli, gitHost)

	// The retried build must reach terminal complete within the retry
	// budget (MaxAttempts=3).
	var imageTag string
	pollUntil(t, 240*time.Second, 3*time.Second, "build job completing after retry", func() error {
		var status string
		if err := pool.QueryRow(ctx,
			`select coalesce(status::text,''), coalesce(image_tag,'') from build_jobs where id = $1::uuid`, buildID,
		).Scan(&status, &imageTag); err != nil {
			return err
		}
		switch status {
		case "complete":
			return nil
		case "failed":
			return fmt.Errorf("build exhausted retries and failed")
		default:
			return fmt.Errorf("status=%s", status)
		}
	})
	if imageTag == "" {
		t.Fatalf("completed build %s has no image tag", buildID)
	}

	var attempts int
	if err := pool.QueryRow(ctx,
		`select attempt from river_job where kind = 'build' and args->>'build_id' = $1 order by id desc limit 1`, buildID,
	).Scan(&attempts); err != nil || attempts < 2 {
		t.Fatalf("expected >=2 river attempts after forced failure (got %d, err=%v)", attempts, err)
	}

	// The pushed image must be queryable in the registry catalog:
	// /v2/{project}/{app}/tags/list. The registry is only reachable from
	// inside the overlay, so query it through a control-plane task.
	catalogPath := fmt.Sprintf("/v2/%s/%s/tags/list", projectID, appName)
	out, err := containerExec(ctx, cli, cpContainer, []string{"wget", "-qO-", "http://registry:5000" + catalogPath})
	if err != nil {
		t.Fatalf("registry catalog query %s failed: %v (%s)", catalogPath, err, out)
	}
	var catalog struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(out), &catalog); err != nil {
		t.Fatalf("decode catalog response failed: %v (%s)", err, out)
	}
	if len(catalog.Tags) == 0 {
		t.Fatalf("registry catalog %s has no tags: %s", catalogPath, out)
	}
	imageTagParts := strings.Split(imageTag, ":")
	if len(imageTagParts) < 2 || !strings.Contains(out, imageTagParts[len(imageTagParts)-1]) {
		t.Fatalf("catalog response for %s does not contain tag %q: %s", catalogPath, imageTag, out)
	}
}

// pickRunningContainer returns one running task container of the service.
func pickRunningContainer(t *testing.T, cli *dockerclient.Client, serviceName string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ids, err := serviceTaskContainers(ctx, cli, serviceName)
	if err != nil || len(ids) == 0 {
		t.Skipf("no running task container for %s: %v", serviceName, err)
	}
	return ids[0]
}

// startGitDaemon runs a throwaway git daemon container on hive_internal
// serving /repo.git with a minimal Dockerfile on branch main.
func startGitDaemon(t *testing.T, cli *dockerclient.Client, name string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 240*time.Second)
	defer cancel()

	dockerfile := strings.Join([]string{
		"FROM alpine:3.21",
		"RUN apk add --no-cache git git-daemon",
		"RUN mkdir -p /srv/git && \\",
		"    git init --bare -b main /srv/git/repo.git && \\",
		"    cd /tmp && git init -b main work && cd work && \\",
		"    git config user.email ci@example.com && git config user.name ci && \\",
		"    printf 'FROM alpine:3.21\\nCMD [\"sleep\", \"3600\"]\\n' > Dockerfile && \\",
		"    echo hello > README.md && \\",
		"    git add -A && git commit -m init && \\",
		"    git push /srv/git/repo.git main",
		"EXPOSE 9418",
		`CMD ["git", "daemon", "--base-path=/srv/git", "--export-all", "--reuseaddr", "--listen=0.0.0.0"]`,
	}, "\n")

	buildCtx := &bytes.Buffer{}
	tw := tar.NewWriter(buildCtx)
	dfBytes := []byte(dockerfile)
	if err := tw.WriteHeader(&tar.Header{Name: "Dockerfile", Mode: 0o644, Size: int64(len(dfBytes))}); err != nil {
		t.Fatalf("tar dockerfile header: %v", err)
	}
	if _, err := tw.Write(dfBytes); err != nil {
		t.Fatalf("tar dockerfile body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}

	imageTag := name + ":latest"
	resp, err := cli.ImageBuild(ctx, buildCtx, dockerclient.ImageBuildOptions{
		Dockerfile: "Dockerfile",
		Tags:       []string{imageTag},
	})
	if err != nil {
		t.Fatalf("build git daemon image failed: %v", err)
	}
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatalf("drain build output: %v", err)
	}
	resp.Body.Close()

	// A one-replica swarm SERVICE (not a standalone container): task
	// containers resolve service names via the overlay's embedded DNS,
	// while plain container names are not resolvable from tasks.
	one := uint64(1)
	spec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name:   name,
			Labels: map[string]string{"hive.ci.test": "buildkit-recovery"},
		},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{Image: imageTag},
			// The image was built on the node backing DOCKER_HOST (the
			// ci-labeled manager); pin the daemon there so the image exists.
			Placement: &swarm.Placement{Constraints: []string{"node.labels.ci==true"}},
			Networks:  []swarm.NetworkAttachmentConfig{{Target: "hive_internal"}},
		},
		Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &one}},
	}
	if _, err := cli.ServiceCreate(ctx, dockerclient.ServiceCreateOptions{Spec: spec}); err != nil {
		t.Fatalf("create git daemon service failed: %v", err)
	}
	t.Cleanup(func() {
		rmCtx, rmCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer rmCancel()
		_, _ = cli.ServiceRemove(rmCtx, name, dockerclient.ServiceRemoveOptions{})
	})
	// Wait until the daemon answers from the control-plane's network
	// position before handing the URL to the build.
	cpContainer := pickRunningContainer(t, cli, getenv("STACK_NAME", "hive")+"_control-plane")
	pollUntil(t, 90*time.Second, 2*time.Second, "git daemon answering on tcp/9418", func() error {
		out, err := containerExec(ctx, cli, cpContainer, []string{
			"nc", "-z", "-w", "2", name, "9418",
		})
		if err != nil {
			return fmt.Errorf("tcp probe failed (out=%s): %w", out, err)
		}
		return nil
	})
}
