package storage

import (
	"encoding/json"
	"testing"

	"github.com/lholliger/hive/internal/store"
	"github.com/stretchr/testify/assert"
)

func TestParseCapabilities(t *testing.T) {
	raw, _ := json.Marshal(map[string]bool{"nfs": true, "cifs": false, "cephfs": true})
	caps := ParseCapabilities(raw)
	assert.True(t, caps.NFS)
	assert.False(t, caps.CIFS)
	assert.True(t, caps.CephFS)
}

func TestParseCapabilitiesEmpty(t *testing.T) {
	caps := ParseCapabilities(nil)
	assert.False(t, caps.NFS)
}

func TestCephMonitorAddresses(t *testing.T) {
	host := &store.StorageHost{Type: "ceph", Address: "10.0.0.1, 10.0.0.2, 10.0.0.3"}
	addrs := CephMonitorAddresses(host)
	assert.Len(t, addrs, 3)
	assert.Equal(t, "10.0.0.1", addrs[0])
}

func TestCephMonitorAddressesNonCeph(t *testing.T) {
	host := &store.StorageHost{Type: "nas", Address: "10.0.0.1"}
	addrs := CephMonitorAddresses(host)
	assert.Nil(t, addrs)
}

func TestIsCephHost(t *testing.T) {
	assert.True(t, IsCephHost(&store.StorageHost{Type: "ceph"}))
	assert.False(t, IsCephHost(&store.StorageHost{Type: "nas"}))
}

func TestSupportsCapability(t *testing.T) {
	caps, _ := json.Marshal(map[string]bool{"nfs": true, "cifs": false})
	host := &store.StorageHost{Capabilities: caps}
	assert.True(t, SupportsCapability(host, "nfs"))
	assert.False(t, SupportsCapability(host, "cifs"))
	assert.True(t, SupportsCapability(host, "unknown"))
}
