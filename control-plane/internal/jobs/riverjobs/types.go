package riverjobs

import "github.com/riverqueue/river"

// BuildJobArgs represents a build job. BuildID is the build_jobs row
// (uuid) the worker executes.
type BuildJobArgs struct {
	BuildID string `json:"build_id"`
}

// Kind implements river.JobArgs.
func (BuildJobArgs) Kind() string { return "build" }

// InsertOpts returns the default queue options for this job kind.
func (BuildJobArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       QueueBuild,
		MaxAttempts: 3,
	}
}

// DeployJobArgs represents a deployment job. DeploymentID is the
// deployments row (uuid) the worker applies.
type DeployJobArgs struct {
	DeploymentID string `json:"deployment_id"`
}

// Kind implements river.JobArgs.
func (DeployJobArgs) Kind() string { return "deploy" }

// InsertOpts returns the default queue options for this job kind.
func (DeployJobArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       QueueDeploy,
		MaxAttempts: 4,
	}
}

// BackupJobArgs triggers execution of a queued backup run for the given
// target (e.g. TargetType "database", TargetID the database service id).
type BackupJobArgs struct {
	TargetType string `json:"target_type"`
	TargetID   string `json:"target_id"`
}

// Kind implements river.JobArgs.
func (BackupJobArgs) Kind() string { return "backup" }

// CleanupJobArgs represents a periodic cleanup job (build history pruning,
// orphaned preview stack removal).
type CleanupJobArgs struct{}

// Kind implements river.JobArgs.
func (CleanupJobArgs) Kind() string { return "cleanup" }

// CertRenewalJobArgs represents a periodic control-plane client certificate
// renewal job.
type CertRenewalJobArgs struct{}

// Kind implements river.JobArgs.
func (CertRenewalJobArgs) Kind() string { return "cert_renewal" }

// PreviewDeployJobArgs represents a preview deployment build job.
type PreviewDeployJobArgs struct {
	PreviewID     string `json:"preview_id"`
	ApplicationID string `json:"application_id"`
	Branch        string `json:"branch"`
	CommitSha     string `json:"commit_sha"`
}

// Kind implements river.JobArgs.
func (PreviewDeployJobArgs) Kind() string { return "preview_deploy" }

// InsertOpts returns the default queue options for this job kind.
func (PreviewDeployJobArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       QueueBuild,
		MaxAttempts: 3,
	}
}

// PreviewCleanupJobArgs represents a preview deployment cleanup job.
type PreviewCleanupJobArgs struct{}

// Kind implements river.JobArgs.
func (PreviewCleanupJobArgs) Kind() string { return "preview_cleanup" }

// Queue names shared by workers and enqueuers.
const (
	QueueBuild   = "build"
	QueueDeploy  = "deploy"
	QueueDefault = river.QueueDefault
)
