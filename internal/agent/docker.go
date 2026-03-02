package agent

import (
	"context"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
)

func collectDocker(report *NodeMetricsReport) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return
	}
	defer func() { _ = cli.Close() }()

	ctx := context.Background()

	running, err := cli.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(filters.Arg("status", "running")),
	})
	if err == nil {
		report.ContainersRunning = len(running)
	}

	all, err := cli.ContainerList(ctx, container.ListOptions{All: true})
	if err == nil {
		report.ContainersStopped = len(all) - report.ContainersRunning
	}

	images, err := cli.ImageList(ctx, image.ListOptions{})
	if err == nil {
		report.ImagesCount = len(images)
	}

	vols, err := cli.VolumeList(ctx, volume.ListOptions{})
	if err == nil {
		report.VolumesCount = len(vols.Volumes)
	}
}
