package bootstrap

import (
	"context"

	"github.com/docker/docker/api/types/network"
)

const hivenetName = "hive-net"

func (b *Bootstrapper) ensureNetwork(ctx context.Context) error {
	return b.swarm.EnsureNetwork(ctx, hivenetName, network.CreateOptions{
		Driver:     "overlay",
		Attachable: true,
		Labels: map[string]string{
			"hive.managed": "true",
		},
	})
}
