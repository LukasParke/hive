package riverjobs

import (
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivertype"

	"github.com/luke/hive/control-plane/internal/backup"
	buildruntime "github.com/luke/hive/control-plane/internal/build"
	"github.com/luke/hive/control-plane/internal/deploy"
	"github.com/luke/hive/control-plane/internal/notify"
	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
)

// Option configures optional collaborators on the river workers.
type Option func(*workerDeps)

type workerDeps struct {
	fanout       deploy.Emitter
	certRenewer  CertRenewer
	backupRunner *backup.Runner
}

// WithFanout injects the NOTIFY fan-out used by the DeployWorker to emit
// deployment:{appID} notifications.
func WithFanout(fanout deploy.Emitter) Option {
	return func(d *workerDeps) { d.fanout = fanout }
}

// WithCertRenewer injects the control-plane certificate renewal hook.
func WithCertRenewer(renewer CertRenewer) Option {
	return func(d *workerDeps) { d.certRenewer = renewer }
}

// WithBackupRunner injects the backup runner used by BackupWorker.
func WithBackupRunner(runner *backup.Runner) Option {
	return func(d *workerDeps) { d.backupRunner = runner }
}

// NewClient builds the River client with all job workers registered.
// Queue concurrency: build max 2, deploy max 5, everything else default
// (max 1). Periodic jobs are NOT started here — call StartPeriodicJobs
// once leadership is acquired.
func NewClient(pool *pgxpool.Pool, registryHost string, swarm *swarmclient.Client, buildkit *buildruntime.Client, notifier *notify.Dispatcher, opts ...Option) (*river.Client[pgx.Tx], error) {
	deps := &workerDeps{}
	for _, opt := range opts {
		opt(deps)
	}
	backupRunner := deps.backupRunner
	if backupRunner == nil && pool != nil {
		backupRunner = backup.NewRunner(pool, notifier)
	}

	buildWorker := &BuildWorker{
		Pool:         pool,
		RegistryHost: registryHost,
		Swarm:        swarm,
		Buildkit:     buildkit,
		Notifier:     notifier,
	}
	_ = buildWorker

	workers := river.NewWorkers()
	river.AddWorker(workers, buildWorker)
	river.AddWorker(workers, &DeployWorker{Pool: pool, Swarm: swarm, Fanout: deps.fanout})
	river.AddWorker(workers, &CleanupWorker{Pool: pool, Swarm: swarm})
	river.AddWorker(workers, &BackupWorker{Runner: backupRunner})
	river.AddWorker(workers, &CertRenewalWorker{Renewer: deps.certRenewer})
	river.AddWorker(workers, &PreviewDeployWorker{
		Pool:         pool,
		RegistryHost: registryHost,
		Swarm:        swarm,
		Buildkit:     buildkit,
		Notifier:     notifier,
	})
	river.AddWorker(workers, &PreviewCleanupWorker{Pool: pool, Swarm: swarm})

	return river.NewClient(riverpgxv5.New(pool), &river.Config{
		Queues: map[string]river.QueueConfig{
			QueueBuild:   {MaxWorkers: 2},
			QueueDeploy:  {MaxWorkers: 5},
			QueueDefault: {MaxWorkers: 1},
		},
		Workers: workers,
	})
}

// StartPeriodicJobs registers the recurring jobs (preview cleanup daily,
// cleanup daily, cert renewal hourly). It must be called only by the
// elected leader so periodic work does not run on every replica; regular
// job workers start on all replicas as usual via client.Start.
func StartPeriodicJobs(client *river.Client[pgx.Tx]) []rivertype.PeriodicJobHandle {
	return client.PeriodicJobs().AddMany([]*river.PeriodicJob{
		river.NewPeriodicJob(
			river.PeriodicInterval(24*time.Hour),
			func() (river.JobArgs, *river.InsertOpts) { return PreviewCleanupJobArgs{}, nil },
			nil,
		),
		river.NewPeriodicJob(
			river.PeriodicInterval(24*time.Hour),
			func() (river.JobArgs, *river.InsertOpts) { return CleanupJobArgs{}, nil },
			nil,
		),
		river.NewPeriodicJob(
			river.PeriodicInterval(time.Hour),
			func() (river.JobArgs, *river.InsertOpts) { return CertRenewalJobArgs{}, nil },
			nil,
		),
	})
}
