package database

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"

	hiveswarm "github.com/lholliger/hive/internal/swarm"

	"go.uber.org/zap"
)

var dbImages = map[string]string{
	"postgres": "postgres:16-alpine",
	"mysql":    "mysql:8",
	"redis":    "redis:7-alpine",
	"mongo":    "mongo:7",
}

var haImages = map[string]string{
	"postgres": "bitnami/postgresql-repmgr:16",
	"mysql":    "bitnami/mysql:8.0",
	"redis":    "bitnami/redis:7.0",
	"mongo":    "mongo:7",
}

type ProvisionOptions struct {
	StorageMode   string // "local", "pinned", "remote", "ha"
	StorageHostID string
	NodeID        string
	NFSHost       string
	NFSPath       string
	Replicas      int
}

type Provisioner struct {
	swarm *hiveswarm.Client
	log   *zap.SugaredLogger
}

func NewProvisioner(sc *hiveswarm.Client, log *zap.SugaredLogger) *Provisioner {
	return &Provisioner{swarm: sc, log: log}
}

func generateSecurePassword() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (p *Provisioner) Provision(ctx context.Context, name, dbType, version string) (string, error) {
	return p.ProvisionWithOptions(ctx, name, dbType, version, ProvisionOptions{StorageMode: "local"})
}

func (p *Provisioner) ProvisionWithOptions(ctx context.Context, name, dbType, version string, opts ProvisionOptions) (string, error) {
	image, ok := dbImages[dbType]
	if !ok {
		return "", fmt.Errorf("unsupported database type: %s", dbType)
	}
	if version != "" && version != "latest" {
		image = fmt.Sprintf("%s:%s", dbType, version)
	}

	if opts.StorageMode == "" {
		opts.StorageMode = "local"
	}

	serviceName := fmt.Sprintf("hive-db-%s", name)
	password := generateSecurePassword()

	var env []string
	switch dbType {
	case "postgres":
		env = []string{
			fmt.Sprintf("POSTGRES_DB=%s", name),
			fmt.Sprintf("POSTGRES_USER=%s", name),
			fmt.Sprintf("POSTGRES_PASSWORD=%s", password),
		}
	case "mysql":
		env = []string{
			fmt.Sprintf("MYSQL_DATABASE=%s", name),
			fmt.Sprintf("MYSQL_USER=%s", name),
			fmt.Sprintf("MYSQL_PASSWORD=%s", password),
			fmt.Sprintf("MYSQL_ROOT_PASSWORD=%s", generateSecurePassword()),
		}
	case "redis":
		env = []string{}
	case "mongo":
		env = []string{
			fmt.Sprintf("MONGO_INITDB_ROOT_USERNAME=%s", name),
			fmt.Sprintf("MONGO_INITDB_ROOT_PASSWORD=%s", password),
		}
	}

	volName := fmt.Sprintf("hive-db-%s-data", name)
	var constraints []string
	replicas := uint64(1)

	switch opts.StorageMode {
	case "pinned":
		if opts.NodeID != "" {
			constraints = append(constraints, fmt.Sprintf("node.id == %s", opts.NodeID))
		} else {
			constraints = append(constraints, "node.role == manager")
		}

	case "remote":
		if opts.NFSHost != "" && opts.NFSPath != "" {
			labels := map[string]string{"hive.managed": "true", "hive.db_name": name}
			if _, err := p.swarm.CreateNFSVolume(ctx, volName, opts.NFSHost, opts.NFSPath, "", labels); err != nil {
				p.log.Warnf("db provision: create NFS volume %s: %v", volName, err)
			}
		}

	case "ha":
		if haImg, ok := haImages[dbType]; ok {
			image = haImg
		}
		if opts.Replicas > 1 {
			replicas = uint64(opts.Replicas)
		} else {
			replicas = 3
		}
		if dbType == "postgres" {
			env = append(env,
				"REPMGR_PRIMARY_HOST="+serviceName,
				"REPMGR_PARTNER_NODES="+serviceName,
				fmt.Sprintf("REPMGR_PASSWORD=%s", generateSecurePassword()),
				"REPMGR_NODE_NAME=node-$(hostname)",
				"REPMGR_NODE_NETWORK_NAME="+serviceName,
			)
		}
		if opts.StorageMode == "ha" && opts.NFSHost == "" {
			p.log.Warnf("db provision: HA mode for %s with local storage - replicas will not share data unless NFS is configured", name)
		}

	default:
		constraints = append(constraints, "node.role == manager")
	}

	spec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name: serviceName,
			Labels: map[string]string{
				"hive.managed":      "true",
				"hive.component":    "database",
				"hive.db_type":      dbType,
				"hive.db_name":      name,
				"hive.storage_mode": opts.StorageMode,
			},
		},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image: image,
				Env:   env,
				Mounts: []mount.Mount{
					{
						Type:   mount.TypeVolume,
						Source: volName,
						Target: dbDataDir(dbType),
					},
				},
			},
			Networks: []swarm.NetworkAttachmentConfig{
				{Target: "hive-net"},
			},
		},
		Mode: swarm.ServiceMode{
			Replicated: &swarm.ReplicatedService{Replicas: &replicas},
		},
	}

	if len(constraints) > 0 {
		spec.TaskTemplate.Placement = &swarm.Placement{Constraints: constraints}
	}

	if err := p.swarm.ValidatePlacement(ctx, constraints); err != nil {
		return "", fmt.Errorf("preflight: %w", err)
	}

	if err := p.swarm.CreateService(ctx, spec); err != nil {
		return "", err
	}

	connStr := connectionString(dbType, serviceName, name, password)
	return connStr, nil
}

func dbDataDir(dbType string) string {
	switch dbType {
	case "postgres":
		return "/var/lib/postgresql/data"
	case "mysql":
		return "/var/lib/mysql"
	case "redis":
		return "/data"
	case "mongo":
		return "/data/db"
	default:
		return "/data"
	}
}

func connectionString(dbType, host, name, password string) string {
	switch dbType {
	case "postgres":
		return fmt.Sprintf("postgres://%s:%s@%s:5432/%s?sslmode=disable", name, password, host, name)
	case "mysql":
		return fmt.Sprintf("%s:%s@tcp(%s:3306)/%s", name, password, host, name)
	case "redis":
		return fmt.Sprintf("redis://%s:6379", host)
	case "mongo":
		return fmt.Sprintf("mongodb://%s:%s@%s:27017/%s", name, password, host, name)
	default:
		return ""
	}
}
