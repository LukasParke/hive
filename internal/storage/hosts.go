package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/internal/swarm"
	"github.com/lholliger/hive/pkg/encryption"
)

type HostCapabilities struct {
	NFS             bool `json:"nfs"`
	CIFS            bool `json:"cifs"`
	CephFS          bool `json:"cephfs"`
	CephRBD         bool `json:"rbd"`
	SMBMultichannel bool `json:"smb_multichannel"`
}

func ParseCapabilities(raw []byte) HostCapabilities {
	var caps HostCapabilities
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &caps); err != nil {
			log.Printf("failed to parse host capabilities: %v", err)
		}
	}
	return caps
}

// ApplyNodeLabel sets the storage host's label on the corresponding Docker Swarm node.
func ApplyNodeLabel(ctx context.Context, sc *swarm.Client, host *store.StorageHost) error {
	if host.NodeID == "" || host.NodeLabel == "" {
		return nil
	}

	parts := strings.SplitN(host.NodeLabel, "=", 2)
	key := parts[0]
	value := "true"
	if len(parts) == 2 {
		value = parts[1]
	}

	labels := map[string]string{key: value}
	return sc.UpdateNodeLabels(ctx, host.NodeID, labels)
}

// DecryptCredentials returns the decrypted credentials for a storage host.
func DecryptCredentials(host *store.StorageHost) (string, error) {
	if len(host.CredentialsEncrypted) == 0 {
		return "", nil
	}
	plain, err := encryption.Decrypt(host.CredentialsEncrypted)
	if err != nil {
		return "", fmt.Errorf("decrypt credentials for %s: %w", host.Name, err)
	}
	return string(plain), nil
}

// CephMonitorAddresses parses the comma-separated monitor addresses from a Ceph storage host.
func CephMonitorAddresses(host *store.StorageHost) []string {
	if host.Type != "ceph" {
		return nil
	}
	addrs := strings.Split(host.Address, ",")
	var trimmed []string
	for _, a := range addrs {
		a = strings.TrimSpace(a)
		if a != "" {
			trimmed = append(trimmed, a)
		}
	}
	return trimmed
}

// IsCephHost returns true if the storage host is a Ceph cluster.
func IsCephHost(host *store.StorageHost) bool {
	return host.Type == "ceph"
}

// SupportsCapability checks if a storage host supports a specific mount type.
func SupportsCapability(host *store.StorageHost, mountType string) bool {
	caps := ParseCapabilities(host.Capabilities)
	switch mountType {
	case "nfs":
		return caps.NFS
	case "cifs":
		return caps.CIFS
	case "cephfs":
		return caps.CephFS
	case "ceph-rbd", "rbd":
		return caps.CephRBD
	default:
		return true
	}
}
