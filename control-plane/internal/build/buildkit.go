package build

import (
	"context"
	"fmt"
	"io"

	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/auth/authprovider"
)

type BuildRequest struct {
	ContextPath string
	Dockerfile  string
	ImageTag    string
}

type Client struct {
	Addr string
}

func NewClient(addr string) *Client {
	return &Client{Addr: addr}
}

func (c *Client) BuildAndPush(ctx context.Context, req BuildRequest, logWriter io.Writer) error {
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
	defer bk.Close()

	statusChan := make(chan *client.SolveStatus)
	go func() {
		for st := range statusChan {
			for _, v := range st.Vertexes {
				if v.Name != "" && logWriter != nil {
					fmt.Fprintf(logWriter, "%s\n", v.Name)
				}
			}
		}
	}()

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
		Session: []session.Attachable{
			authprovider.NewDockerAuthProvider(authprovider.DockerAuthProviderConfig{}),
		},
	}

	_, err = bk.Solve(ctx, nil, opt, statusChan)
	if err != nil {
		return fmt.Errorf("buildkit solve: %w", err)
	}
	return nil
}
