package swarm

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
)

func (c *Client) CreateSecret(ctx context.Context, name string, data []byte, labels map[string]string) (string, error) {
	if labels == nil {
		labels = make(map[string]string)
	}
	labels["hive.managed"] = "true"

	resp, err := c.docker.SecretCreate(ctx, swarm.SecretSpec{
		Annotations: swarm.Annotations{
			Name:   name,
			Labels: labels,
		},
		Data: data,
	})
	if err != nil {
		return "", fmt.Errorf("secret create %s: %w", name, err)
	}
	c.log.Infof("created secret: %s (id=%s)", name, resp.ID)
	return resp.ID, nil
}

func (c *Client) ListSecrets(ctx context.Context, labelFilter string) ([]swarm.Secret, error) {
	opts := swarm.SecretListOptions{}
	if labelFilter != "" {
		opts.Filters = filters.NewArgs(filters.Arg("label", labelFilter))
	}
	secrets, err := c.docker.SecretList(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("secret list: %w", err)
	}
	return secrets, nil
}

func (c *Client) GetSecret(ctx context.Context, id string) (swarm.Secret, error) {
	secret, _, err := c.docker.SecretInspectWithRaw(ctx, id)
	if err != nil {
		return swarm.Secret{}, fmt.Errorf("secret inspect %s: %w", id, err)
	}
	return secret, nil
}

func (c *Client) UpdateSecret(ctx context.Context, id string, version swarm.Version, data []byte) error {
	secret, err := c.GetSecret(ctx, id)
	if err != nil {
		return err
	}
	secret.Spec.Data = data
	return c.docker.SecretUpdate(ctx, id, version, secret.Spec)
}

func (c *Client) RemoveSecret(ctx context.Context, id string) error {
	if err := c.docker.SecretRemove(ctx, id); err != nil {
		return fmt.Errorf("secret remove %s: %w", id, err)
	}
	return nil
}
