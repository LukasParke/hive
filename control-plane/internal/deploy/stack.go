package deploy

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/moby/moby/api/types/swarm"
	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
	"gopkg.in/yaml.v3"
)

type ComposeFile struct {
	Services map[string]ComposeService `yaml:"services"`
}

type ComposeService struct {
	Image    string            `yaml:"image"`
	Command  []string          `yaml:"command"`
	Env      map[string]string `yaml:"environment"`
	Labels   map[string]string `yaml:"labels"`
	Ports    []string          `yaml:"ports"`
	Networks []string          `yaml:"networks"`
	Deploy   ComposeDeploy     `yaml:"deploy"`
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
		spec.Networks = networkAttachments(svc.Networks)
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


func flattenEnv(m map[string]string) []string {
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
			Protocol:      swarm.PortConfigProtocolTCP,
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

func networkAttachments(networks []string) []swarm.NetworkAttachmentConfig {
	if len(networks) == 0 {
		return nil
	}
	out := make([]swarm.NetworkAttachmentConfig, 0, len(networks))
	for _, n := range networks {
		name := strings.TrimSpace(n)
		if name == "" {
			continue
		}
		out = append(out, swarm.NetworkAttachmentConfig{Target: name})
	}
	return out
}
