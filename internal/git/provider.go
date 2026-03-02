package git

import "context"

type Repository struct {
	FullName      string `json:"full_name"`
	Name          string `json:"name"`
	CloneURL      string `json:"clone_url"`
	SSHURL        string `json:"ssh_url"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
	Description   string `json:"description"`
}

type Branch struct {
	Name      string `json:"name"`
	Protected bool   `json:"protected"`
	IsDefault bool   `json:"is_default"`
}

type CommitStatus struct {
	State       string `json:"state"` // pending, success, failure, error
	TargetURL  string `json:"target_url"`
	Description string `json:"description"`
	Context    string `json:"context"`
}

type Provider interface {
	ListRepos(ctx context.Context) ([]Repository, error)
	ListBranches(ctx context.Context, repo string) ([]Branch, error)
	CreateWebhook(ctx context.Context, repo, callbackURL, secret string) (string, error)
	DeleteWebhook(ctx context.Context, repo, webhookID string) error
	PostCommitStatus(ctx context.Context, repo, sha string, status CommitStatus) error
	DetectBuildType(ctx context.Context, repo, branch string) (string, error)
}
