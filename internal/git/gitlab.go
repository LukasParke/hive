package git

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const gitlabAPI = "https://gitlab.com/api/v4"

var errNotImplemented = fmt.Errorf("gitlab: not implemented")

type GitLabProvider struct {
	token  string
	client *http.Client
}

func NewGitLabProvider(token string) *GitLabProvider {
	return &GitLabProvider{
		token:  token,
		client: &http.Client{},
	}
}

func (p *GitLabProvider) req(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, gitlabAPI+path, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return p.client.Do(req)
}

func (p *GitLabProvider) ListRepos(ctx context.Context) ([]Repository, error) {
	var all []Repository
	page := 1
	for {
		path := fmt.Sprintf("/projects?membership=true&per_page=100&page=%d", page)
		resp, err := p.req(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("gitlab api: %s: %s", resp.Status, string(b))
		}
		var batch []struct {
			PathWithNamespace string `json:"path_with_namespace"`
			Name              string `json:"name"`
			HTTPURLToRepo      string `json:"http_url_to_repo"`
			SSHURLToRepo       string `json:"ssh_url_to_repo"`
			DefaultBranch      string `json:"default_branch"`
			Description       string `json:"description"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
			return nil, err
		}
		for _, proj := range batch {
			all = append(all, Repository{
				FullName:      proj.PathWithNamespace,
				Name:          proj.Name,
				CloneURL:      proj.HTTPURLToRepo,
				SSHURL:        proj.SSHURLToRepo,
				DefaultBranch: proj.DefaultBranch,
				Description:   proj.Description,
			})
		}
		if len(batch) < 100 {
			break
		}
		page++
	}
	return all, nil
}

func (p *GitLabProvider) ListBranches(ctx context.Context, repo string) ([]Branch, error) {
	return nil, errNotImplemented
}

func (p *GitLabProvider) CreateWebhook(ctx context.Context, repo, callbackURL, secret string) (string, error) {
	return "", errNotImplemented
}

func (p *GitLabProvider) DeleteWebhook(ctx context.Context, repo, webhookID string) error {
	return errNotImplemented
}

func (p *GitLabProvider) PostCommitStatus(ctx context.Context, repo, sha string, status CommitStatus) error {
	return errNotImplemented
}

func (p *GitLabProvider) DetectBuildType(ctx context.Context, repo, branch string) (string, error) {
	return "", errNotImplemented
}

// Ensure GitLabProvider implements Provider
var _ Provider = (*GitLabProvider)(nil)
