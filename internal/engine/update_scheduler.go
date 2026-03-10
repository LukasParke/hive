package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/internal/swarm"
)

type UpdateScheduler struct {
	sc          *swarm.Client
	nc          *nats.Conn
	db          *store.Store
	log         *zap.SugaredLogger
	updater     *ServiceUpdater
	updateCache *UpdateCache
}

func NewUpdateScheduler(sc *swarm.Client, nc *nats.Conn, db *store.Store, log *zap.SugaredLogger, updater *ServiceUpdater, cache *UpdateCache) *UpdateScheduler {
	return &UpdateScheduler{
		sc:          sc,
		nc:          nc,
		db:          db,
		log:         log,
		updater:     updater,
		updateCache: cache,
	}
}

func (us *UpdateScheduler) Run(ctx context.Context) {
	us.log.Info("update scheduler started")

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			us.evaluate(ctx)
		}
	}
}

func (us *UpdateScheduler) EvalOnce(ctx context.Context) error {
	us.evaluate(ctx)
	return nil
}

func (us *UpdateScheduler) evaluate(ctx context.Context) {
	policies, err := us.db.ListAutoUpdatePolicies(ctx)
	if err != nil {
		us.log.Warnf("update scheduler: list policies: %v", err)
		return
	}

	now := time.Now()

	for _, policy := range policies {
		if !us.inMaintenanceWindow(policy, now) {
			continue
		}

		switch policy.TargetType {
		case "node":
			us.evaluateNodePolicy(ctx, policy)
		case "app":
			us.evaluateAppPolicy(ctx, policy)
		case "global":
			us.evaluateGlobalPolicy(ctx, policy)
		}
	}
}

func (us *UpdateScheduler) evaluateNodePolicy(ctx context.Context, policy store.UpdatePolicy) {
	nodeID := policy.TargetID
	entry := us.updateCache.Get(nodeID)
	if entry == nil || entry.PendingCount == 0 {
		return
	}

	if policy.SecurityOnly && entry.SecurityCount == 0 {
		return
	}

	recentEvents, _ := us.db.ListUpdateEventsByTarget(ctx, "node", nodeID, 1)
	if len(recentEvents) > 0 {
		lastEvent := recentEvents[0]
		if time.Since(lastEvent.StartedAt) < 6*time.Hour {
			return
		}
	}

	action := "apt_upgrade"
	if policy.SecurityOnly {
		action = "apt_security_upgrade"
	}

	us.log.Infof("auto-update: applying %s to node %s (%d pending)", action, nodeID, entry.PendingCount)

	payload, _ := json.Marshal(map[string]any{
		"node_id": nodeID,
		"action":  action,
	})
	_ = us.nc.Publish(fmt.Sprintf("hive.node.maintenance.%s", nodeID), payload)

	event := &store.UpdateEvent{
		EventType:   "node_os",
		TargetType:  "node",
		TargetID:    nodeID,
		TargetName:  entry.Hostname,
		Status:      "running",
		Details:     action,
		TriggeredBy: "auto",
	}
	_ = us.db.CreateUpdateEvent(ctx, event)

	if policy.AutoRestart && entry.RebootRequired {
		go func() {
			time.Sleep(10 * time.Minute)
			rebootPayload, _ := json.Marshal(map[string]any{
				"node_id": nodeID,
				"action":  "reboot",
			})
			_ = us.nc.Publish(fmt.Sprintf("hive.node.maintenance.%s", nodeID), rebootPayload)
		}()
	}
}

func (us *UpdateScheduler) evaluateAppPolicy(ctx context.Context, policy store.UpdatePolicy) {
	appID := policy.TargetID

	app, err := us.db.GetApp(ctx, appID)
	if err != nil {
		return
	}

	serviceName := "hive-app-" + app.Name
	sus, err := us.db.GetServiceUpdateStatus(ctx, serviceName)
	if err != nil || !sus.UpdateAvailable {
		return
	}

	recentEvents, _ := us.db.ListUpdateEventsByTarget(ctx, "app", appID, 1)
	if len(recentEvents) > 0 && time.Since(recentEvents[0].StartedAt) < 1*time.Hour {
		return
	}

	newImage := sus.CurrentImage
	if sus.LatestVersion != "" {
		parts := splitImageTag(sus.CurrentImage)
		newImage = parts[0] + ":" + sus.LatestVersion
	}

	us.log.Infof("auto-update: updating service %s to %s", serviceName, newImage)
	if err := us.updater.UpdateService(ctx, serviceName, newImage, "auto"); err != nil {
		us.log.Warnf("auto-update: %s failed: %v", serviceName, err)
	}
}

func (us *UpdateScheduler) evaluateGlobalPolicy(ctx context.Context, policy store.UpdatePolicy) {
	// Global node updates (rolling, one at a time)
	entries := us.updateCache.GetAll()
	for nodeID, entry := range entries {
		if entry.PendingCount == 0 {
			continue
		}
		if policy.SecurityOnly && entry.SecurityCount == 0 {
			continue
		}

		recentEvents, _ := us.db.ListUpdateEventsByTarget(ctx, "node", nodeID, 1)
		if len(recentEvents) > 0 && time.Since(recentEvents[0].StartedAt) < 6*time.Hour {
			continue
		}

		action := "apt_upgrade"
		if policy.SecurityOnly {
			action = "apt_security_upgrade"
		}

		us.log.Infof("auto-update (global): applying %s to node %s", action, nodeID)
		payload, _ := json.Marshal(map[string]any{
			"node_id": nodeID,
			"action":  action,
		})
		_ = us.nc.Publish(fmt.Sprintf("hive.node.maintenance.%s", nodeID), payload)

		event := &store.UpdateEvent{
			EventType:   "node_os",
			TargetType:  "node",
			TargetID:    nodeID,
			TargetName:  entry.Hostname,
			Status:      "running",
			Details:     action,
			TriggeredBy: "auto",
		}
		_ = us.db.CreateUpdateEvent(ctx, event)

		// Rolling: only one node at a time
		break
	}

	// Global service updates (skip infrastructure services)
	statuses, err := us.db.ListServiceUpdateStatusesWithUpdates(ctx)
	if err != nil {
		return
	}
	for _, sus := range statuses {
		if isInfraService(sus.ServiceName) {
			continue
		}

		recentEvents, _ := us.db.ListUpdateEventsByTarget(ctx, "app", sus.AppID, 1)
		if len(recentEvents) > 0 && time.Since(recentEvents[0].StartedAt) < 1*time.Hour {
			continue
		}

		newImage := sus.CurrentImage
		if sus.LatestVersion != "" {
			parts := splitImageTag(sus.CurrentImage)
			newImage = parts[0] + ":" + sus.LatestVersion
		}

		us.log.Infof("auto-update (global): updating %s to %s", sus.ServiceName, newImage)
		if err := us.updater.UpdateService(ctx, sus.ServiceName, newImage, "auto"); err != nil {
			us.log.Warnf("auto-update: %s failed: %v", sus.ServiceName, err)
		}
	}
}

func (us *UpdateScheduler) inMaintenanceWindow(policy store.UpdatePolicy, now time.Time) bool {
	if policy.MaintenanceWindowStart == "" || policy.MaintenanceWindowEnd == "" {
		return true
	}

	startH, startM := parseTime(policy.MaintenanceWindowStart)
	endH, endM := parseTime(policy.MaintenanceWindowEnd)
	if startH < 0 || endH < 0 {
		return true
	}

	if policy.MaintenanceWindowDays != "" {
		dayName := strings.ToLower(now.Weekday().String()[:3])
		days := strings.ToLower(policy.MaintenanceWindowDays)
		if !strings.Contains(days, dayName) {
			return false
		}
	}

	nowMinutes := now.Hour()*60 + now.Minute()
	startMinutes := startH*60 + startM
	endMinutes := endH*60 + endM

	if startMinutes <= endMinutes {
		return nowMinutes >= startMinutes && nowMinutes <= endMinutes
	}
	// Wraps midnight
	return nowMinutes >= startMinutes || nowMinutes <= endMinutes
}

func parseTime(s string) (int, int) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return -1, -1
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return -1, -1
	}
	return h, m
}
