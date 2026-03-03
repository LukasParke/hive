package bootstrap

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

func (b *Bootstrapper) ensurePostgresSecret(ctx context.Context) error {
	secrets, err := b.swarm.ListSecrets(ctx, "hive.managed=true")
	if err != nil {
		return fmt.Errorf("list secrets: %w", err)
	}
	for _, s := range secrets {
		if s.Spec.Name == postgresSecretName {
			b.pgSecretID = s.ID
			b.log.Infof("postgres secret already exists (id=%s)", s.ID)
			return nil
		}
	}

	password, err := b.getOrCreatePostgresPassword()
	if err != nil {
		return fmt.Errorf("postgres password: %w", err)
	}

	id, err := b.swarm.CreateSecret(ctx, postgresSecretName, []byte(password), map[string]string{
		"hive.component": "postgres",
	})
	if err != nil {
		return fmt.Errorf("create secret: %w", err)
	}
	b.pgSecretID = id
	return nil
}

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
					"POSTGRES_DB=" + postgresDB,
					"POSTGRES_USER=" + postgresUser,
					"POSTGRES_PASSWORD_FILE=/run/secrets/" + postgresSecretName,
				},
				Secrets: []*swarm.SecretReference{{
					SecretID:   b.pgSecretID,
					SecretName: postgresSecretName,
					File: &swarm.SecretReferenceFileTarget{
						Name: postgresSecretName,
						UID:  "0", GID: "0", Mode: 0400,
					},
				}},
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

func (b *Bootstrapper) postgresPassword() (string, error) {
	secretPath := "/run/secrets/" + postgresSecretName
	if data, err := os.ReadFile(secretPath); err == nil && len(data) > 0 {
		return strings.TrimSpace(string(data)), nil
	}
	return b.getOrCreatePostgresPassword()
}

func (b *Bootstrapper) postgresURL() (string, error) {
	password, err := b.postgresPassword()
	if err != nil {
		return "", fmt.Errorf("failed to get postgres password: %w", err)
	}
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=disable",
		postgresUser, password, postgresServiceName, postgresPort, postgresDB,
	), nil
}
