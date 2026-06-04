package deploy

import (
	"context"
	"strconv"

	dockerswarm "github.com/docker/docker/api/types/swarm"
	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
)

type EnvVar struct {
	Key        string
	Value      string // empty for secrets
	IsSecret   bool
	SecretName string // Docker secret name, empty for non-secrets
}

type ApplicationSpec struct {
	AppID         string
	ServiceName   string
	Image         string
	ContainerPort int
	EnvVars       []EnvVar
}

func DeployApplication(ctx context.Context, cli *swarmclient.Client, spec ApplicationSpec) error {
	services, err := cli.ListServices(ctx)
	if err != nil {
		return err
	}

	var existing *dockerswarm.Service
	for i := range services {
		svc := services[i]
		if svc.Spec.Labels["hive.app.id"] == spec.AppID {
			existing = &svc
			break
		}
	}

	var envStrings []string
	var secretRefs []*dockerswarm.SecretReference
	for _, ev := range spec.EnvVars {
		if ev.IsSecret {
			secretRefs = append(secretRefs, &dockerswarm.SecretReference{
				File: &dockerswarm.SecretReferenceFileTarget{
					Name: ev.Key,
					UID:  "0",
					GID:  "0",
					Mode: 0o400,
				},
				SecretName: ev.SecretName,
			})
		} else {
			envStrings = append(envStrings, ev.Key+"="+ev.Value)
		}
	}

	serviceSpec := dockerswarm.ServiceSpec{
		Annotations: dockerswarm.Annotations{
			Name: spec.ServiceName,
			Labels: map[string]string{
				"hive.app.id":        spec.AppID,
				"hive.app.port":      strconv.Itoa(spec.ContainerPort),
			},
		},
		TaskTemplate: dockerswarm.TaskSpec{
			ContainerSpec: &dockerswarm.ContainerSpec{
				Image: spec.Image,
				Env:   envStrings,
				Secrets: secretRefs,
				Labels: map[string]string{
					"hive.app.id":   spec.AppID,
					"hive.app.port": strconv.Itoa(spec.ContainerPort),
				},
			},
			RestartPolicy: &dockerswarm.RestartPolicy{
				Condition: dockerswarm.RestartPolicyConditionAny,
				Delay:     nil,
				MaxAttempts: nil,
			},
		},
		Mode: dockerswarm.ServiceMode{
			Replicated: &dockerswarm.ReplicatedService{Replicas: ptrUint64(1)},
		},
		UpdateConfig: &dockerswarm.UpdateConfig{
			Order:         "start-first",
			FailureAction: "rollback",
		},
		EndpointSpec: &dockerswarm.EndpointSpec{},
	}

	if existing == nil {
		_, err := cli.CreateService(ctx, serviceSpec)
		return err
	}

	serviceSpec.Annotations.Name = existing.Spec.Name
	return cli.UpdateService(ctx, existing.ID, existing.Version.Index, serviceSpec)
}

func ptrUint64(v uint64) *uint64 { return &v }
