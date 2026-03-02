package swarm

import (
	"context"

	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/api/types/filters"
)

type EventHandler func(event events.Message)

func (c *Client) WatchEvents(ctx context.Context, handler EventHandler) {
	f := filters.NewArgs()
	f.Add("type", "node")
	f.Add("type", "service")

	msgCh, errCh := c.docker.Events(ctx, events.ListOptions{Filters: f})

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-msgCh:
				handler(msg)
			case err := <-errCh:
				if err != nil {
					c.log.Warnf("docker event stream error: %v", err)
				}
				return
			}
		}
	}()
}
