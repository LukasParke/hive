package proxy

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
	"github.com/moby/moby/api/types/swarm"
	"strings"
)

// CountryBlockUnsupportedMsg is the rejection reason for country_block rules:
// Traefik has no built-in geo-blocking and no plugin is shipped with Hive.
const CountryBlockUnsupportedMsg = "country blocking requires an external Traefik plugin and is not supported"

// ValidateSecurityRuleType rejects rule types that cannot be enforced by the
// built-in Traefik middleware set. It must be called on every create/update
// of a security rule so unsupported rules never reach the database.
func ValidateSecurityRuleType(ruleType string) error {
	switch ruleType {
	case "country_block":
		return errors.New(CountryBlockUnsupportedMsg)
	case "ip_blocklist":
		// Traefik has no native blocklist middleware; the previous
		// "approximation" (ipallowlist with 127.0.0.1/32) would have blocked
		// ALL traffic except localhost. Require an external WAF/plugin
		// instead of shipping a footgun.
		return errors.New("ip_blocklist requires an external Traefik plugin or WAF and is not supported by built-in rules")
	}
	return nil
}

// rowQuerier is the slice of *pgxpool.Pool the security applier needs.
type rowQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// ApplySecurityRulesForApplication generates Traefik middleware labels for all enabled
// security rules targeting an application and applies them to its Swarm service.
func ApplySecurityRulesForApplication(ctx context.Context, pool *pgxpool.Pool, cli *swarmclient.Client, appID string) error {
	return applySecurityRules(ctx, pool, cli, appID)
}

func applySecurityRules(ctx context.Context, q rowQuerier, store ServiceStore, appID string) error {
	// Find the Swarm service for this application.
	services, err := store.ListServices(ctx)
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
	rows, err := q.Query(ctx, `select hostname from domains where application_id = $1::uuid`, appID)
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
	ruleRows, err := q.Query(ctx, `
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
		// Unsupported rule types (e.g. legacy country_block rows) emit no
		// middleware labels; referencing a non-existent middleware would
		// break the whole router in Traefik.
		if err := ValidateSecurityRuleType(ruleType); err != nil {
			continue
		}
		name := "hive-sec-" + id[:8]
		if applyMiddlewareLabels(&spec, name, ruleType, string(config)) {
			middlewareNames = append(middlewareNames, name)
		}
	}
	security := make(map[string]bool, len(middlewareNames))
	for _, name := range middlewareNames {
		security[name] = true
	}
	middlewareList := strings.Join(middlewareNames, ",")
	for _, host := range hosts {
		router := RouterNameFromHost(host)
		key := "traefik.http.routers." + router + ".middlewares"
		// Preserve middlewares owned by other features (e.g. the domain
		// strip-prefix middleware): only replace or remove entries this
		// function emitted previously.
		var kept []string
		for _, name := range strings.Split(spec.Labels[key], ",") {
			name = strings.TrimSpace(name)
			if name == "" || security[name] {
				continue
			}
			kept = append(kept, name)
		}
		switch {
		case middlewareList != "":
			spec.Labels[key] = strings.Join(append(kept, middlewareNames...), ",")
		case len(kept) > 0:
			spec.Labels[key] = strings.Join(kept, ",")
		default:
			delete(spec.Labels, key)
		}
	}

	return store.UpdateService(ctx, targetServiceID, targetVersion, spec)
}

// applyMiddlewareLabels writes the Traefik middleware labels for one rule
// and reports whether ANY label was emitted. Rules whose config produces no
// labels must not be referenced from routers: Traefik drops a router whose
// middlewares reference a non-existent middleware.
func applyMiddlewareLabels(spec *swarm.ServiceSpec, name, ruleType, configJSON string) bool {
	emitted := false
	switch ruleType {
	case "ip_allowlist":
		ranges := extractStringSlice(configJSON, "sourceRange")
		if len(ranges) > 0 {
			spec.Labels["traefik.http.middlewares."+name+".ipallowlist.sourcerange"] = strings.Join(ranges, ",")
			emitted = true
		}
	case "header_security":
		if v := extractInt(configJSON, "stsSeconds"); v > 0 {
			spec.Labels["traefik.http.middlewares."+name+".headers.stsSeconds"] = fmt.Sprintf("%d", v)
			emitted = true
		}
		if extractBool(configJSON, "forceSTSHeader") {
			spec.Labels["traefik.http.middlewares."+name+".headers.forcestsheader"] = "true"
			emitted = true
		}
		if v := extractString(configJSON, "contentSecurityPolicy"); v != "" {
			spec.Labels["traefik.http.middlewares."+name+".headers.contentsecuritypolicy"] = v
			emitted = true
		}
		if v := extractString(configJSON, "frameDeny"); v != "" {
			spec.Labels["traefik.http.middlewares."+name+".headers.framedeny"] = v
			emitted = true
		}
	case "rate_limit":
		if v := extractInt(configJSON, "average"); v > 0 {
			spec.Labels["traefik.http.middlewares."+name+".ratelimit.average"] = fmt.Sprintf("%d", v)
			emitted = true
		}
		if v := extractInt(configJSON, "burst"); v > 0 {
			spec.Labels["traefik.http.middlewares."+name+".ratelimit.burst"] = fmt.Sprintf("%d", v)
			emitted = true
		}
	}
	return emitted
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
	_, _ = fmt.Sscanf(json[start:end], "%d", &v)
	return v
}

func extractBool(json, key string) bool {
	prefix := `"` + key + `":`
	start := strings.Index(json, prefix)
	if start == -1 {
		return false
	}
	start += len(prefix)
	return strings.HasPrefix(json[start:], "true")
}
