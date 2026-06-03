package riverjobs

import (
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	buildruntime "github.com/luke/hive/control-plane/internal/build"
	"github.com/luke/hive/control-plane/internal/notify"
	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
)

func NewClient(pool *pgxpool.Pool, registryHost string, swarm *swarmclient.Client, buildkit *buildruntime.Client, notifier *notify.Dispatcher) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, &BuildWorker{
		Pool:         pool,
		RegistryHost: registryHost,
		Swarm:        swarm,
		Buildkit:     buildkit,
		Notifier:     notifier,
	})
	river.AddWorker(workers, &CleanupWorker{})
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
			river.QueueDefault: {MaxWorkers: 5},
		},
		Workers: workers,
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(
				river.PeriodicInterval(15*time.Minute),
				func() (river.JobArgs, *river.InsertOpts) {
					return PreviewCleanupJobArgs{}, nil
				},
				&river.PeriodicJobOpts{RunOnStart: true},
			),
		},
	})
}
