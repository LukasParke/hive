package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"github.com/lholliger/hive/internal/store"
)

type GitPoller struct {
	nc       *nats.Conn
	db       *store.Store
	log      *zap.SugaredLogger
	interval time.Duration
}

func NewGitPoller(nc *nats.Conn, db *store.Store, log *zap.SugaredLogger) *GitPoller {
	return &GitPoller{
		nc:       nc,
		db:       db,
		log:      log,
		interval: 15 * time.Minute,
	}
}

func (gp *GitPoller) Run(ctx context.Context) {
	gp.log.Info("git poller started (15m interval)")

	time.Sleep(2 * time.Minute)

	gp.pollAll(ctx)

	ticker := time.NewTicker(gp.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			gp.pollAll(ctx)
		}
	}
}

func (gp *GitPoller) PollOnce(ctx context.Context) error {
	gp.pollAll(ctx)
	return nil
}

func (gp *GitPoller) pollAll(ctx context.Context) {
	apps, err := gp.db.ListGitAppsWithAutoDeploy(ctx)
	if err != nil {
		gp.log.Warnf("git poller: list apps: %v", err)
		return
	}

	polled := 0
	triggered := 0
	for _, app := range apps {
		if app.GitRepo == "" || app.AutoDeployBranch == "" {
			continue
		}

		remoteHead := gp.getRemoteHead(app.GitRepo, app.GitBranch)
		if remoteHead == "" {
			continue
		}
		polled++

		deployments, err := gp.db.ListDeployments(ctx, app.ID)
		if err != nil || len(deployments) == 0 {
			continue
		}

		lastDeploy := deployments[0]
		if lastDeploy.CommitSHA == remoteHead {
			continue
		}

		if lastDeploy.Status == "building" || lastDeploy.Status == "deploying" {
			continue
		}

		gp.log.Infof("git poller: new commit for %s (%s -> %s)", app.Name, lastDeploy.CommitSHA[:8], remoteHead[:8])

		d := &store.Deployment{
			AppID:     app.ID,
			Status:    "building",
			CommitSHA: remoteHead,
		}
		if err := gp.db.CreateDeployment(ctx, d); err != nil {
			gp.log.Warnf("git poller: create deployment for %s: %v", app.Name, err)
			continue
		}

		payload, _ := json.Marshal(map[string]any{
			"job_id":        uuid.New().String(),
			"app_id":        app.ID,
			"deployment_id": d.ID,
			"name":          app.Name,
			"git_repo":      app.GitRepo,
			"git_branch":    app.GitBranch,
			"dockerfile":    app.DockerfilePath,
		})
		_ = gp.nc.Publish("hive.build", payload)

		event := &store.UpdateEvent{
			EventType:       "git_rebuild",
			TargetType:      "app",
			TargetID:        app.ID,
			TargetName:      app.Name,
			PreviousVersion: lastDeploy.CommitSHA,
			NewVersion:      remoteHead,
			Status:          "running",
			TriggeredBy:     "auto",
		}
		_ = gp.db.CreateUpdateEvent(ctx, event)

		triggered++
	}

	if polled > 0 {
		gp.log.Debugf("git poller: checked %d repos, triggered %d rebuilds", polled, triggered)
	}
}

func (gp *GitPoller) getRemoteHead(repo, branch string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "ls-remote", "--heads", repo, fmt.Sprintf("refs/heads/%s", branch))
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &bytes.Buffer{}

	if err := cmd.Run(); err != nil {
		return ""
	}

	output := strings.TrimSpace(out.String())
	if output == "" {
		return ""
	}

	fields := strings.Fields(output)
	if len(fields) >= 1 {
		return fields[0]
	}
	return ""
}
