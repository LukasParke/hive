package build

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/docker/cli/cli/config/types"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/auth/authprovider"
)

// Request describes a BuildKit image build.
type Request struct {
	ContextPath string
	Dockerfile  string
	ImageTag    string
	// Auth carries resolved registry credentials for the push target.
	// nil means anonymous / docker-config-based auth.
	Auth *RegistryAuth
}

// Client is a BuildKit image build client.
type Client struct {
	Addr string
}

// NewClient returns a Client for the given BuildKit address.
func NewClient(addr string) *Client {
	return &Client{Addr: addr}
}

// Builder builds and pushes images. *Client is the production
// implementation; tests inject fakes.
type Builder interface {
	BuildAndPush(ctx context.Context, req Request, logWriter io.Writer) error
}

// Compile-time check that the BuildKit client satisfies Builder.
var _ Builder = (*Client)(nil)

// BuildAndPush builds the given context and pushes the resulting image.
func (c *Client) BuildAndPush(ctx context.Context, req Request, logWriter io.Writer) error {
	if req.ImageTag == "" {
		return fmt.Errorf("image tag is required")
	}
	if req.ContextPath == "" {
		return fmt.Errorf("context path is required")
	}
	if req.Dockerfile == "" {
		req.Dockerfile = "Dockerfile"
	}

	bk, err := client.New(ctx, c.Addr)
	if err != nil {
		return fmt.Errorf("buildkit connect: %w", err)
	}
	defer func() { _ = bk.Close() }()

	statusChan := make(chan *client.SolveStatus)
	go bridgeSolveLogs(statusChan, logWriter)

	sessionAttachables := []session.Attachable{
		authprovider.NewDockerAuthProvider(authprovider.DockerAuthProviderConfig{}),
	}
	if req.Auth != nil {
		sessionAttachables = []session.Attachable{
			authprovider.NewDockerAuthProvider(authprovider.DockerAuthProviderConfig{
				AuthConfigProvider: staticAuthConfigProvider(req.Auth),
			}),
		}
	}

	opt := client.SolveOpt{
		Frontend: "dockerfile.v0",
		FrontendAttrs: map[string]string{
			"filename": req.Dockerfile,
		},
		LocalDirs: map[string]string{
			"context":    req.ContextPath,
			"dockerfile": req.ContextPath,
		},
		Exports: []client.ExportEntry{
			{
				Type: "image",
				Attrs: map[string]string{
					"name": req.ImageTag,
					"push": "true",
				},
			},
		},
		Session: sessionAttachables,
	}

	_, err = bk.Solve(ctx, nil, opt, statusChan)
	if err != nil {
		return fmt.Errorf("buildkit solve: %w", err)
	}
	return nil
}

// bridgeSolveLogs converts SolveStatus messages into line-oriented output:
// vertex names as they start and streamed log chunks from running steps.
func bridgeSolveLogs(statusChan <-chan *client.SolveStatus, w io.Writer) {
	if w == nil {
		for range statusChan {
		}
		return
	}
	bw := bufio.NewWriter(w)
	flush := func() { _ = bw.Flush() }
	defer flush()

	seen := map[string]bool{}
	for st := range statusChan {
		for _, v := range st.Vertexes {
			if v.Name == "" || seen[v.Digest.String()] {
				continue
			}
			seen[v.Digest.String()] = true
			_, _ = fmt.Fprintf(bw, "# %s\n", v.Name)
		}
		for _, l := range st.Logs {
			chunk := strings.TrimSuffix(string(l.Data), "\n")
			if chunk == "" {
				continue
			}
			_, _ = fmt.Fprintln(bw, chunk)
		}
		flush()
	}
}

// staticAuthConfigProvider returns credentials for the resolved registry
// host; other hosts fall through to anonymous pulls so public base images
// keep working.
func staticAuthConfigProvider(ra *RegistryAuth) authprovider.AuthConfigProvider {
	return func(_ context.Context, host string, _ []string, _ authprovider.ExpireCachedAuthCheck) (types.AuthConfig, error) {
		if ra != nil && SameRegistryHost(host, ra.Host) {
			return types.AuthConfig{
				Username:      ra.Username,
				Password:      ra.Password,
				ServerAddress: ra.Host,
			}, nil
		}
		return types.AuthConfig{}, nil
	}
}
