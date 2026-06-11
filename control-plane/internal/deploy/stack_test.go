package deploy

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestComposeEnvironmentAcceptsMapAndList(t *testing.T) {
	raw := []byte(`
services:
  api:
    image: nginx:latest
    environment:
      FOO: bar
      EMPTY: ""
  worker:
    image: busybox
    environment:
      - QUEUE=default
      - FLAG
`)
	var cfg ComposeFile
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if cfg.Services["api"].Env["FOO"] != "bar" || cfg.Services["worker"].Env["QUEUE"] != "default" || cfg.Services["worker"].Env["FLAG"] != "" {
		t.Fatalf("unexpected env parse: %#v %#v", cfg.Services["api"].Env, cfg.Services["worker"].Env)
	}
}

func TestComposeStackNetworks(t *testing.T) {
	raw := []byte(`
services:
  api:
    image: nginx:latest
    networks: [web]
  worker:
    image: busybox
networks:
  web: {}
`)
	var cfg ComposeFile
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	networks := cfg.stackNetworks("mystack")
	apiAttachments := networkAttachments("mystack", cfg.Services["api"].Networks, networks)
	workerAttachments := networkAttachments("mystack", cfg.Services["worker"].Networks, networks)
	if got := apiAttachments[0].Target; got != "mystack_web" {
		t.Fatalf("api network target = %q", got)
	}
	if got := workerAttachments[0].Target; got != "mystack_default" {
		t.Fatalf("worker default network target = %q", got)
	}
}

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
