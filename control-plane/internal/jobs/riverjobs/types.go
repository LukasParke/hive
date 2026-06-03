package riverjobs

import "github.com/riverqueue/river"

// BuildJobArgs represents a build + deploy job.
type BuildJobArgs struct {
	ApplicationID string `json:"application_id"`
	GitRef        string `json:"git_ref"`
	Trigger       string `json:"trigger"`
}

func (BuildJobArgs) Kind() string { return "build" }

func (BuildJobArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		UniqueOpts: river.UniqueOpts{
			ByArgs: true,
		},
	}
}

// CleanupJobArgs represents a periodic cleanup job.
type CleanupJobArgs struct{}

func (CleanupJobArgs) Kind() string { return "cleanup" }

// CertRenewalJobArgs represents a certificate renewal job.
type CertRenewalJobArgs struct {
	DomainID string `json:"domain_id"`
}

func (CertRenewalJobArgs) Kind() string { return "cert_renewal" }

// PreviewDeployJobArgs represents a preview deployment build job.
type PreviewDeployJobArgs struct {
	PreviewID     string `json:"preview_id"`
	ApplicationID string `json:"application_id"`
	Branch        string `json:"branch"`
	CommitSha     string `json:"commit_sha"`
}

func (PreviewDeployJobArgs) Kind() string { return "preview_deploy" }

// PreviewCleanupJobArgs represents a preview deployment cleanup job.
type PreviewCleanupJobArgs struct{}

func (PreviewCleanupJobArgs) Kind() string { return "preview_cleanup" }
