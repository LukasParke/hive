package monitor

import (
	"context"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
)

type ServiceHealth struct {
	ServiceName string `json:"service_name"`
	Replicas    uint64 `json:"replicas"`
	Running     uint64 `json:"running"`
	Healthy     bool   `json:"healthy"`
}

func (c *Collector) CheckServices(ctx context.Context) ([]ServiceHealth, error) {
	services, err := c.docker.ServiceList(ctx, swarm.ServiceListOptions{
		Filters: filters.NewArgs(filters.Arg("label", "hive.managed=true")),
	})
	if err != nil {
		return nil, err
	}

	var results []ServiceHealth
	for _, svc := range services {
		desired := uint64(0)
		if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil {
			desired = *svc.Spec.Mode.Replicated.Replicas
		}

		tasks, err := c.docker.TaskList(ctx, swarm.TaskListOptions{
			Filters: filters.NewArgs(
				filters.Arg("service", svc.ID),
				filters.Arg("desired-state", "running"),
			),
		})
		if err != nil {
			c.log.Warnf("task list for %s: %v", svc.Spec.Name, err)
			continue
		}

		running := uint64(0)
		for _, t := range tasks {
			if t.Status.State == "running" {
				running++
			}
		}

		results = append(results, ServiceHealth{
			ServiceName: svc.Spec.Name,
			Replicas:    desired,
			Running:     running,
			Healthy:     running >= desired,
		})
	}
	return results, nil
}
