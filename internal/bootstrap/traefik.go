package bootstrap

import (
	"context"
	"fmt"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
)

const (
	traefikServiceName = "hive-traefik"
	traefikImage       = "traefik:v3.3"
)

func (b *Bootstrapper) ensureTraefik(ctx context.Context) error {
	exists, err := b.swarm.ServiceExists(ctx, traefikServiceName)
	if err != nil {
		return err
	}
	if exists {
		b.log.Info("traefik service already running")
		return nil
	}

	b.log.Info("deploying traefik service")

	args := []string{
		"--providers.swarm=true",
		"--providers.swarm.exposedByDefault=false",
		"--providers.swarm.network=" + hivenetName,
		"--providers.file.directory=/dynamic",
		"--providers.file.watch=true",
		"--entrypoints.web.address=:80",
		"--entrypoints.websecure.address=:443",
		"--entrypoints.web.http.redirections.entryPoint.to=websecure",
		"--entrypoints.web.http.redirections.entryPoint.scheme=https",
		"--certificatesresolvers.letsencrypt.acme.httpchallenge=true",
		"--certificatesresolvers.letsencrypt.acme.httpchallenge.entrypoint=web",
		"--certificatesresolvers.letsencrypt.acme.storage=/certs/acme.json",
		"--api.dashboard=false",
		"--log.level=WARN",
	}

	var env []string
	if b.cfg.CFAPIToken != "" {
		args = append(args,
			"--certificatesresolvers.cloudflare.acme.dnschallenge=true",
			"--certificatesresolvers.cloudflare.acme.dnschallenge.provider=cloudflare",
			"--certificatesresolvers.cloudflare.acme.dnschallenge.resolvers=1.1.1.1:53",
			"--certificatesresolvers.cloudflare.acme.storage=/certs/acme-cf.json",
		)
		env = append(env, "CF_API_EMAIL=", "CF_DNS_API_TOKEN="+b.cfg.CFAPIToken)
	}

	mounts := []mount.Mount{
		{
			Type:   mount.TypeBind,
			Source: "/var/run/docker.sock",
			Target: "/var/run/docker.sock",
		},
		{
			Type:   mount.TypeVolume,
			Source: "hive-traefik-certs",
			Target: "/certs",
		},
		{
			Type:   mount.TypeVolume,
			Source: "hive-traefik-dynamic",
			Target: "/dynamic",
		},
	}

	spec := swarm.ServiceSpec{
		Annotations: swarm.Annotations{
			Name: traefikServiceName,
			Labels: map[string]string{
				"hive.managed":   "true",
				"hive.component": "traefik",
			},
		},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{
				Image:  traefikImage,
				Args:   args,
				Env:    env,
				Mounts: mounts,
			},
			Networks: []swarm.NetworkAttachmentConfig{
				{Target: hivenetName},
			},
		},
		Mode: swarm.ServiceMode{
			Global: &swarm.GlobalService{},
		},
		EndpointSpec: &swarm.EndpointSpec{
			Ports: []swarm.PortConfig{
				{
					Protocol:      swarm.PortConfigProtocolTCP,
					TargetPort:    80,
					PublishedPort: 80,
					PublishMode:   swarm.PortConfigPublishModeIngress,
				},
				{
					Protocol:      swarm.PortConfigProtocolTCP,
					TargetPort:    443,
					PublishedPort: 443,
					PublishMode:   swarm.PortConfigPublishModeIngress,
				},
			},
		},
	}

	return b.swarm.CreateService(ctx, spec)
}

func (b *Bootstrapper) traefikLabels(serviceName, domain string, port int) map[string]string {
	return map[string]string{
		"traefik.enable": "true",
		fmt.Sprintf("traefik.http.routers.%s.rule", serviceName):              fmt.Sprintf("Host(`%s`)", domain),
		fmt.Sprintf("traefik.http.routers.%s.tls.certresolver", serviceName):  "letsencrypt",
		fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", serviceName): fmt.Sprintf("%d", port),
	}
}
