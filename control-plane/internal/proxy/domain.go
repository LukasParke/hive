package proxy

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
	"github.com/moby/moby/api/types/swarm"
)

// Supported domain route types.
const (
	RouteTypeHost     = "host"
	RouteTypeWildcard = "wildcard"
	RouteTypePath     = "path"
)

// Route describes how a single domain maps onto a Traefik router.
type Route struct {
	Host        string
	RouteType   string // host | wildcard | path; "" is treated as host
	PathPrefix  string // only meaningful for the path route type
	StripPrefix bool   // only meaningful for the path route type
	Priority    int    // >0 overrides Traefik's automatic router priority
	TLSEnabled  bool   // routes the router through the websecure entrypoint
}

// ServiceStore is the slice of the swarm client the DomainManager needs.
// *swarmclient.Client satisfies it; tests inject fakes.
type ServiceStore interface {
	ListServices(ctx context.Context) ([]swarm.Service, error)
	UpdateService(ctx context.Context, id string, version uint64, spec swarm.ServiceSpec) error
}

// DomainManager keeps proxy routing in sync with configured domains.
type DomainManager struct {
	client ServiceStore
}

// NewDomainManager returns a DomainManager using the given swarm store.
func NewDomainManager(client ServiceStore) *DomainManager {
	return &DomainManager{client: client}
}

// RouterRule builds the Traefik rule expression for a route.
//
//   - host:     Host(`example.com`)
//   - wildcard: HostRegexp(`{sub:[^.]+}\.example\.com`) for `*.example.com`
//     (matches any single-label subdomain; dots are escaped)
//   - path:     Host(`h`) && PathPrefix(`/pp`); wildcard+path combines
//     HostRegexp with PathPrefix.
func RouterRule(host, routeType, pathPrefix string) string {
	// Wildcard hostnames (a leading "*.") match any single-label subdomain
	// via HostRegexp with escaped dots; this applies to plain wildcard
	// routes and to wildcard+path combinations alike.
	hostExpr := fmt.Sprintf("Host(`%s`)", host)
	if strings.HasPrefix(host, "*.") {
		suffix := regexp.QuoteMeta(host[len("*."):])
		hostExpr = fmt.Sprintf("HostRegexp(`{sub:[^.]+}\\.%s`)", suffix)
	}
	if routeType == RouteTypePath {
		prefix := pathPrefix
		if !strings.HasPrefix(prefix, "/") {
			prefix = "/" + prefix
		}
		return fmt.Sprintf("%s && PathPrefix(`%s`)", hostExpr, prefix)
	}
	return hostExpr
}

// stripMiddlewareName is the per-router Traefik middleware name used to strip
// matched path prefixes.
func stripMiddlewareName(router string) string {
	return router + "-strip"
}

// composeMiddlewares rewrites the router's middlewares list: any previous
// reference to this router's strip middleware is removed, security
// middlewares already on the list are preserved, and the strip middleware is
// appended when the route enables it (after the security entries). When the
// resulting list is empty the label is dropped entirely.
func composeMiddlewares(labels map[string]string, router string, r Route) {
	key := "traefik.http.routers." + router + ".middlewares"
	strip := stripMiddlewareName(router)
	var kept []string
	for _, name := range strings.Split(labels[key], ",") {
		name = strings.TrimSpace(name)
		if name == "" || name == strip {
			continue
		}
		kept = append(kept, name)
	}
	middlewareKey := "traefik.http.middlewares." + strip + ".stripprefix.prefixes"
	if r.StripPrefix && r.RouteType == RouteTypePath {
		prefix := r.PathPrefix
		if !strings.HasPrefix(prefix, "/") {
			prefix = "/" + prefix
		}
		labels[middlewareKey] = prefix
		kept = append(kept, strip)
	} else {
		delete(labels, middlewareKey)
	}
	if len(kept) == 0 {
		delete(labels, key)
		return
	}
	labels[key] = strings.Join(kept, ",")
}

// ApplyDomain attaches the domain's service to the proxy network.
func (m *DomainManager) ApplyDomain(ctx context.Context, serviceID, routerName string, r Route, port int) error {
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
		labels := svc.Spec.Labels
		router := "traefik.http.routers." + routerName + "."
		labels["traefik.enable"] = "true"
		labels[router+"rule"] = RouterRule(r.Host, r.RouteType, r.PathPrefix)
		labels[router+"entrypoints"] = "web"
		delete(labels, router+"tls")
		delete(labels, router+"tls.certresolver")
		if r.TLSEnabled {
			labels[router+"entrypoints"] = "websecure"
			labels[router+"tls"] = "true"
			labels[router+"tls.certresolver"] = "letsencrypt"
		}
		if r.Priority > 0 {
			labels[router+"priority"] = strconv.Itoa(r.Priority)
		} else {
			delete(labels, router+"priority")
		}
		composeMiddlewares(labels, routerName, r)
		labels["traefik.http.services."+routerName+".loadbalancer.server.port"] = strconv.Itoa(port)
		return m.client.UpdateService(ctx, svc.ID, svc.Version.Index, svc.Spec)
	}
	return nil
}

// RemoveDomain detaches the domain's service from the proxy network.
func (m *DomainManager) RemoveDomain(ctx context.Context, serviceID, routerName string) error {
	services, err := m.client.ListServices(ctx)
	if err != nil {
		return err
	}
	for _, svc := range services {
		if svc.ID != serviceID {
			continue
		}
		labels := svc.Spec.Labels
		router := "traefik.http.routers." + routerName + "."
		delete(labels, router+"rule")
		delete(labels, router+"entrypoints")
		delete(labels, router+"tls")
		delete(labels, router+"tls.certresolver")
		delete(labels, router+"priority")
		delete(labels, "traefik.http.middlewares."+stripMiddlewareName(routerName)+".stripprefix.prefixes")
		composeMiddlewares(labels, routerName, Route{})
		delete(labels, "traefik.http.services."+routerName+".loadbalancer.server.port")
		return m.client.UpdateService(ctx, svc.ID, svc.Version.Index, svc.Spec)
	}
	return nil
}

// AttachNetworks attaches a service spec to the given networks.
func AttachNetworks(spec *swarm.ServiceSpec, networkIDs ...string) {
	spec.TaskTemplate.Networks = nil
	for _, id := range networkIDs {
		spec.TaskTemplate.Networks = append(spec.TaskTemplate.Networks, swarm.NetworkAttachmentConfig{Target: id})
	}
}

// RouterNameFromHost derives the swarm router name for a host. A leading "*."
// is stripped so wildcard domains produce valid router names.
func RouterNameFromHost(host string) string {
	name := strings.ToLower(strings.TrimPrefix(host, "*."))
	name = strings.ReplaceAll(name, ".", "-")
	name = strings.ReplaceAll(name, "*", "wildcard")
	return "app-" + name
}

// Compile-time check that the concrete swarm client satisfies ServiceStore.
var _ ServiceStore = (*swarmclient.Client)(nil)
