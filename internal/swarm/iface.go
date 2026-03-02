package swarm

import (
	"context"
	"io"

	"github.com/docker/docker/api/types/swarm"
)

type ServiceAPI interface {
	ServiceExists(ctx context.Context, name string) (bool, error)
	CreateService(ctx context.Context, spec swarm.ServiceSpec) error
	UpdateService(ctx context.Context, serviceID string, version swarm.Version, spec swarm.ServiceSpec) error
	RemoveService(ctx context.Context, serviceID string) error
	GetService(ctx context.Context, name string) (*swarm.Service, error)
	ListServices(ctx context.Context) ([]swarm.Service, error)
	ServiceLogs(ctx context.Context, serviceID string, tail string, follow bool) (io.ReadCloser, error)
	ScaleService(ctx context.Context, serviceID string, replicas uint64) error
	RollbackService(ctx context.Context, serviceID string) error
}
