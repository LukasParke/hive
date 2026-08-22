package proxy

import (
	"strings"
	"testing"

	"github.com/moby/moby/api/types/swarm"
)

func TestValidateSecurityRuleTypeRejectsCountryBlock(t *testing.T) {
	err := ValidateSecurityRuleType("country_block")
	if err == nil {
		t.Fatal("expected country_block to be rejected")
	}
	if err.Error() != CountryBlockUnsupportedMsg {
		t.Fatalf("got %q, want %q", err.Error(), CountryBlockUnsupportedMsg)
	}
	if CountryBlockUnsupportedMsg != "country blocking requires an external Traefik plugin and is not supported" {
		t.Fatalf("unexpected message text: %q", CountryBlockUnsupportedMsg)
	}
}

func TestValidateSecurityRuleTypeRejectsBlocklist(t *testing.T) {
	err := ValidateSecurityRuleType("ip_blocklist")
	if err == nil || !strings.Contains(err.Error(), "ip_blocklist") {
		t.Fatalf("expected ip_blocklist rejection, got %v", err)
	}
}

func TestValidateSecurityRuleTypeAcceptsSupportedTypes(t *testing.T) {
	for _, ruleType := range []string{"ip_allowlist", "header_security", "rate_limit"} {
		if err := ValidateSecurityRuleType(ruleType); err != nil {
			t.Fatalf("expected %q to be accepted, got %v", ruleType, err)
		}
	}
}

func TestApplyMiddlewareLabelsEmitsNothingForCountryBlock(t *testing.T) {
	spec := swarm.ServiceSpec{Annotations: swarm.Annotations{Labels: map[string]string{}}}
	applyMiddlewareLabels(&spec, "hive-sec-abcd1234", "country_block", `{"countries":["DE","FR"]}`)
	if len(spec.Labels) != 0 {
		t.Fatalf("expected no labels for country_block, got %v", spec.Labels)
	}
}

func TestApplyMiddlewareLabelsReportsEmission(t *testing.T) {
	spec := swarm.ServiceSpec{Annotations: swarm.Annotations{Labels: map[string]string{}}}
	if applyMiddlewareLabels(&spec, "hive-sec-abcd1234", "ip_allowlist", `{"sourceRange":[]}`) {
		t.Fatal("empty sourceRange must not emit labels")
	}
	if applyMiddlewareLabels(&spec, "hive-sec-abcd1235", "ip_allowlist", `{"sourceRange":["10.0.0.0/8"]}`) != true {
		t.Fatal("non-empty sourceRange must emit labels")
	}
	if len(spec.Labels) != 1 {
		t.Fatalf("expected exactly 1 label, got %v", spec.Labels)
	}
}
