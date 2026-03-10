package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/filters"
	dockerswarm "github.com/docker/docker/api/types/swarm"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/internal/swarm"
)

type ServiceUpdater struct {
	sc  *swarm.Client
	nc  *nats.Conn
	db  *store.Store
	log *zap.SugaredLogger
}

func NewServiceUpdater(sc *swarm.Client, nc *nats.Conn, db *store.Store, log *zap.SugaredLogger) *ServiceUpdater {
	return &ServiceUpdater{sc: sc, nc: nc, db: db, log: log}
}

func (su *ServiceUpdater) UpdateService(ctx context.Context, serviceName, newImage, triggeredBy string) error {
	svc, err := su.sc.GetService(ctx, serviceName)
	if err != nil {
		return fmt.Errorf("get service: %w", err)
	}
	if svc == nil {
		return fmt.Errorf("service %s not found", serviceName)
	}

	oldImage := svc.Spec.TaskTemplate.ContainerSpec.Image
	cleanOld, _ := parseImageWithDigest(oldImage)

	appID := svc.Spec.Labels["hive.app_id"]

	event := &store.UpdateEvent{
		EventType:       "service_image",
		TargetType:      "app",
		TargetID:        appID,
		TargetName:      serviceName,
		PreviousVersion: cleanOld,
		NewVersion:      newImage,
		Status:          "running",
		TriggeredBy:     triggeredBy,
	}
	if su.db != nil {
		_ = su.db.CreateUpdateEvent(ctx, event)
	}

	su.broadcastProgress(serviceName, "running", "Starting service update...", 10)

	if su.db != nil && appID != "" {
		if su.hasLinkedDatabase(ctx, appID) {
			policy := su.getPolicy(ctx, appID)
			if policy != nil && policy.PreUpdateBackup {
				su.broadcastProgress(serviceName, "running", "Backing up linked database...", 20)
				if err := su.triggerBackup(ctx, appID); err != nil {
					su.log.Warnf("pre-update backup failed for %s: %v", serviceName, err)
				} else {
					su.broadcastProgress(serviceName, "running", "Database backup complete", 30)
				}
			}
		}
	}

	su.broadcastProgress(serviceName, "running", "Updating service image...", 50)

	svc.Spec.TaskTemplate.ContainerSpec.Image = newImage
	if err := su.sc.UpdateService(ctx, svc.ID, svc.Version, svc.Spec); err != nil {
		su.broadcastProgress(serviceName, "failed", "Service update failed: "+err.Error(), 0)
		if su.db != nil && event.ID != "" {
			_ = su.db.UpdateUpdateEvent(ctx, event.ID, "failed", err.Error())
		}
		return fmt.Errorf("update service: %w", err)
	}

	su.broadcastProgress(serviceName, "running", "Waiting for service to stabilize...", 70)

	healthy := su.waitForHealthy(ctx, serviceName, 5*time.Minute)
	if !healthy {
		su.broadcastProgress(serviceName, "running", "Health check failed, rolling back...", 80)
		su.log.Warnf("service %s unhealthy after update, rolling back", serviceName)

		if err := su.sc.RollbackService(ctx, svc.ID); err != nil {
			su.log.Errorf("rollback %s failed: %v", serviceName, err)
			su.broadcastProgress(serviceName, "failed", "Rollback failed: "+err.Error(), 0)
			if su.db != nil && event.ID != "" {
				_ = su.db.UpdateUpdateEvent(ctx, event.ID, "failed", "update and rollback failed")
			}
			return fmt.Errorf("rollback failed: %w", err)
		}

		su.broadcastProgress(serviceName, "rolled_back", "Service rolled back to previous version", 100)
		if su.db != nil && event.ID != "" {
			_ = su.db.UpdateUpdateEvent(ctx, event.ID, "rolled_back", "service unhealthy after update")
		}
		return fmt.Errorf("service unhealthy after update, rolled back")
	}

	su.broadcastProgress(serviceName, "completed", "Service updated successfully", 100)
	if su.db != nil && event.ID != "" {
		_ = su.db.UpdateUpdateEvent(ctx, event.ID, "success", "")
	}

	if su.db != nil {
		sus := &store.ServiceUpdateStatus{
			AppID:           appID,
			ServiceName:     serviceName,
			CurrentImage:    newImage,
			UpdateAvailable: false,
		}
		_ = su.db.UpsertServiceUpdateStatus(ctx, sus)
	}

	su.log.Infof("service %s updated: %s -> %s", serviceName, cleanOld, newImage)
	return nil
}

func (su *ServiceUpdater) waitForHealthy(ctx context.Context, serviceName string, timeout time.Duration) bool {
	deadline := time.After(timeout)
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// Give the service a moment to start rolling out
	time.Sleep(15 * time.Second)

	for {
		select {
		case <-ctx.Done():
			return false
		case <-deadline:
			return false
		case <-ticker.C:
			svc, err := su.sc.GetService(ctx, serviceName)
			if err != nil || svc == nil {
				continue
			}

			desired := uint64(1)
			if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil {
				desired = *svc.Spec.Mode.Replicated.Replicas
			}

			tasks, err := su.sc.Docker().TaskList(ctx, dockerswarm.TaskListOptions{
				Filters: filters.NewArgs(
					filters.Arg("service", svc.ID),
					filters.Arg("desired-state", "running"),
				),
			})
			if err != nil {
				continue
			}

			running := uint64(0)
			for _, t := range tasks {
				if t.Status.State == "running" {
					running++
				}
			}

			if running >= desired && desired > 0 {
				return true
			}
		}
	}
}

func (su *ServiceUpdater) hasLinkedDatabase(ctx context.Context, appID string) bool {
	links, err := su.db.ListServiceLinks(ctx, appID)
	if err != nil {
		return false
	}
	for _, link := range links {
		if link.TargetDatabaseID != "" {
			return true
		}
	}
	return false
}

func (su *ServiceUpdater) getPolicy(ctx context.Context, appID string) *store.UpdatePolicy {
	policies, err := su.db.ListAutoUpdatePolicies(ctx)
	if err != nil {
		return nil
	}
	for _, p := range policies {
		if p.TargetID == appID || p.TargetType == "global" {
			return &p
		}
	}
	return nil
}

func (su *ServiceUpdater) triggerBackup(ctx context.Context, appID string) error {
	payload, _ := json.Marshal(map[string]string{
		"resource_id": appID,
		"backup_type": "database",
	})
	msg, err := su.nc.Request("hive.backup", payload, 5*time.Minute)
	if err != nil {
		return err
	}
	var resp struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(msg.Data, &resp)
	if resp.Status == "failed" {
		return fmt.Errorf("backup failed")
	}
	return nil
}

func (su *ServiceUpdater) broadcastProgress(serviceName, status, message string, progress int) {
	data, _ := json.Marshal(map[string]any{
		"type": "service_update_progress",
		"payload": map[string]any{
			"service_name": serviceName,
			"status":       status,
			"message":      message,
			"progress":     progress,
		},
		"ts": time.Now().Unix(),
	})
	getUpdatesHub().broadcast(data)
}
