package storage

import (
	"testing"

	"github.com/docker/docker/api/types/mount"
	"github.com/stretchr/testify/assert"

	"github.com/lholliger/hive/internal/store"
)

func TestDedup(t *testing.T) {
	result := dedup([]string{"a", "b", "a", "c", "b"})
	assert.Len(t, result, 3)
	assert.Contains(t, result, "a")
	assert.Contains(t, result, "b")
	assert.Contains(t, result, "c")
}

func TestDedupEmpty(t *testing.T) {
	assert.Nil(t, dedup(nil))
	assert.Nil(t, dedup([]string{}))
}

func TestHasConflictingNodeConstraint(t *testing.T) {
	assert.True(t, hasConflictingNodeConstraint([]string{"node.id==abc123"}))
	assert.True(t, hasConflictingNodeConstraint([]string{"node.hostname==myhost"}))
	assert.False(t, hasConflictingNodeConstraint([]string{"node.labels.hive.storage.nas==true"}))
	assert.False(t, hasConflictingNodeConstraint(nil))
}

func TestIsAlreadyPinnedToHost(t *testing.T) {
	assert.False(t, isAlreadyPinnedToHost(nil, nil))
	h1 := storageHostForTest("nas")
	assert.True(t, isAlreadyPinnedToHost(
		[]string{"node.labels.hive.storage.nas==true"},
		&h1,
	))
	h2 := storageHostForTest("nas")
	assert.False(t, isAlreadyPinnedToHost(
		[]string{"node.labels.other==true"},
		&h2,
	))
}

func storageHostForTest(name string) store.StorageHost {
	return store.StorageHost{
		ID:        "sh-1",
		Name:      name,
		NodeID:    "node-1",
		NodeLabel: "hive.storage." + name + "=true",
		Type:      "nas",
	}
}

func TestResolveOneMountNoHost(t *testing.T) {
	vol := &store.Volume{Name: "test-vol", MountType: "volume"}
	av := store.AppVolume{ContainerPath: "/data"}

	m, constraint := resolveOneMount(vol, av, nil, false, nil)
	assert.Equal(t, mount.TypeVolume, m.Type)
	assert.Equal(t, "test-vol", m.Source)
	assert.Equal(t, "/data", m.Target)
	assert.Empty(t, constraint)
}

func TestResolveOneMountCeph(t *testing.T) {
	host := &store.StorageHost{Type: "ceph"}
	vol := &store.Volume{Name: "ceph-vol", MountType: "cephfs"}
	av := store.AppVolume{ContainerPath: "/ceph-data"}

	m, constraint := resolveOneMount(vol, av, host, false, nil)
	assert.Equal(t, mount.TypeVolume, m.Type)
	assert.Equal(t, "ceph-vol", m.Source)
	assert.Empty(t, constraint)
}

func TestResolveOneMountLocalBind(t *testing.T) {
	host := storageHostForTest("nas")
	hostPtr := &host
	vol := &store.Volume{Name: "nas-vol", MountType: "nfs", LocalPath: "/mnt/pool/docker/app"}
	av := store.AppVolume{ContainerPath: "/data"}

	m, constraint := resolveOneMount(vol, av, hostPtr, true, hostPtr)
	assert.Equal(t, mount.TypeBind, m.Type)
	assert.Equal(t, "/mnt/pool/docker/app", m.Source)
	assert.Equal(t, "/data", m.Target)
	assert.Contains(t, constraint, "hive.storage.nas")
}

func TestResolveOneMountFallbackToRemote(t *testing.T) {
	host := storageHostForTest("nas")
	hostPtr := &host
	vol := &store.Volume{Name: "nas-vol", MountType: "nfs", LocalPath: "/mnt/pool/docker/app"}
	av := store.AppVolume{ContainerPath: "/data"}

	m, constraint := resolveOneMount(vol, av, hostPtr, false, nil)
	assert.Equal(t, mount.TypeVolume, m.Type)
	assert.Equal(t, "nas-vol", m.Source)
	assert.Empty(t, constraint)
}
