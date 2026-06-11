package deploy

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/swarm"
	"gopkg.in/yaml.v3"
)

type ComposeFile struct {
	Services map[string]ComposeService `yaml:"services"`
	Networks map[string]ComposeNetwork `yaml:"networks"`
}

type ComposeService struct {
	Image    string             `yaml:"image"`
	Command  []string           `yaml:"command"`
	Env      ComposeEnvironment `yaml:"environment"`
	Labels   map[string]string  `yaml:"labels"`
	Ports    []string           `yaml:"ports"`
	Networks []string           `yaml:"networks"`
	Deploy   ComposeDeploy      `yaml:"deploy"`
}

type ComposeEnvironment map[string]string

type ComposeNetwork struct {
	Name     string `yaml:"name"`
	External bool   `yaml:"external"`
}

type ComposeDeploy struct {
	Replicas      *uint64 `yaml:"replicas"`
	Mode          string  `yaml:"mode"`
	RestartPolicy struct {
		Condition string `yaml:"condition"`
	} `yaml:"restart_policy"`
}

func DeployStack(ctx context.Context, cli *swarmclient.Client, stackName, composePath string) error {
	raw, err := os.ReadFile(composePath)
	if err != nil {
		return err
	}

	var cfg ComposeFile
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return err
	}

	existingServices, err := cli.ListServices(ctx)
	if err != nil {
		return err
	}
	byName := map[string]swarm.Service{}
	for _, existing := range existingServices {
		byName[existing.Spec.Name] = existing
	}
	networks := cfg.stackNetworks(stackName)
	if err := ensureStackNetworks(ctx, cli, networks); err != nil {
		return err
	}

	for svcName, svc := range cfg.Services {
		name := fmt.Sprintf("%s_%s", stackName, svcName)
		spec := swarm.ServiceSpec{
			Annotations: swarm.Annotations{
				Name: name,
				Labels: map[string]string{
					"com.docker.stack.namespace": stackName,
				},
			},
			TaskTemplate: swarm.TaskSpec{
				ContainerSpec: &swarm.ContainerSpec{
					Image:   svc.Image,
					Env:     flattenEnv(svc.Env),
					Labels:  svc.Labels,
					Command: svc.Command,
				},
				RestartPolicy: &swarm.RestartPolicy{
					Condition: swarm.RestartPolicyConditionAny,
				},
			},
			UpdateConfig: &swarm.UpdateConfig{
				FailureAction: "rollback",
				Order:         "start-first",
			},
			Mode: swarm.ServiceMode{
				Replicated: &swarm.ReplicatedService{Replicas: ptrUint64(1)},
			},
		}
		spec.EndpointSpec = endpointSpecFromPorts(svc.Ports)
		spec.TaskTemplate.Networks = networkAttachments(stackName, svc.Networks, networks)
		if svc.Deploy.Replicas != nil {
			spec.Mode.Replicated.Replicas = svc.Deploy.Replicas
		}
		if strings.EqualFold(svc.Deploy.Mode, "global") {
			spec.Mode = swarm.ServiceMode{Global: &swarm.GlobalService{}}
		}
		if svc.Deploy.RestartPolicy.Condition == "none" {
			spec.TaskTemplate.RestartPolicy.Condition = swarm.RestartPolicyConditionNone
		}
		if existing, ok := byName[name]; ok {
			if err := cli.UpdateService(ctx, existing.ID, existing.Version.Index, spec); err != nil {
				return err
			}
			continue
		}
		if _, err := cli.CreateService(ctx, spec); err != nil {
			return err
		}
	}

	desired := map[string]struct{}{}
	for svcName := range cfg.Services {
		desired[fmt.Sprintf("%s_%s", stackName, svcName)] = struct{}{}
	}
	for _, existing := range existingServices {
		if existing.Spec.Labels["com.docker.stack.namespace"] != stackName {
			continue
		}
		if _, ok := desired[existing.Spec.Name]; ok {
			continue
		}
		if err := cli.RemoveService(ctx, existing.ID); err != nil {
			return err
		}
	}

	return nil
}

func (e *ComposeEnvironment) UnmarshalYAML(value *yaml.Node) error {
	out := map[string]string{}
	switch value.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(value.Content); i += 2 {
			key := strings.TrimSpace(value.Content[i].Value)
			if key == "" {
				continue
			}
			out[key] = value.Content[i+1].Value
		}
	case yaml.SequenceNode:
		for _, item := range value.Content {
			entry := strings.TrimSpace(item.Value)
			if entry == "" {
				continue
			}
			key, val, hasValue := strings.Cut(entry, "=")
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			if !hasValue {
				val = ""
			}
			out[key] = val
		}
	case 0:
		// Environment omitted.
	default:
		return fmt.Errorf("environment must be a mapping or KEY=VALUE list")
	}
	*e = out
	return nil
}

func (cfg ComposeFile) stackNetworks(stackName string) map[string]string {
	if len(cfg.Networks) == 0 {
		return map[string]string{"default": stackName + "_default"}
	}
	out := make(map[string]string, len(cfg.Networks))
	for name, cfg := range cfg.Networks {
		if strings.TrimSpace(name) == "" {
			continue
		}
		if cfg.External {
			if strings.TrimSpace(cfg.Name) != "" {
				out[name] = strings.TrimSpace(cfg.Name)
			} else {
				out[name] = name
			}
			continue
		}
		if strings.TrimSpace(cfg.Name) != "" {
			out[name] = strings.TrimSpace(cfg.Name)
		} else {
			out[name] = stackName + "_" + name
		}
	}
	if _, ok := out["default"]; !ok {
		out["default"] = stackName + "_default"
	}
	return out
}

func ensureStackNetworks(ctx context.Context, cli *swarmclient.Client, networks map[string]string) error {
	existing, err := cli.ListNetworks(ctx)
	if err != nil {
		return err
	}
	byName := map[string]struct{}{}
	for _, n := range existing {
		byName[n.Name] = struct{}{}
	}
	for _, name := range networks {
		if strings.TrimSpace(name) == "" {
			continue
		}
		if _, ok := byName[name]; ok {
			continue
		}
		if _, err := cli.CreateNetwork(ctx, name); err != nil {
			return err
		}
	}
	return nil
}

func flattenEnv(m ComposeEnvironment) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

func endpointSpecFromPorts(ports []string) *swarm.EndpointSpec {
	if len(ports) == 0 {
		return nil
	}
	out := make([]swarm.PortConfig, 0, len(ports))
	for _, p := range ports {
		parts := strings.Split(p, ":")
		if len(parts) != 2 {
			continue
		}
		published, err1 := strconv.ParseUint(strings.TrimSpace(parts[0]), 10, 32)
		target, err2 := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 32)
		if err1 != nil || err2 != nil {
			continue
		}
		out = append(out, swarm.PortConfig{
			Protocol:      network.TCP,
			PublishMode:   swarm.PortConfigPublishModeIngress,
			PublishedPort: uint32(published),
			TargetPort:    uint32(target),
		})
	}
	if len(out) == 0 {
		return nil
	}
	return &swarm.EndpointSpec{Ports: out}
}

func networkAttachments(stackName string, requested []string, networks map[string]string) []swarm.NetworkAttachmentConfig {
	if len(requested) == 0 {
		requested = []string{"default"}
	}
	out := make([]swarm.NetworkAttachmentConfig, 0, len(requested))
	for _, n := range requested {
		name := strings.TrimSpace(n)
		if name == "" {
			continue
		}
		target := networks[name]
		if target == "" {
			target = stackName + "_" + name
		}
		out = append(out, swarm.NetworkAttachmentConfig{Target: target})
	}
	return out
}
