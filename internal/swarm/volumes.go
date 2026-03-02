package swarm

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/volume"
)

func (c *Client) CreateVolume(ctx context.Context, name, driver string, driverOpts, labels map[string]string) (volume.Volume, error) {
	if labels == nil {
		labels = make(map[string]string)
	}
	labels["hive.managed"] = "true"

	if driver == "" {
		driver = "local"
	}

	vol, err := c.docker.VolumeCreate(ctx, volume.CreateOptions{
		Name:       name,
		Driver:     driver,
		DriverOpts: driverOpts,
		Labels:     labels,
	})
	if err != nil {
		return volume.Volume{}, fmt.Errorf("volume create %s: %w", name, err)
	}
	c.log.Infof("created volume: %s (driver=%s)", name, driver)
	return vol, nil
}

func (c *Client) CreateNFSVolume(ctx context.Context, name, host, path, mountOpts string, labels map[string]string) (volume.Volume, error) {
	opts := fmt.Sprintf("addr=%s", host)
	if mountOpts != "" {
		opts = opts + "," + mountOpts
	}

	device := ":" + path

	driverOpts := map[string]string{
		"type":   "nfs",
		"device": device,
		"o":      opts,
	}

	if labels == nil {
		labels = make(map[string]string)
	}
	labels["hive.mount_type"] = "nfs"
	labels["hive.remote_host"] = host

	return c.CreateVolume(ctx, name, "local", driverOpts, labels)
}

func (c *Client) CreateCIFSVolume(ctx context.Context, name, host, share, username, password, mountOpts string, labels map[string]string) (volume.Volume, error) {
	var optParts []string
	optParts = append(optParts, fmt.Sprintf("addr=%s", host))
	if username != "" {
		optParts = append(optParts, fmt.Sprintf("username=%s", username))
	}
	if password != "" {
		optParts = append(optParts, fmt.Sprintf("password=%s", password))
	}
	if mountOpts != "" {
		optParts = append(optParts, mountOpts)
	}

	device := fmt.Sprintf("//%s%s", host, share)

	driverOpts := map[string]string{
		"type":   "cifs",
		"device": device,
		"o":      strings.Join(optParts, ","),
	}

	if labels == nil {
		labels = make(map[string]string)
	}
	labels["hive.mount_type"] = "cifs"
	labels["hive.remote_host"] = host

	return c.CreateVolume(ctx, name, "local", driverOpts, labels)
}

func (c *Client) CreateCephFSVolume(ctx context.Context, name, monitors, fsName, path, mountOpts string, labels map[string]string) (volume.Volume, error) {
	device := monitors + ":" + path
	opts := "name=admin"
	if fsName != "" {
		opts += ",mds_namespace=" + fsName
	}
	if mountOpts != "" {
		opts += "," + mountOpts
	}

	driverOpts := map[string]string{
		"type":   "ceph",
		"device": device,
		"o":      opts,
	}

	if labels == nil {
		labels = make(map[string]string)
	}
	labels["hive.mount_type"] = "cephfs"

	return c.CreateVolume(ctx, name, "local", driverOpts, labels)
}

func (c *Client) CreateCephRBDVolume(ctx context.Context, name, monitors, pool, image, mountOpts string, labels map[string]string) (volume.Volume, error) {
	driverOpts := map[string]string{
		"type":   "rbd",
		"device": fmt.Sprintf("%s/%s", pool, image),
		"o":      fmt.Sprintf("mon_host=%s", monitors),
	}
	if mountOpts != "" {
		driverOpts["o"] += "," + mountOpts
	}

	if labels == nil {
		labels = make(map[string]string)
	}
	labels["hive.mount_type"] = "ceph-rbd"

	return c.CreateVolume(ctx, name, "local", driverOpts, labels)
}

func (c *Client) ListVolumes(ctx context.Context, labelFilter string) ([]*volume.Volume, error) {
	opts := volume.ListOptions{}
	if labelFilter != "" {
		opts.Filters = filters.NewArgs(filters.Arg("label", labelFilter))
	}
	resp, err := c.docker.VolumeList(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("volume list: %w", err)
	}
	return resp.Volumes, nil
}

func (c *Client) GetVolume(ctx context.Context, name string) (volume.Volume, error) {
	vol, err := c.docker.VolumeInspect(ctx, name)
	if err != nil {
		return volume.Volume{}, fmt.Errorf("volume inspect %s: %w", name, err)
	}
	return vol, nil
}

func (c *Client) RemoveVolume(ctx context.Context, name string, force bool) error {
	if err := c.docker.VolumeRemove(ctx, name, force); err != nil {
		return fmt.Errorf("volume remove %s: %w", name, err)
	}
	c.log.Infof("removed volume: %s", name)
	return nil
}
