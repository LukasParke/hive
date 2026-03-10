package proxy

import "fmt"

func ServiceLabels(serviceName, domain string, port int, certResolver ...string) map[string]string {
	resolver := "letsencrypt"
	if len(certResolver) > 0 && certResolver[0] != "" {
		resolver = certResolver[0]
	}
	labels := map[string]string{
		"traefik.enable": "true",
		fmt.Sprintf("traefik.http.routers.%s.rule", serviceName):              fmt.Sprintf("Host(`%s`)", domain),
		fmt.Sprintf("traefik.http.routers.%s.entrypoints", serviceName):       "websecure",
		fmt.Sprintf("traefik.http.routers.%s.tls.certresolver", serviceName):  resolver,
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
