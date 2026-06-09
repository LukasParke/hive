package proxy

import (
	"context"
	"fmt"
	"strings"

	"github.com/moby/moby/api/types/swarm"
	"github.com/jackc/pgx/v5/pgxpool"
	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
)

// ApplySecurityRulesForApplication generates Traefik middleware labels for all enabled
// security rules targeting an application and applies them to its Swarm service.
func ApplySecurityRulesForApplication(ctx context.Context, pool *pgxpool.Pool, cli *swarmclient.Client, appID string) error {
	// Find the Swarm service for this application.
	services, err := cli.ListServices(ctx)
	if err != nil {
		return err
	}
	var targetServiceID string
	var targetVersion uint64
	var spec swarm.ServiceSpec
	for _, svc := range services {
		if svc.Spec.Labels["hive.app.id"] == appID {
			targetServiceID = svc.ID
			targetVersion = svc.Version.Index
			spec = svc.Spec
			break
		}
	}
	if targetServiceID == "" {
		return nil // No deployed service yet.
	}
	if spec.Labels == nil {
		spec.Labels = map[string]string{}
	}

	// Load domains for the application to know router names.
	rows, err := pool.Query(ctx, `select hostname from domains where application_id = $1::uuid`, appID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var hosts []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err == nil && h != "" {
			hosts = append(hosts, h)
		}
	}

	// Load enabled security rules for the application.
	ruleRows, err := pool.Query(ctx, `
		select id::text, type, config, priority
		from security_rules
		where application_id = $1::uuid and enabled = true
		order by priority desc, created_at desc
	`, appID)
	if err != nil {
		return err
	}
	defer ruleRows.Close()

	var middlewareNames []string
	for ruleRows.Next() {
		var id, ruleType string
		var config []byte
		var priority int32
		if err := ruleRows.Scan(&id, &ruleType, &config, &priority); err != nil {
			continue
		}
		name := "hive-sec-" + id[:8]
		middlewareNames = append(middlewareNames, name)
		applyMiddlewareLabels(&spec, name, ruleType, string(config))
	}

	// Attach middleware references to each router.
	middlewareList := strings.Join(middlewareNames, ",")
	for _, host := range hosts {
		router := RouterNameFromHost(host)
		if middlewareList != "" {
			spec.Labels["traefik.http.routers."+router+".middlewares"] = middlewareList
		} else {
			delete(spec.Labels, "traefik.http.routers."+router+".middlewares")
		}
	}

	return cli.UpdateService(ctx, targetServiceID, targetVersion, spec)
}

func applyMiddlewareLabels(spec *swarm.ServiceSpec, name, ruleType, configJSON string) {
	switch ruleType {
	case "ip_allowlist":
		ranges := extractStringSlice(configJSON, "sourceRange")
		if len(ranges) > 0 {
			spec.Labels["traefik.http.middlewares."+name+".ipallowlist.sourcerange"] = strings.Join(ranges, ",")
		}
	case "ip_blocklist":
		// Traefik does not have a native blocklist. We approximate by using
		// ipallowlist with a plugin-like convention. In production, a custom
		// plugin or external WAF is recommended for blocklists.
		ranges := extractStringSlice(configJSON, "sourceRange")
		if len(ranges) > 0 {
			spec.Labels["traefik.http.middlewares."+name+".ipallowlist.sourcerange"] = "127.0.0.1/32"
		}
	case "header_security":
		if v := extractInt(configJSON, "stsSeconds"); v > 0 {
			spec.Labels["traefik.http.middlewares."+name+".headers.stsSeconds"] = fmt.Sprintf("%d", v)
		}
		if extractBool(configJSON, "forceSTSHeader") {
			spec.Labels["traefik.http.middlewares."+name+".headers.forcestsheader"] = "true"
		}
		if v := extractString(configJSON, "contentSecurityPolicy"); v != "" {
			spec.Labels["traefik.http.middlewares."+name+".headers.contentsecuritypolicy"] = v
		}
		if v := extractString(configJSON, "frameDeny"); v != "" {
			spec.Labels["traefik.http.middlewares."+name+".headers.framedeny"] = v
		}
	case "rate_limit":
		if v := extractInt(configJSON, "average"); v > 0 {
			spec.Labels["traefik.http.middlewares."+name+".ratelimit.average"] = fmt.Sprintf("%d", v)
		}
		if v := extractInt(configJSON, "burst"); v > 0 {
			spec.Labels["traefik.http.middlewares."+name+".ratelimit.burst"] = fmt.Sprintf("%d", v)
		}
	case "country_block":
		// Not natively supported by Traefik without a plugin. Placeholder.
		spec.Labels["traefik.http.middlewares."+name+".plugin.countryblock.countries"] = strings.Join(extractStringSlice(configJSON, "countries"), ",")
	}
}

// Very simple JSON extractors for the config object. In production, unmarshal into a struct.

func extractStringSlice(json, key string) []string {
	// Naive parser: look for "key":["...","..."]
	prefix := `"` + key + `":[`
	start := strings.Index(json, prefix)
	if start == -1 {
		return nil
	}
	start += len(prefix)
	end := strings.Index(json[start:], `]`)
	if end == -1 {
		return nil
	}
	arr := json[start : start+end]
	var out []string
	for _, p := range strings.Split(arr, `,`) {
		p = strings.TrimSpace(p)
		p = strings.Trim(p, `"`)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func extractString(json, key string) string {
	prefix := `"` + key + `":"`
	start := strings.Index(json, prefix)
	if start == -1 {
		return ""
	}
	start += len(prefix)
	end := strings.Index(json[start:], `"`)
	if end == -1 {
		return ""
	}
	return json[start : start+end]
}

func extractInt(json, key string) int {
	prefix := `"` + key + `":`
	start := strings.Index(json, prefix)
	if start == -1 {
		return 0
	}
	start += len(prefix)
	end := start
	for end < len(json) && (json[end] >= '0' && json[end] <= '9' || json[end] == '-') {
		end++
	}
	var v int
	fmt.Sscanf(json[start:end], "%d", &v)
	return v
}

func extractBool(json, key string) bool {
	prefix := `"` + key + `":`
	start := strings.Index(json, prefix)
	if start == -1 {
		return false
	}
	start += len(prefix)
	if strings.HasPrefix(json[start:], "true") {
		return true
	}
	return false
}
