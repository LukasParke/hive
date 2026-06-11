package proxy

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
	"github.com/moby/moby/api/types/swarm"
)

type DomainManager struct {
	client *swarmclient.Client
}

func NewDomainManager(client *swarmclient.Client) *DomainManager {
	return &DomainManager{client: client}
}

func (m *DomainManager) ApplyDomain(ctx context.Context, serviceID, routerName, host string, port int, tlsEnabled bool) error {
	services, err := m.client.ListServices(ctx)
	if err != nil {
		return err
	}
	for _, svc := range services {
		if svc.ID != serviceID {
			continue
		}
		if svc.Spec.Labels == nil {
			svc.Spec.Labels = map[string]string{}
		}
		svc.Spec.Labels["traefik.enable"] = "true"
		svc.Spec.Labels["traefik.http.routers."+routerName+".rule"] = fmt.Sprintf("Host(`%s`)", host)
		svc.Spec.Labels["traefik.http.routers."+routerName+".entrypoints"] = "web"
		delete(svc.Spec.Labels, "traefik.http.routers."+routerName+".tls")
		delete(svc.Spec.Labels, "traefik.http.routers."+routerName+".tls.certresolver")
		if tlsEnabled {
			svc.Spec.Labels["traefik.http.routers."+routerName+".entrypoints"] = "websecure"
			svc.Spec.Labels["traefik.http.routers."+routerName+".tls"] = "true"
			svc.Spec.Labels["traefik.http.routers."+routerName+".tls.certresolver"] = "letsencrypt"
		}
		svc.Spec.Labels["traefik.http.services."+routerName+".loadbalancer.server.port"] = strconv.Itoa(port)
		return m.client.UpdateService(ctx, svc.ID, svc.Version.Index, svc.Spec)
	}
	return nil
}

func (m *DomainManager) RemoveDomain(ctx context.Context, serviceID, routerName string) error {
	services, err := m.client.ListServices(ctx)
	if err != nil {
		return err
	}
	for _, svc := range services {
		if svc.ID != serviceID {
			continue
		}
		delete(svc.Spec.Labels, "traefik.http.routers."+routerName+".rule")
		delete(svc.Spec.Labels, "traefik.http.routers."+routerName+".entrypoints")
		delete(svc.Spec.Labels, "traefik.http.routers."+routerName+".tls")
		delete(svc.Spec.Labels, "traefik.http.routers."+routerName+".tls.certresolver")
		delete(svc.Spec.Labels, "traefik.http.services."+routerName+".loadbalancer.server.port")
		return m.client.UpdateService(ctx, svc.ID, svc.Version.Index, svc.Spec)
	}
	return nil
}

func AttachNetworks(spec *swarm.ServiceSpec, networkIDs ...string) {
	spec.TaskTemplate.Networks = nil
	for _, id := range networkIDs {
		spec.TaskTemplate.Networks = append(spec.TaskTemplate.Networks, swarm.NetworkAttachmentConfig{Target: id})
	}
}

func RouterNameFromHost(host string) string {
	name := strings.ToLower(host)
	name = strings.ReplaceAll(name, ".", "-")
	name = strings.ReplaceAll(name, "*", "wildcard")
	return "app-" + name
}
