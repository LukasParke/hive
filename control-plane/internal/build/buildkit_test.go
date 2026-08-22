package build

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/docker/cli/cli/config/types"
	"github.com/moby/buildkit/client"
)

func TestNewClient(t *testing.T) {
	c := NewClient("tcp://127.0.0.1:1234")
	if c == nil || c.Addr != "tcp://127.0.0.1:1234" {
		t.Fatalf("NewClient() = %+v, want client with Addr set", c)
	}
}

func TestBuildAndPushValidation(t *testing.T) {
	c := NewClient("tcp://127.0.0.1:1")

	if err := c.BuildAndPush(context.Background(), Request{ContextPath: "/ctx"}, nil); err == nil || err.Error() != "image tag is required" {
		t.Fatalf("err = %v, want image tag is required", err)
	}
	if err := c.BuildAndPush(context.Background(), Request{ImageTag: "reg/app:tag"}, nil); err == nil || err.Error() != "context path is required" {
		t.Fatalf("err = %v, want context path is required", err)
	}
}

func TestBuildAndPushConnectFailure(t *testing.T) {
	t.Run("unparseable address fails at connect", func(t *testing.T) {
		c := NewClient("127.0.0.1:1")
		err := c.BuildAndPush(context.Background(), Request{ImageTag: "reg.example.com/app:tag", ContextPath: t.TempDir()}, &bytes.Buffer{})
		if err == nil || !strings.HasPrefix(err.Error(), "buildkit connect: ") {
			t.Fatalf("err = %v, want buildkit connect failure", err)
		}
	})

	// A well-formed but unreachable tcp:// address dials lazily, so the
	// failure surfaces from the solve call instead of the connect.
	t.Run("unreachable daemon fails at solve", func(t *testing.T) {
		c := NewClient("tcp://127.0.0.1:1")
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		err := c.BuildAndPush(ctx, Request{
			ImageTag:    "reg.example.com/app:tag",
			ContextPath: t.TempDir(),
			Dockerfile:  "Dockerfile.custom",
			Auth:        &RegistryAuth{Host: "reg.example.com", Username: "u", Password: "p"},
		}, &bytes.Buffer{})
		if err == nil || !strings.HasPrefix(err.Error(), "buildkit solve: ") {
			t.Fatalf("err = %v, want buildkit solve failure", err)
		}
	})
}

func TestBridgeSolveLogs(t *testing.T) {
	t.Run("writes vertex names and log chunks", func(t *testing.T) {
		statusChan := make(chan *client.SolveStatus, 2)
		statusChan <- &client.SolveStatus{
			Vertexes: []*client.Vertex{
				{Digest: "d1", Name: "[1/2] FROM scratch"},
				{Digest: "d2", Name: ""}, // unnamed vertices are skipped
			},
		}
		statusChan <- &client.SolveStatus{
			Vertexes: []*client.Vertex{
				{Digest: "d1", Name: "[1/2] FROM scratch"}, // duplicate digest is skipped
				{Digest: "d3", Name: "[2/2] RUN echo hi"},
			},
			Logs: []*client.VertexLog{
				{Vertex: "d3", Stream: 1, Data: []byte("step one output\n")},
				{Vertex: "d3", Stream: 1, Data: []byte("\n")}, // whitespace-only chunk is skipped
			},
		}
		close(statusChan)

		var buf bytes.Buffer
		done := make(chan struct{})
		go func() {
			defer close(done)
			bridgeSolveLogs(statusChan, &buf)
		}()
		<-done

		want := "# [1/2] FROM scratch\n# [2/2] RUN echo hi\nstep one output\n"
		if buf.String() != want {
			t.Fatalf("output = %q, want %q", buf.String(), want)
		}
	})

	t.Run("nil writer drains channel", func(t *testing.T) {
		statusChan := make(chan *client.SolveStatus, 1)
		statusChan <- &client.SolveStatus{
			Vertexes: []*client.Vertex{{Digest: "d1", Name: "[1/2] FROM scratch"}},
		}
		close(statusChan)

		done := make(chan struct{})
		go func() {
			defer close(done)
			bridgeSolveLogs(statusChan, nil)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("bridgeSolveLogs with nil writer did not drain the channel")
		}
	})
}

func TestStaticAuthConfigProvider(t *testing.T) {
	ra := &RegistryAuth{Host: "reg.example.com", Username: "u", Password: "p"}
	provider := staticAuthConfigProvider(ra)

	matching, err := provider(context.Background(), "reg.example.com", nil, nil)
	if err != nil {
		t.Fatalf("provider(reg.example.com): %v", err)
	}
	if matching.Username != "u" || matching.Password != "p" || matching.ServerAddress != "reg.example.com" {
		t.Fatalf("auth config = %+v, want resolved registry credentials", matching)
	}

	// Scheme/trailing-slash variants of the configured host still match.
	variant, err := provider(context.Background(), "https://reg.example.com/", nil, nil)
	if err != nil {
		t.Fatalf("provider(https://reg.example.com/): %v", err)
	}
	if variant.Username != "u" {
		t.Fatalf("variant auth config = %+v, want credentials for normalized host", variant)
	}

	other, err := provider(context.Background(), "other.example.com", nil, nil)
	if err != nil {
		t.Fatalf("provider(other.example.com): %v", err)
	}
	if other.Username != "" || other.Password != "" || other.ServerAddress != "" {
		t.Fatalf("other-host auth config = %+v, want anonymous fallback", other)
	}

	anonymous, err := staticAuthConfigProvider(nil)(context.Background(), "reg.example.com", nil, nil)
	if err != nil {
		t.Fatalf("provider(nil)(reg.example.com): %v", err)
	}
	if anonymous != (types.AuthConfig{}) {
		t.Fatalf("nil-ra auth config = %+v, want zero value", anonymous)
	}
}
