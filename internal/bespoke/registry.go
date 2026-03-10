package bespoke

import "strings"

type AppClass struct {
	Slug             string            `json:"slug"`
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	TemplateName     string            `json:"template_name"`
	Category         string            `json:"category"`
	RecommendedPorts []string          `json:"recommended_ports"`
	RecommendedEnv   map[string]string `json:"recommended_env"`
	Notes            []string          `json:"notes"`
}

var classes = []AppClass{
	{
		Slug:         "plex",
		Name:         "Plex Media Server",
		Description:  "Optimized media server deployment profile with persistent library mounts and LAN-first access.",
		TemplateName: "plex",
		Category:     "media",
		RecommendedPorts: []string{
			"32400:32400",
		},
		RecommendedEnv: map[string]string{
			"TZ":           "UTC",
			"PLEX_CLAIM":   "",
			"PUID":         "1000",
			"PGID":         "1000",
			"ADVERTISE_IP": "",
		},
		Notes: []string{
			"Mount media and transcode paths to persistent storage.",
			"Use host networking only when discovery issues require it.",
			"Prefer domain + TLS ingress for remote access.",
		},
	},
	{
		Slug:         "home-assistant",
		Name:         "Home Assistant",
		Description:  "Home automation profile tuned for persistent config and device integration.",
		TemplateName: "home-assistant",
		Category:     "automation",
		RecommendedPorts: []string{
			"8123:8123",
		},
		RecommendedEnv: map[string]string{
			"TZ": "UTC",
		},
		Notes: []string{
			"Persist /config on a durable volume.",
			"If mDNS is required, colocate supporting services on same node/network.",
			"Use regular backups before upgrading core/home-assistant image.",
		},
	},
	{
		Slug:         "arr-suite",
		Name:         "Arr Suite",
		Description:  "Coordinated media management stack guidance for Sonarr, Radarr, Prowlarr, and related services.",
		TemplateName: "sonarr",
		Category:     "media",
		RecommendedPorts: []string{
			"8989:8989",
			"7878:7878",
			"9696:9696",
		},
		RecommendedEnv: map[string]string{
			"TZ":   "UTC",
			"PUID": "1000",
			"PGID": "1000",
		},
		Notes: []string{
			"Use shared media/download volumes across all Arr services.",
			"Deploy indexer first (Prowlarr) before Sonarr/Radarr wiring.",
			"Prefer per-service domains behind Traefik labels.",
		},
	},
}

func List() []AppClass {
	out := make([]AppClass, len(classes))
	copy(out, classes)
	return out
}

func Get(slug string) (AppClass, bool) {
	for _, c := range classes {
		if c.Slug == slug {
			return c, true
		}
	}
	return AppClass{}, false
}

func MatchTemplate(name string) (AppClass, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, c := range classes {
		if strings.ToLower(c.TemplateName) == n {
			return c, true
		}
	}
	return AppClass{}, false
}
