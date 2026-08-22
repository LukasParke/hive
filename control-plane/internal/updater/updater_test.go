package updater

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	mswarm "github.com/moby/moby/api/types/swarm"
	dockerclient "github.com/moby/moby/client"

	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
	"github.com/luke/hive/control-plane/internal/version"
)

var errBoom = errors.New("boom")

// fakeSwarm embeds swarmclient.APIClient so only the service slices the
// Updater uses need overriding.
type fakeSwarm struct {
	swarmclient.APIClient

	services   []mswarm.Service
	listErr    error
	updateErrs []error // consumed in order; last repeats

	updates []updateCall
}

type updateCall struct {
	id      string
	version uint64
	image   string
}

func (f *fakeSwarm) ServiceList(ctx context.Context, opts dockerclient.ServiceListOptions) (dockerclient.ServiceListResult, error) {
	return dockerclient.ServiceListResult{Items: f.services}, f.listErr
}

func (f *fakeSwarm) ServiceUpdate(ctx context.Context, serviceID string, opts dockerclient.ServiceUpdateOptions) (dockerclient.ServiceUpdateResult, error) {
	var err error
	if len(f.updateErrs) > 0 {
		err = f.updateErrs[0]
		if len(f.updateErrs) > 1 {
			f.updateErrs = f.updateErrs[1:]
		}
	}
	img := ""
	if opts.Spec.TaskTemplate.ContainerSpec != nil {
		img = opts.Spec.TaskTemplate.ContainerSpec.Image
	}
	f.updates = append(f.updates, updateCall{id: serviceID, version: opts.Version.Index, image: img})
	return dockerclient.ServiceUpdateResult{}, err
}

func hiveService(name, image string, index uint64) mswarm.Service {
	return mswarm.Service{
		ID: name + "-id",
		Spec: mswarm.ServiceSpec{
			Annotations: mswarm.Annotations{Name: name},
			TaskTemplate: mswarm.TaskSpec{
				ContainerSpec: &mswarm.ContainerSpec{Image: image},
			},
		},
		Meta: mswarm.Meta{Version: mswarm.Version{Index: index}},
	}
}

// startFakeGitHub serves the given payload on the releases endpoint and
// points the updater at it.
func startFakeGitHub(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newUpdaterWithFake(t *testing.T, fs *fakeSwarm) *Updater {
	t.Helper()
	u := New(swarmclient.NewWithAPI(fs))
	return u
}

func TestFetchLatestRelease(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		srv := startFakeGitHub(t, http.StatusOK, `{"tag_name":"v9.9.9"}`)
		u := newUpdaterWithFake(t, &fakeSwarm{})
		u.releaseURL = srv.URL
		tag, err := u.fetchLatestRelease(context.Background())
		if err != nil || tag != "v9.9.9" {
			t.Fatalf("fetchLatestRelease = %q, %v", tag, err)
		}
	})

	t.Run("http error status", func(t *testing.T) {
		srv := startFakeGitHub(t, http.StatusNotFound, "not found")
		u := newUpdaterWithFake(t, &fakeSwarm{})
		u.releaseURL = srv.URL
		_, err := u.fetchLatestRelease(context.Background())
		if err == nil || !strings.Contains(err.Error(), "404") {
			t.Fatalf("err = %v, want 404", err)
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		srv := startFakeGitHub(t, http.StatusOK, "{not json")
		u := newUpdaterWithFake(t, &fakeSwarm{})
		u.releaseURL = srv.URL
		var syntaxErr *json.SyntaxError
		if _, err := u.fetchLatestRelease(context.Background()); !errors.As(err, &syntaxErr) {
			t.Fatalf("err = %v, want json syntax error", err)
		}
	})

	t.Run("missing tag name", func(t *testing.T) {
		srv := startFakeGitHub(t, http.StatusOK, `{"tag_name":""}`)
		u := newUpdaterWithFake(t, &fakeSwarm{})
		u.releaseURL = srv.URL
		if _, err := u.fetchLatestRelease(context.Background()); err == nil || !strings.Contains(err.Error(), "no tag_name") {
			t.Fatalf("err = %v, want missing tag_name", err)
		}
	})

	t.Run("unreachable server", func(t *testing.T) {
		u := newUpdaterWithFake(t, &fakeSwarm{})
		u.releaseURL = "http://127.0.0.1:1/none"
		if _, err := u.fetchLatestRelease(context.Background()); err == nil {
			t.Fatal("err = nil, want transport failure")
		}
	})
}

func TestCheckNow(t *testing.T) {
	oldCurrent := version.Current
	t.Cleanup(func() { version.Current = oldCurrent })

	t.Run("update available", func(t *testing.T) {
		version.Current = "v1.0.0"
		srv := startFakeGitHub(t, http.StatusOK, `{"tag_name":"v2.0.0"}`)
		u := newUpdaterWithFake(t, &fakeSwarm{})
		u.releaseURL = srv.URL
		if err := u.CheckNow(context.Background()); err != nil {
			t.Fatalf("CheckNow: %v", err)
		}
		st := u.Status()
		if !st.UpdateAvailable || st.LatestVersion != "v2.0.0" || st.CurrentVersion != "v1.0.0" {
			t.Fatalf("status = %+v", st)
		}
		if st.LastCheckedAt == "" {
			t.Fatal("LastCheckedAt not set")
		}
	})

	t.Run("dev build never reports update", func(t *testing.T) {
		version.Current = "dev"
		srv := startFakeGitHub(t, http.StatusOK, `{"tag_name":"v99.0.0"}`)
		u := newUpdaterWithFake(t, &fakeSwarm{})
		u.releaseURL = srv.URL
		if err := u.CheckNow(context.Background()); err != nil {
			t.Fatalf("CheckNow: %v", err)
		}
		if u.Status().UpdateAvailable {
			t.Fatal("UpdateAvailable = true for dev build")
		}
	})

	t.Run("up to date", func(t *testing.T) {
		version.Current = "v3.0.0"
		srv := startFakeGitHub(t, http.StatusOK, `{"tag_name":"v3.0.0"}`)
		u := newUpdaterWithFake(t, &fakeSwarm{})
		u.releaseURL = srv.URL
		if err := u.CheckNow(context.Background()); err != nil {
			t.Fatalf("CheckNow: %v", err)
		}
		if u.Status().UpdateAvailable {
			t.Fatal("UpdateAvailable = true for equal versions")
		}
	})

	t.Run("fetch failure propagates", func(t *testing.T) {
		srv := startFakeGitHub(t, http.StatusInternalServerError, "oops")
		u := newUpdaterWithFake(t, &fakeSwarm{})
		u.releaseURL = srv.URL
		if err := u.CheckNow(context.Background()); err == nil || !strings.Contains(err.Error(), "fetch latest release") {
			t.Fatalf("err = %v, want wrapped fetch failure", err)
		}
	})
}

func TestRunChecksThenStopsOnCanceledContext(t *testing.T) {
	oldCurrent := version.Current
	version.Current = "v1.0.0"
	t.Cleanup(func() { version.Current = oldCurrent })

	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte(`{"tag_name":"v2.0.0"}`))
	}))
	t.Cleanup(srv.Close)
	u := newUpdaterWithFake(t, &fakeSwarm{})
	u.releaseURL = srv.URL
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	u.Run(ctx) // initial check runs, then the loop observes ctx.Done and returns
	if hits < 1 {
		t.Fatalf("github hits = %d, want at least the initial check", hits)
	}
}

func TestRunChecksPeriodicallyUntilCanceled(t *testing.T) {
	oldInterval := checkInterval
	checkInterval = 10 * time.Millisecond
	t.Cleanup(func() { checkInterval = oldInterval })

	var mu sync.Mutex
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		_, _ = w.Write([]byte(`{"tag_name":"v1.0.0"}`))
	}))
	t.Cleanup(srv.Close)

	u := newUpdaterWithFake(t, &fakeSwarm{})
	u.releaseURL = srv.URL
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(80 * time.Millisecond)
		cancel()
	}()
	u.Run(ctx)
	mu.Lock()
	defer mu.Unlock()
	if hits < 2 {
		t.Fatalf("github hits = %d, want initial check plus ticker checks", hits)
	}
}

func TestUpdateReturnsCheckFailureWhenStatusEmpty(t *testing.T) {
	srv := startFakeGitHub(t, http.StatusInternalServerError, "oops")
	u := newUpdaterWithFake(t, &fakeSwarm{})
	u.releaseURL = srv.URL
	err := u.Update(context.Background())
	if err == nil || !strings.Contains(err.Error(), "fetch latest release") {
		t.Fatalf("err = %v, want CheckNow failure propagated", err)
	}
}

func TestUpdateRewritesServiceImages(t *testing.T) {
	oldCurrent := version.Current
	version.Current = "v1.0.0"
	t.Cleanup(func() { version.Current = oldCurrent })

	srv := startFakeGitHub(t, http.StatusOK, `{"tag_name":"v2.0.0"}`)
	fs := &fakeSwarm{services: []mswarm.Service{
		hiveService("hive_control-plane", "ghcr.io/lukasparke/hive/control-plane:v1.0.0", 5),
		hiveService("hive_agent", "ghcr.io/lukasparke/hive/agent@sha256:abc123", 9),
		hiveService("unrelated", "docker.io/library/redis:7", 1),
	}}
	u := newUpdaterWithFake(t, fs)
	u.releaseURL = srv.URL
	if err := u.CheckNow(context.Background()); err != nil {
		t.Fatalf("CheckNow: %v", err)
	}
	if err := u.Update(context.Background()); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(fs.updates) != 2 {
		t.Fatalf("updates = %+v, want control-plane and agent rewrites", fs.updates)
	}
	if fs.updates[0].id != "hive_control-plane-id" || fs.updates[0].version != 5 {
		t.Errorf("control-plane update = %+v", fs.updates[0])
	}
	if fs.updates[0].image != "ghcr.io/lukasparke/hive/control-plane:v2.0.0" {
		t.Errorf("control-plane image = %q", fs.updates[0].image)
	}
	if fs.updates[1].id != "hive_agent-id" || fs.updates[1].version != 9 {
		t.Errorf("agent update = %+v", fs.updates[1])
	}
	if fs.updates[1].image != "ghcr.io/lukasparke/hive/agent:v2.0.0@sha256:abc123" {
		t.Errorf("agent image = %q, want digest preserved", fs.updates[1].image)
	}
}

func TestUpdateNoUpdateAvailable(t *testing.T) {
	oldCurrent := version.Current
	version.Current = "v5.0.0"
	t.Cleanup(func() { version.Current = oldCurrent })

	srv := startFakeGitHub(t, http.StatusOK, `{"tag_name":"v5.0.0"}`)
	u := newUpdaterWithFake(t, &fakeSwarm{})
	u.releaseURL = srv.URL
	err := u.Update(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no update available") {
		t.Fatalf("err = %v, want no-update error", err)
	}
}

func TestUpdateChecksFirstWhenStatusEmpty(t *testing.T) {
	srv := startFakeGitHub(t, http.StatusOK, `{"tag_name":"v1.0.0"}`)
	u := newUpdaterWithFake(t, &fakeSwarm{})
	u.releaseURL = srv.URL
	// Status.LatestVersion is empty, so Update runs CheckNow itself; the
	// fake reports the current dev version, so no update is available.
	if err := u.Update(context.Background()); err == nil || !strings.Contains(err.Error(), "no update available") {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateControlPlaneFailureWrapped(t *testing.T) {
	oldCurrent := version.Current
	version.Current = "v1.0.0"
	t.Cleanup(func() { version.Current = oldCurrent })

	srv := startFakeGitHub(t, http.StatusOK, `{"tag_name":"v2.0.0"}`)
	fs := &fakeSwarm{
		services:   []mswarm.Service{hiveService("hive_control-plane", "ghcr.io/lukasparke/hive/control-plane:v1.0.0", 5)},
		updateErrs: []error{errBoom},
	}
	u := newUpdaterWithFake(t, fs)
	u.releaseURL = srv.URL
	if err := u.CheckNow(context.Background()); err != nil {
		t.Fatalf("CheckNow: %v", err)
	}
	err := u.Update(context.Background())
	if !errors.Is(err, errBoom) || !strings.Contains(err.Error(), "update control-plane") {
		t.Fatalf("err = %v, want wrapped control-plane failure", err)
	}
}

func TestUpdateAgentFailureWrapped(t *testing.T) {
	oldCurrent := version.Current
	version.Current = "v1.0.0"
	t.Cleanup(func() { version.Current = oldCurrent })

	srv := startFakeGitHub(t, http.StatusOK, `{"tag_name":"v2.0.0"}`)
	fs := &fakeSwarm{
		services: []mswarm.Service{
			hiveService("hive_control-plane", "ghcr.io/lukasparke/hive/control-plane:v1.0.0", 5),
			hiveService("hive_agent", "ghcr.io/lukasparke/hive/agent:v1.0.0", 9),
		},
		updateErrs: []error{nil, errBoom},
	}
	u := newUpdaterWithFake(t, fs)
	u.releaseURL = srv.URL
	if err := u.CheckNow(context.Background()); err != nil {
		t.Fatalf("CheckNow: %v", err)
	}
	err := u.Update(context.Background())
	if !errors.Is(err, errBoom) || !strings.Contains(err.Error(), "update agent") {
		t.Fatalf("err = %v, want wrapped agent failure", err)
	}
}

func TestUpdateServiceByImagePrefixErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("list failure", func(t *testing.T) {
		fs := &fakeSwarm{listErr: errBoom}
		u := newUpdaterWithFake(t, fs)
		if err := u.updateServiceByImagePrefix(ctx, "ghcr.io/x", "2"); !errors.Is(err, errBoom) {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("no matching service", func(t *testing.T) {
		fs := &fakeSwarm{services: []mswarm.Service{
			hiveService("other", "docker.io/library/nginx:1", 1),
		}}
		u := newUpdaterWithFake(t, fs)
		err := u.updateServiceByImagePrefix(ctx, "ghcr.io/lukasparke/hive/agent", "2")
		if err == nil || !strings.Contains(err.Error(), "no swarm service found") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("skips services without container spec", func(t *testing.T) {
		svc := hiveService("hive_agent", "ghcr.io/lukasparke/hive/agent:v1", 3)
		svc.Spec.TaskTemplate.ContainerSpec = nil
		fs := &fakeSwarm{services: []mswarm.Service{svc}}
		u := newUpdaterWithFake(t, fs)
		err := u.updateServiceByImagePrefix(ctx, "ghcr.io/lukasparke/hive/agent", "2")
		if err == nil || !strings.Contains(err.Error(), "no swarm service found") {
			t.Fatalf("err = %v", err)
		}
		if len(fs.updates) != 0 {
			t.Fatalf("updates = %v, want none", fs.updates)
		}
	})
}

func TestGetCurrentImageTag(t *testing.T) {
	ctx := context.Background()

	t.Run("extracts tag", func(t *testing.T) {
		fs := &fakeSwarm{services: []mswarm.Service{
			hiveService("hive_control-plane", "ghcr.io/lukasparke/hive/control-plane:v4.5.6", 1),
		}}
		u := newUpdaterWithFake(t, fs)
		tag, err := u.GetCurrentImageTag(ctx, "ghcr.io/lukasparke/hive/control-plane")
		if err != nil || tag != "v4.5.6" {
			t.Fatalf("GetCurrentImageTag = %q, %v", tag, err)
		}
	})

	t.Run("defaults to latest without tag", func(t *testing.T) {
		fs := &fakeSwarm{services: []mswarm.Service{
			hiveService("hive_agent", "ghcr.io/lukasparke/hive/agent", 1),
		}}
		u := newUpdaterWithFake(t, fs)
		tag, err := u.GetCurrentImageTag(ctx, "ghcr.io/lukasparke/hive/agent")
		if err != nil || tag != "latest" {
			t.Fatalf("GetCurrentImageTag = %q, %v", tag, err)
		}
	})

	t.Run("skips services without container spec", func(t *testing.T) {
		svc := hiveService("hive_agent", "ghcr.io/lukasparke/hive/agent:v1", 1)
		svc.Spec.TaskTemplate.ContainerSpec = nil
		fs := &fakeSwarm{services: []mswarm.Service{svc}}
		u := newUpdaterWithFake(t, fs)
		if _, err := u.GetCurrentImageTag(ctx, "ghcr.io/lukasparke/hive/agent"); err == nil || !strings.Contains(err.Error(), "no service found") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("no matching service", func(t *testing.T) {
		u := newUpdaterWithFake(t, &fakeSwarm{})
		if _, err := u.GetCurrentImageTag(ctx, "ghcr.io/none"); err == nil || !strings.Contains(err.Error(), "no service found") {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("list failure", func(t *testing.T) {
		u := newUpdaterWithFake(t, &fakeSwarm{listErr: errBoom})
		if _, err := u.GetCurrentImageTag(ctx, "ghcr.io/x"); !errors.Is(err, errBoom) {
			t.Fatalf("err = %v", err)
		}
	})
}
