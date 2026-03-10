package monitor

import (
	"context"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
)

type ServiceHealth struct {
	ServiceName string            `json:"service_name"`
	Replicas    uint64            `json:"replicas"`
	Running     uint64            `json:"running"`
	Healthy     bool              `json:"healthy"`
	IsGlobal    bool              `json:"is_global"`
	Nodes       []string          `json:"nodes"`
	Labels      map[string]string `json:"-"`
}

func (c *Collector) CheckServices(ctx context.Context) ([]ServiceHealth, error) {
	services, err := c.docker.ServiceList(ctx, swarm.ServiceListOptions{
		Filters: filters.NewArgs(filters.Arg("label", "hive.managed=true")),
	})
	if err != nil {
		return nil, err
	}

	nodes, err := c.docker.NodeList(ctx, dockertypes.NodeListOptions{})
	if err != nil {
		c.log.Warnf("node list for service health: %v", err)
		nodes = nil
	}

	nodeHostname := make(map[string]string, len(nodes))
	var activeNodes uint64
	for _, n := range nodes {
		nodeHostname[n.ID] = n.Description.Hostname
		if n.Status.State == swarm.NodeStateReady && n.Spec.Availability == swarm.NodeAvailabilityActive {
			activeNodes++
		}
	}

	var results []ServiceHealth
	for _, svc := range services {
		isGlobal := svc.Spec.Mode.Global != nil
		desired := uint64(0)

		if isGlobal {
			desired = activeNodes
		} else if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil {
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
		var taskNodes []string
		for _, t := range tasks {
			if t.Status.State == "running" {
				running++
				if hostname, ok := nodeHostname[t.NodeID]; ok {
					taskNodes = append(taskNodes, hostname)
				}
			}
		}
		if taskNodes == nil {
			taskNodes = []string{}
		}

		results = append(results, ServiceHealth{
			ServiceName: svc.Spec.Name,
			Replicas:    desired,
			Running:     running,
			Healthy:     running >= desired && desired > 0,
			IsGlobal:    isGlobal,
			Nodes:       taskNodes,
			Labels:      svc.Spec.Labels,
		})
	}
	return results, nil
}
