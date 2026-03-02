package proxy

import "fmt"

func ServiceLabels(serviceName, domain string, port int) map[string]string {
	labels := map[string]string{
		"traefik.enable": "true",
		fmt.Sprintf("traefik.http.routers.%s.rule", serviceName):              fmt.Sprintf("Host(`%s`)", domain),
		fmt.Sprintf("traefik.http.routers.%s.entrypoints", serviceName):       "websecure",
		fmt.Sprintf("traefik.http.routers.%s.tls.certresolver", serviceName):  "letsencrypt",
		fmt.Sprintf("traefik.http.services.%s.loadbalancer.server.port", serviceName): fmt.Sprintf("%d", port),
	}
	return labels
}

func MergeLabels(base, extra map[string]string) map[string]string {
	merged := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range extra {
		merged[k] = v
	}
	return merged
}
