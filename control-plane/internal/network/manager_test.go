package network

import (
	"context"
	"errors"
	"strings"
	"testing"

	dockernet "github.com/moby/moby/api/types/network"
)

var errBoom = errors.New("boom")

// fakeNetworkStore scripts NetworkStore results and records created names.
type fakeNetworkStore struct {
	networks  []dockernet.Summary
	listErr   error
	createID  string
	createErr error

	created []string
}

func (f *fakeNetworkStore) ListNetworks(ctx context.Context) ([]dockernet.Summary, error) {
	return f.networks, f.listErr
}

func (f *fakeNetworkStore) CreateNetwork(ctx context.Context, name string) (string, error) {
	f.created = append(f.created, name)
	return f.createID, f.createErr
}

func TestProjectNetworkName(t *testing.T) {
	cases := []struct {
		slug string
		want string
	}{
		{"my-app", "hive_project_my-app"},
		{"MyApp", "hive_project_myapp"},
		{"my app", "hive_project_my-app"},
		{"My App!2026", "hive_project_my-app-2026"},
		{"spaces   and___underscores", "hive_project_spaces-and___underscores"},
		{"UPPER/lower", "hive_project_upper-lower"},
		{"dots.and.dashes-x", "hive_project_dots-and-dashes-x"},
	}
	for _, tc := range cases {
		if got := ProjectNetworkName(tc.slug); got != tc.want {
			t.Errorf("ProjectNetworkName(%q) = %q, want %q", tc.slug, got, tc.want)
		}
	}
}

func TestEnsureOverlayExisting(t *testing.T) {
	f := &fakeNetworkStore{
		networks: []dockernet.Summary{
			{Network: dockernet.Network{Name: "other", ID: "n0"}},
			{Network: dockernet.Network{Name: "hive_project_web", ID: "n42"}},
		},
	}
	m := New(f)
	id, err := m.EnsureOverlay(context.Background(), "hive_project_web")
	if err != nil {
		t.Fatalf("EnsureOverlay: %v", err)
	}
	if id != "n42" {
		t.Fatalf("id = %q, want n42", id)
	}
	if len(f.created) != 0 {
		t.Fatalf("created = %v, want no creates for existing network", f.created)
	}
}

func TestEnsureOverlayCreates(t *testing.T) {
	f := &fakeNetworkStore{createID: "n-new"}
	m := New(f)
	id, err := m.EnsureOverlay(context.Background(), "hive_new")
	if err != nil {
		t.Fatalf("EnsureOverlay: %v", err)
	}
	if id != "n-new" {
		t.Fatalf("id = %q, want n-new", id)
	}
	if len(f.created) != 1 || f.created[0] != "hive_new" {
		t.Fatalf("created = %v, want [hive_new]", f.created)
	}
}

func TestEnsureOverlayListError(t *testing.T) {
	f := &fakeNetworkStore{listErr: errBoom}
	m := New(f)
	_, err := m.EnsureOverlay(context.Background(), "hive_x")
	if !errors.Is(err, errBoom) || !strings.Contains(err.Error(), "list networks") {
		t.Fatalf("err = %v, want wrapped list error", err)
	}
}

func TestEnsureOverlayCreateError(t *testing.T) {
	f := &fakeNetworkStore{createErr: errBoom}
	m := New(f)
	_, err := m.EnsureOverlay(context.Background(), "hive_x")
	if !errors.Is(err, errBoom) || !strings.Contains(err.Error(), "create network hive_x") {
		t.Fatalf("err = %v, want wrapped create error", err)
	}
	if len(f.created) != 1 {
		t.Fatalf("create attempted %d times, want 1", len(f.created))
	}
}

func TestEnsureProjectNetwork(t *testing.T) {
	f := &fakeNetworkStore{
		networks: []dockernet.Summary{{Network: dockernet.Network{Name: "hive_project_my-app", ID: "p1"}}},
	}
	m := New(f)
	id, err := m.EnsureProjectNetwork(context.Background(), "My App")
	if err != nil {
		t.Fatalf("EnsureProjectNetwork: %v", err)
	}
	if id != "p1" {
		t.Fatalf("id = %q, want p1", id)
	}
	if len(f.created) != 0 {
		t.Fatalf("created = %v, want reuse of sanitized existing network", f.created)
	}
}

func TestEnsureProjectNetworkCreatesSanitized(t *testing.T) {
	f := &fakeNetworkStore{createID: "p2"}
	m := New(f)
	id, err := m.EnsureProjectNetwork(context.Background(), "Weird Slug!")
	if err != nil {
		t.Fatalf("EnsureProjectNetwork: %v", err)
	}
	if id != "p2" {
		t.Fatalf("id = %q, want p2", id)
	}
	if len(f.created) != 1 || f.created[0] != "hive_project_weird-slug-" {
		t.Fatalf("created = %v, want [hive_project_weird-slug-]", f.created)
	}
}
