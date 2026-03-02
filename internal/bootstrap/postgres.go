package bootstrap

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"

	_ "github.com/lib/pq"
)

const (
	postgresServiceName   = "hive-postgres"
	postgresImage         = "postgres:16-alpine"
	postgresPort          = 5432
	postgresDB            = "hive"
	postgresUser          = "hive"
	postgresPasswordFile  = "hive-postgres-password"
	postgresSecretName    = "hive-pg-password"
)

func (b *Bootstrapper) ensurePostgres(ctx context.Context) error {
	exists, err := b.swarm.ServiceExists(ctx, postgresServiceName)
	if err != nil {
		return err
	}
	if exists {
		b.log.Info("postgres service already running")
		return nil
	}

	b.log.Info("deploying postgres service")

	password, err := b.getOrCreatePostgresPassword()
	if err != nil {
		return fmt.Errorf("postgres password: %w", err)
	}

	replicas := uint64(1)
	spec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name: postgresServiceName,
			Labels: map[string]string{
				"hive.managed":   "true",
				"hive.component": "postgres",
			},
		},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image: postgresImage,
				Env: []string{
					fmt.Sprintf("POSTGRES_DB=%s", postgresDB),
					fmt.Sprintf("POSTGRES_USER=%s", postgresUser),
					fmt.Sprintf("POSTGRES_PASSWORD=%s", password),
				},
				Mounts: []mount.Mount{
					{
						Type:   mount.TypeVolume,
						Source: "hive-postgres-data",
						Target: "/var/lib/postgresql/data",
					},
				},
			},
			Networks: []swarm.NetworkAttachmentConfig{
				{Target: hivenetName},
			},
			Placement: &swarm.Placement{
				Constraints: []string{"node.role == manager"},
			},
		},
		Mode: swarm.ServiceMode{
			Replicated: &swarm.ReplicatedService{Replicas: &replicas},
		},
	}

	return b.swarm.CreateService(ctx, spec)
}

func (b *Bootstrapper) getOrCreatePostgresPassword() (string, error) {
	pwFile := filepath.Join(b.cfg.DataDir, postgresPasswordFile)
	data, err := os.ReadFile(pwFile)
	if err == nil && len(data) > 0 {
		return string(data), nil
	}

	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate password: %w", err)
	}
	password := hex.EncodeToString(buf)

	if err := os.MkdirAll(b.cfg.DataDir, 0700); err != nil {
		return "", fmt.Errorf("create data dir: %w", err)
	}
	if err := os.WriteFile(pwFile, []byte(password), 0600); err != nil {
		return "", fmt.Errorf("persist password: %w", err)
	}
	b.log.Info("generated new postgres password")
	return password, nil
}

func (b *Bootstrapper) waitForPostgres(ctx context.Context) error {
	b.log.Info("waiting for postgres to become ready")
	dsn, err := b.postgresURL()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("postgres did not become ready within timeout")
		case <-ticker.C:
			db, err := sql.Open("postgres", dsn)
			if err != nil {
				continue
			}
			if err := db.PingContext(ctx); err != nil {
				_ = db.Close()
				continue
			}
			_ = db.Close()
			b.log.Info("postgres is ready")
			b.cfg.DatabaseURL = dsn
			return nil
		}
	}
}

func (b *Bootstrapper) postgresURL() (string, error) {
	password, err := b.getOrCreatePostgresPassword()
	if err != nil {
		return "", fmt.Errorf("failed to get postgres password: %w", err)
	}
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=disable",
		postgresUser, password, postgresServiceName, postgresPort, postgresDB,
	), nil
}
