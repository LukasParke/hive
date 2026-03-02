package database

import (
	"context"
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

type Provisioner struct {
	swarm *hiveswarm.Client
	log   *zap.SugaredLogger
}

func NewProvisioner(sc *hiveswarm.Client, log *zap.SugaredLogger) *Provisioner {
	return &Provisioner{swarm: sc, log: log}
}

func (p *Provisioner) Provision(ctx context.Context, name, dbType, version string) (string, error) {
	image, ok := dbImages[dbType]
	if !ok {
		return "", fmt.Errorf("unsupported database type: %s", dbType)
	}
	if version != "" && version != "latest" {
		image = fmt.Sprintf("%s:%s", dbType, version)
	}

	serviceName := fmt.Sprintf("hive-db-%s", name)
	password := fmt.Sprintf("hive-%s-pass", name)

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
			fmt.Sprintf("MYSQL_ROOT_PASSWORD=%s-root", password),
		}
	case "redis":
		env = []string{}
	case "mongo":
		env = []string{
			fmt.Sprintf("MONGO_INITDB_ROOT_USERNAME=%s", name),
			fmt.Sprintf("MONGO_INITDB_ROOT_PASSWORD=%s", password),
		}
	}

	replicas := uint64(1)
	spec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name: serviceName,
			Labels: map[string]string{
				"hive.managed":   "true",
				"hive.component": "database",
				"hive.db_type":   dbType,
				"hive.db_name":   name,
			},
		},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image: image,
				Env:   env,
				Mounts: []mount.Mount{
					{
						Type:   mount.TypeVolume,
						Source: fmt.Sprintf("hive-db-%s-data", name),
						Target: dbDataDir(dbType),
					},
				},
			},
			Networks: []swarm.NetworkAttachmentConfig{
				{Target: "hive-net"},
			},
			Placement: &swarm.Placement{
				Constraints: []string{"node.role == manager"},
			},
		},
		Mode: swarm.ServiceMode{
			Replicated: &swarm.ReplicatedService{Replicas: &replicas},
		},
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
