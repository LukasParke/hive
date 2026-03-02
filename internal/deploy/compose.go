package deploy

import (
	"context"
	"fmt"

	"github.com/lholliger/hive/internal/proxy"
	hiveswarm "github.com/lholliger/hive/internal/swarm"

	"go.uber.org/zap"
)

type ComposeDeployer struct {
	swarm *hiveswarm.Client
	log   *zap.SugaredLogger
}

func NewComposeDeployer(sc *hiveswarm.Client, log *zap.SugaredLogger) *ComposeDeployer {
	return &ComposeDeployer{swarm: sc, log: log}
}

type ComposeService struct {
	Name    string
	Image   string
	Port    int
	Domain  string
	Env     map[string]string
	Volumes map[string]string
}

func (cd *ComposeDeployer) Deploy(ctx context.Context, stackName string, services []ComposeService) error {
	for _, svc := range services {
		labels := map[string]string{
			"hive.managed": "true",
			"hive.stack":   stackName,
		}
		if svc.Domain != "" {
			traefikLabels := proxy.ServiceLabels(svc.Name, svc.Domain, svc.Port)
			labels = proxy.MergeLabels(labels, traefikLabels)
		}

		req := DeployRequest{
			Name:   fmt.Sprintf("%s-%s", stackName, svc.Name),
			Image:  svc.Image,
			Domain: svc.Domain,
			Port:   svc.Port,
			Env:    svc.Env,
			Labels: labels,
		}

		deployer := NewDeployer(cd.swarm, cd.log)
		if err := deployer.Deploy(ctx, req); err != nil {
			return fmt.Errorf("deploy service %s: %w", svc.Name, err)
		}
	}
	return nil
}
