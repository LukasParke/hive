package tunnels

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/moby/moby/api/types/swarm"

	"github.com/luke/hive/control-plane/internal/swarm/spec"
)

const (
	// cloudflaredImage is the connector image deployed on the swarm.
	cloudflaredImage = "cloudflare/cloudflared:latest"
	// tunnelNetwork is the overlay the connector attaches to so it can
	// reach origins (e.g. http://traefik:80) inside the cluster.
	tunnelNetwork = "hive_internal"
	// LabelTunnelName tags the connector service with its tunnel name.
	LabelTunnelName = "hive.tunnel/name"
	// LabelConfigRevision tracks how many times the rendered config has
	// been replaced; bumping it forces a connector restart.
	LabelConfigRevision = "hive.tunnel/config-revision"
	// LabelManaged marks swarm objects owned by the tunnels feature.
	LabelManaged = "hive.managed"
)

// serviceName maps a tunnel name onto its connector service name.
func serviceName(name string) string { return "hive_tunnel_" + name }

// credentialSecretName is the swarm secret carrying the credentials JSON.
func credentialSecretName(name string) string { return "hive-tunnel-" + name + "-cred" }

// configSecretPrefix starts the name of every rendered-config revision.
func configSecretPrefix(name string) string { return "hive-tunnel-" + name + "-config-r" }

// configSecretName is the swarm secret carrying config revision rev.
func configSecretName(name string, rev int) string {
	return fmt.Sprintf("%s%d", configSecretPrefix(name), rev)
}

var (
	namePattern  = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
	labelPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)
)

// ValidateName enforces lowercase alphanumeric names with inner hyphens;
// the name doubles as swarm service and Cloudflare tunnel name.
func ValidateName(name string) error {
	if !namePattern.MatchString(name) {
		return InvalidInput("tunnel name %q must be lowercase alphanumeric with inner hyphens (1-63 chars)", name)
	}
	return nil
}

// validDNSLabel reports whether s is a single lowercase DNS label.
func validDNSLabel(s string) bool { return labelPattern.MatchString(s) }

// validExactHostname validates a plain (non-wildcard) hostname.
func validExactHostname(h string) bool {
	labels := strings.Split(strings.TrimSuffix(h, "."), ".")
	if len(labels) < 2 {
		return false
	}
	for _, l := range labels {
		if !validDNSLabel(l) {
			return false
		}
	}
	return true
}

// ValidHostname accepts exact hostnames and single-level `*.` wildcards.
func ValidHostname(hostname string) bool {
	if rest, ok := strings.CutPrefix(hostname, "*."); ok {
		return validExactHostname(rest)
	}
	return validExactHostname(hostname)
}

// ValidateIngress enforces the frozen API contract: at least one rule,
// every hostname an exact or `*.`-wildcard DNS name, and an http(s)
// origin service. The catch-all 404 rule is implicit and must not be
// supplied by callers.
func ValidateIngress(rules []IngressRule) error {
	if len(rules) == 0 {
		return InvalidInput("at least one ingress rule is required")
	}
	seen := map[string]bool{}
	for i, r := range rules {
		if r.Hostname == "" {
			return InvalidInput("ingress rule %d: hostname is required (the catch-all 404 rule is implicit)", i)
		}
		if !ValidHostname(r.Hostname) {
			return InvalidInput("ingress rule %d: hostname %q must be an exact hostname or a `*.`-prefixed wildcard", i, r.Hostname)
		}
		key := r.Hostname + "|" + r.Path
		if seen[key] {
			return InvalidInput("ingress rule %d: duplicate hostname %q", i, r.Hostname)
		}
		seen[key] = true
		u, err := url.Parse(r.Service)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return InvalidInput("ingress rule %d: service %q must be an http:// or https:// origin URL", i, r.Service)
		}
	}
	return nil
}

// RenderConfig renders the cloudflared config.toml: tunnel identity,
// credentials file path, the ordered [[ingress]] rules and the implicit
// catch-all 404.
func RenderConfig(tunnelID, credentialsPath string, rules []IngressRule) string {
	var b strings.Builder
	fmt.Fprintf(&b, "tunnel: %s\n", tunnelID)
	fmt.Fprintf(&b, "credentials-file: %s\n", credentialsPath)
	for _, r := range rules {
		b.WriteString("\n[[ingress]]\n")
		fmt.Fprintf(&b, "hostname = %q\n", r.Hostname)
		if r.Path != "" {
			fmt.Fprintf(&b, "path = %q\n", r.Path)
		}
		fmt.Fprintf(&b, "service = %q\n", r.Service)
	}
	b.WriteString("\n[[ingress]]\nservice = \"http_status:404\"\n")
	return b.String()
}

// buildServiceSpec assembles the cloudflared connector service spec for
// config revision rev. The rendered config and credentials JSON are
// mounted as swarm secrets under /run/secrets.
func buildServiceSpec(name string, rev int, configSecretID, credSecretID string) (swarm.ServiceSpec, error) {
	configName := configSecretName(name, rev)
	credName := credentialSecretName(name)
	b := spec.NewService(serviceName(name)).
		Image(cloudflaredImage).
		Args([]string{"tunnel", "--config", "/run/secrets/" + configName, "run"}).
		Networks(tunnelNetwork).
		Replicas(1).
		ServiceLabels(map[string]string{
			LabelManaged:        "true",
			LabelTunnelName:     name,
			LabelConfigRevision: strconv.Itoa(rev),
		}).
		Secrets(
			spec.FileRef{ID: configSecretID, Name: configName, Target: configName},
			spec.FileRef{ID: credSecretID, Name: credName, Target: credName},
		)
	s, err := b.Build()
	if err != nil {
		return swarm.ServiceSpec{}, fmt.Errorf("build tunnel service spec: %w", err)
	}
	return s, nil
}

// revisionLabel reads the config revision from service labels.
func revisionLabel(labels map[string]string) int {
	if labels == nil {
		return 0
	}
	rev, err := strconv.Atoi(labels[LabelConfigRevision])
	if err != nil {
		return 0
	}
	return rev
}
