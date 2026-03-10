package storage

import (
	"context"
	"strings"

	"github.com/docker/docker/api/types/mount"

	"github.com/lholliger/hive/internal/store"
)

// StorageClass describes the durability/mobility of a volume.
type StorageClass string

const (
	StorageClassLocal  StorageClass = "local"
	StorageClassShared StorageClass = "shared"
	StorageClassCeph   StorageClass = "ceph"
	StorageClassNamed  StorageClass = "named"
)

// ClassifyVolume returns the storage class of a volume based on its host and mount type.
func ClassifyVolume(vol *store.Volume, host *store.StorageHost) StorageClass {
	if host == nil {
		return StorageClassNamed
	}
	if host.Type == "ceph" || vol.MountType == "cephfs" || vol.MountType == "ceph-rbd" {
		return StorageClassCeph
	}
	if host.Type == "nfs" || host.Type == "cifs" {
		return StorageClassShared
	}
	return StorageClassLocal
}

// IsHASafe returns true if the storage class supports scheduling across
// multiple nodes (shared/ceph are safe; local/named are node-bound).
func (sc StorageClass) IsHASafe() bool {
	return sc == StorageClassShared || sc == StorageClassCeph
}

// ResolveVolumeMounts resolves each attached volume into optimal mount entries and
// any additional placement constraints required for local-bind optimization.
// The algorithm:
//   - No storage host → standard named volume mount
//   - Ceph volumes → always remote (no constraint needed)
//   - Single storage host for ALL volumes + no conflicting constraints → auto-pin to host node, use local bind
//   - Multiple storage hosts or existing conflicting constraints → NFS/CIFS remote mounts
func ResolveVolumeMounts(
	ctx context.Context,
	db *store.Store,
	appVolumes []store.AppVolume,
	existingConstraints []string,
) ([]mount.Mount, []string, error) {
	if len(appVolumes) == 0 {
		return nil, nil, nil
	}

	type resolvedVol struct {
		vol  *store.Volume
		av   store.AppVolume
		host *store.StorageHost
	}

	var resolved []resolvedVol
	storageHostIDs := make(map[string]*store.StorageHost)
	var distinctHostIDs []string

	for _, av := range appVolumes {
		vol, err := db.GetVolume(ctx, av.VolumeID)
		if err != nil {
			return nil, nil, err
		}

		var host *store.StorageHost
		if vol.StorageHostID != "" {
			if cached, ok := storageHostIDs[vol.StorageHostID]; ok {
				host = cached
			} else {
				h, err := db.GetStorageHost(ctx, vol.StorageHostID)
				if err != nil {
					return nil, nil, err
				}
				host = h
				storageHostIDs[vol.StorageHostID] = h
				distinctHostIDs = append(distinctHostIDs, vol.StorageHostID)
			}
		}

		resolved = append(resolved, resolvedVol{vol: vol, av: av, host: host})
	}

	singleHost := len(distinctHostIDs) == 1
	var pinnedHost *store.StorageHost
	if singleHost {
		pinnedHost = storageHostIDs[distinctHostIDs[0]]
	}

	alreadyPinned := isAlreadyPinnedToHost(existingConstraints, pinnedHost)

	useLocalBind := singleHost && pinnedHost != nil && pinnedHost.NodeID != "" &&
		pinnedHost.Type != "ceph" &&
		(alreadyPinned || !hasConflictingNodeConstraint(existingConstraints))

	var mounts []mount.Mount
	var addedConstraints []string

	for _, rv := range resolved {
		m, constraint := resolveOneMount(rv.vol, rv.av, rv.host, useLocalBind, pinnedHost)
		mounts = append(mounts, m)
		if constraint != "" {
			addedConstraints = append(addedConstraints, constraint)
		}
	}

	return mounts, dedup(addedConstraints), nil
}

func resolveOneMount(vol *store.Volume, av store.AppVolume, host *store.StorageHost, useLocalBind bool, pinnedHost *store.StorageHost) (mount.Mount, string) {
	if host == nil {
		return mount.Mount{
			Type:     mount.TypeVolume,
			Source:   vol.Name,
			Target:   av.ContainerPath,
			ReadOnly: av.ReadOnly,
		}, ""
	}

	if host.Type == "ceph" || vol.MountType == "cephfs" || vol.MountType == "ceph-rbd" {
		return mount.Mount{
			Type:     mount.TypeVolume,
			Source:   vol.Name,
			Target:   av.ContainerPath,
			ReadOnly: av.ReadOnly,
		}, ""
	}

	if useLocalBind && vol.LocalPath != "" {
		constraint := ""
		if pinnedHost != nil && pinnedHost.NodeLabel != "" {
			labelKey := strings.SplitN(pinnedHost.NodeLabel, "=", 2)[0]
			constraint = "node.labels." + labelKey + "==true"
		}
		return mount.Mount{
			Type:     mount.TypeBind,
			Source:   vol.LocalPath,
			Target:   av.ContainerPath,
			ReadOnly: av.ReadOnly,
		}, constraint
	}

	return mount.Mount{
		Type:     mount.TypeVolume,
		Source:   vol.Name,
		Target:   av.ContainerPath,
		ReadOnly: av.ReadOnly,
	}, ""
}

func isAlreadyPinnedToHost(constraints []string, host *store.StorageHost) bool {
	if host == nil || host.NodeLabel == "" {
		return false
	}
	labelKey := strings.SplitN(host.NodeLabel, "=", 2)[0]
	needle := "node.labels." + labelKey
	for _, c := range constraints {
		if strings.Contains(c, needle) {
			return true
		}
	}
	return false
}

func hasConflictingNodeConstraint(constraints []string) bool {
	for _, c := range constraints {
		if strings.HasPrefix(c, "node.id") || strings.HasPrefix(c, "node.hostname") {
			return true
		}
	}
	return false
}

// ValidateHAStorage checks all volumes for an app and returns a warning
// message if any use node-bound storage with multi-replica deployments.
// This is advisory - it does not block deployments.
func ValidateHAStorage(
	ctx context.Context,
	db *store.Store,
	appVolumes []store.AppVolume,
	replicas int,
) string {
	if replicas <= 1 || len(appVolumes) == 0 {
		return ""
	}

	for _, av := range appVolumes {
		vol, err := db.GetVolume(ctx, av.VolumeID)
		if err != nil {
			continue
		}
		var host *store.StorageHost
		if vol.StorageHostID != "" {
			h, err := db.GetStorageHost(ctx, vol.StorageHostID)
			if err == nil {
				host = h
			}
		}
		sc := ClassifyVolume(vol, host)
		if !sc.IsHASafe() {
			return "volume " + vol.Name + " uses node-bound storage (" + string(sc) +
				"); multi-replica deployment may not work correctly across nodes"
		}
	}
	return ""
}

func dedup(ss []string) []string {
	if len(ss) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	var out []string
	for _, s := range ss {
		if _, ok := seen[s]; !ok {
			seen[s] = struct{}{}
			out = append(out, s)
		}
	}
	return out
}
