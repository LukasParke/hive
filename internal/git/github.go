package git

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// Ensure GitHubProvider implements Provider
var _ Provider = (*GitHubProvider)(nil)

const githubAPI = "https://api.github.com"

type GitHubProvider struct {
	token string
	client *http.Client
}

func NewGitHubProvider(token string) *GitHubProvider {
	return &GitHubProvider{
		token:  token,
		client: &http.Client{},
	}
}

func (p *GitHubProvider) req(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		r = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, githubAPI+path, r)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return p.client.Do(req)
}

func (p *GitHubProvider) ListRepos(ctx context.Context) ([]Repository, error) {
	var all []Repository
	page := 1
	for {
		path := fmt.Sprintf("/user/repos?per_page=100&page=%d&sort=updated", page)
		resp, err := p.req(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			b, _ := io.ReadAll(resp.Body)
			return nil, fmt.Errorf("github api: %s: %s", resp.Status, string(b))
		}
		var batch []struct {
			FullName      string `json:"full_name"`
			Name          string `json:"name"`
			CloneURL      string `json:"clone_url"`
			SSHURL        string `json:"ssh_url"`
			Private       bool   `json:"private"`
			DefaultBranch string `json:"default_branch"`
			Description   string `json:"description"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
			return nil, err
		}
		for _, r := range batch {
			all = append(all, Repository{
				FullName:      r.FullName,
				Name:          r.Name,
				CloneURL:      r.CloneURL,
				SSHURL:        r.SSHURL,
				Private:       r.Private,
				DefaultBranch: r.DefaultBranch,
				Description:   r.Description,
			})
		}
		if len(batch) < 100 {
			break
		}
		page++
	}
	return all, nil
}

func (p *GitHubProvider) ListBranches(ctx context.Context, repo string) ([]Branch, error) {
	path := fmt.Sprintf("/repos/%s/branches?per_page=100", repo)
	resp, err := p.req(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github api: %s: %s", resp.Status, string(b))
	}
	var raw []struct {
		Name      string `json:"name"`
		Protected bool   `json:"protected"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	// Fetch repo for default_branch
	defaultBranch := "main"
	repoResp, _ := p.req(ctx, http.MethodGet, "/repos/"+repo, nil)
	if repoResp != nil && repoResp.StatusCode == http.StatusOK {
		var repoObj struct {
			DefaultBranch string `json:"default_branch"`
		}
		_ = json.NewDecoder(repoResp.Body).Decode(&repoObj)
		_ = repoResp.Body.Close()
		if repoObj.DefaultBranch != "" {
			defaultBranch = repoObj.DefaultBranch
		}
	}
	branches := make([]Branch, len(raw))
	for i, r := range raw {
		branches[i] = Branch{
			Name:      r.Name,
			Protected: r.Protected,
			IsDefault: r.Name == defaultBranch,
		}
	}
	return branches, nil
}

func (p *GitHubProvider) CreateWebhook(ctx context.Context, repo, callbackURL, secret string) (string, error) {
	body := map[string]interface{}{
		"name":   "web",
		"active": true,
		"events": []string{"push", "pull_request"},
		"config": map[string]string{
			"url":          callbackURL,
			"content_type": "json",
			"secret":       secret,
		},
	}
	path := fmt.Sprintf("/repos/%s/hooks", repo)
	resp, err := p.req(ctx, http.MethodPost, path, body)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("github api: %s: %s", resp.Status, string(b))
	}
	var hook struct {
		ID float64 `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&hook); err != nil {
		return "", err
	}
	return fmt.Sprintf("%.0f", hook.ID), nil
}

func (p *GitHubProvider) DeleteWebhook(ctx context.Context, repo, webhookID string) error {
	path := fmt.Sprintf("/repos/%s/hooks/%s", repo, webhookID)
	resp, err := p.req(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github api: %s: %s", resp.Status, string(b))
	}
	return nil
}

func (p *GitHubProvider) PostCommitStatus(ctx context.Context, repo, sha string, status CommitStatus) error {
	body := map[string]string{
		"state":       status.State,
		"target_url": status.TargetURL,
		"description": status.Description,
		"context":     status.Context,
	}
	path := fmt.Sprintf("/repos/%s/statuses/%s", repo, sha)
	resp, err := p.req(ctx, http.MethodPost, path, body)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("github api: %s: %s", resp.Status, string(b))
	}
	return nil
}

func (p *GitHubProvider) DetectBuildType(ctx context.Context, repo, branch string) (string, error) {
	path := fmt.Sprintf("/repos/%s/contents/Dockerfile?ref=%s", repo, url.QueryEscape(branch))
	resp, err := p.req(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		return "dockerfile", nil
	}
	// 404 = no Dockerfile
	if resp.StatusCode == http.StatusNotFound {
		return "nixpacks", nil
	}
	b, _ := io.ReadAll(resp.Body)
	return "", fmt.Errorf("github api: %s: %s", resp.Status, string(b))
}
