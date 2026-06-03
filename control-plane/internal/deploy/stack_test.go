package deploy

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestComposeDeployReplicasParse(t *testing.T) {
	raw := []byte(`
services:
  api:
    image: nginx:latest
    deploy:
      replicas: 3
`)
	var cfg ComposeFile
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	svc := cfg.Services["api"]
	if svc.Deploy.Replicas == nil || *svc.Deploy.Replicas != 3 {
		t.Fatalf("expected replicas=3 got %#v", svc.Deploy.Replicas)
	}
}
