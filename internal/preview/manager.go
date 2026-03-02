package preview

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"
	"go.uber.org/zap"

	"github.com/lholliger/hive/internal/store"
)

type Manager struct {
	store *store.Store
	nc    *nats.Conn
	log   *zap.SugaredLogger
}

func New(s *store.Store, nc *nats.Conn, log *zap.SugaredLogger) *Manager {
	return &Manager{store: s, nc: nc, log: log}
}

func (m *Manager) Create(ctx context.Context, app *store.App, branch string, prNumber int) (*store.PreviewDeployment, error) {
	serviceName := fmt.Sprintf("hive-preview-%s-pr%d", app.Name, prNumber)
	domain := fmt.Sprintf("pr-%d-%s.preview", prNumber, app.Name)

	pd := &store.PreviewDeployment{
		AppID:       app.ID,
		Branch:      branch,
		PRNumber:    &prNumber,
		Domain:      domain,
		Status:      "deploying",
		ServiceName: serviceName,
	}
	if err := m.store.CreatePreviewDeployment(ctx, pd); err != nil {
		return nil, err
	}

	dep := &store.Deployment{AppID: app.ID, Status: "building", CommitSHA: branch}
	_ = m.store.CreateDeployment(ctx, dep)

	job, _ := json.Marshal(map[string]string{
		"action":        "build",
		"app_id":        app.ID,
		"deployment_id": dep.ID,
		"name":          serviceName,
		"git_repo":      app.GitRepo,
		"git_branch":    branch,
		"dockerfile":    app.DockerfilePath,
		"domain":        domain,
		"deploy_type":   "git",
		"preview_id":    pd.ID,
	})
	if m.nc != nil {
		m.nc.Publish("hive.build", job)
	}

	m.log.Infof("preview: created %s for app %s branch %s", serviceName, app.Name, branch)
	return pd, nil
}

func (m *Manager) Destroy(ctx context.Context, previewID string) error {
	pd, err := m.store.GetPreviewDeployment(ctx, previewID)
	if err != nil {
		return err
	}

	if pd.ServiceName != "" && m.nc != nil {
		job, _ := json.Marshal(map[string]string{
			"action": "remove",
			"name":   pd.ServiceName,
			"app_id": pd.AppID,
		})
		m.nc.Publish("hive.deploy", job)
		m.log.Infof("preview: removing service %s", pd.ServiceName)
	}

	return m.store.DeletePreviewDeployment(ctx, previewID)
}

func (m *Manager) FindByPR(ctx context.Context, appID string, prNumber int) (*store.PreviewDeployment, error) {
	previews, err := m.store.ListPreviewDeployments(ctx, appID)
	if err != nil {
		return nil, err
	}
	for _, p := range previews {
		if p.PRNumber != nil && *p.PRNumber == prNumber {
			return &p, nil
		}
	}
	return nil, nil
}
