package deploy

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/swarm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go.uber.org/zap"
)

type mockSwarm struct {
	services       map[string]*swarm.Service
	createdSpecs   []swarm.ServiceSpec
	updatedSpecs   []swarm.ServiceSpec
	removedIDs     []string
}

func newMockSwarm() *mockSwarm {
	return &mockSwarm{services: make(map[string]*swarm.Service)}
}

func (m *mockSwarm) ServiceExists(_ context.Context, name string) (bool, error) {
	_, ok := m.services[name]
	return ok, nil
}

func (m *mockSwarm) CreateService(_ context.Context, spec swarm.ServiceSpec) error {
	m.createdSpecs = append(m.createdSpecs, spec)
	m.services[spec.Name] = &swarm.Service{
		ID:   "svc-" + spec.Name,
		Spec: spec,
	}
	return nil
}

func (m *mockSwarm) UpdateService(_ context.Context, serviceID string, _ swarm.Version, spec swarm.ServiceSpec) error {
	m.updatedSpecs = append(m.updatedSpecs, spec)
	return nil
}

func (m *mockSwarm) RemoveService(_ context.Context, serviceID string) error {
	m.removedIDs = append(m.removedIDs, serviceID)
	return nil
}

func (m *mockSwarm) GetService(_ context.Context, name string) (*swarm.Service, error) {
	svc, ok := m.services[name]
	if !ok {
		return nil, nil
	}
	return svc, nil
}

func (m *mockSwarm) ListServices(_ context.Context) ([]swarm.Service, error) {
	var result []swarm.Service
	for _, s := range m.services {
		result = append(result, *s)
	}
	return result, nil
}

func (m *mockSwarm) ServiceLogs(_ context.Context, _ string, _ string, _ bool) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (m *mockSwarm) ScaleService(_ context.Context, _ string, _ uint64) error {
	return nil
}

func (m *mockSwarm) RollbackService(_ context.Context, _ string) error {
	return nil
}

func testLogger() *zap.SugaredLogger {
	l, _ := zap.NewNop().Sugar(), error(nil)
	return l
}

func TestDeployNewService(t *testing.T) {
	ms := newMockSwarm()
	d := NewDeployer(ms, testLogger())

	err := d.Deploy(context.Background(), DeployRequest{
		Name:     "myapp",
		Image:    "nginx:latest",
		Port:     8080,
		Replicas: 2,
	})
	require.NoError(t, err)
	require.Len(t, ms.createdSpecs, 1)

	spec := ms.createdSpecs[0]
	assert.Equal(t, "hive-app-myapp", spec.Name)
	assert.Equal(t, "nginx:latest", spec.TaskTemplate.ContainerSpec.Image)
	assert.Equal(t, "true", spec.Labels["hive.managed"])
	replicas := spec.Mode.Replicated.Replicas
	assert.Equal(t, uint64(2), *replicas)
}

func TestDeployExistingService(t *testing.T) {
	ms := newMockSwarm()
	ms.services["hive-app-myapp"] = &swarm.Service{
		ID:   "svc-existing",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "hive-app-myapp"}},
	}

	d := NewDeployer(ms, testLogger())
	err := d.Deploy(context.Background(), DeployRequest{
		Name:  "myapp",
		Image: "nginx:2.0",
	})
	require.NoError(t, err)
	assert.Len(t, ms.createdSpecs, 0, "should not create")
	assert.Len(t, ms.updatedSpecs, 1, "should update")
	assert.Equal(t, "nginx:2.0", ms.updatedSpecs[0].TaskTemplate.ContainerSpec.Image)
}

func TestDeployWithDomain(t *testing.T) {
	ms := newMockSwarm()
	d := NewDeployer(ms, testLogger())

	err := d.Deploy(context.Background(), DeployRequest{
		Name:   "webapp",
		Image:  "app:latest",
		Domain: "app.example.com",
		Port:   3000,
	})
	require.NoError(t, err)
	require.Len(t, ms.createdSpecs, 1)

	labels := ms.createdSpecs[0].Labels
	assert.Equal(t, "true", labels["traefik.enable"])
	assert.Contains(t, labels["traefik.http.routers.hive-app-webapp.rule"], "app.example.com")
	assert.Equal(t, "3000", labels["traefik.http.services.hive-app-webapp.loadbalancer.server.port"])
}

func TestDeployWithSecrets(t *testing.T) {
	ms := newMockSwarm()
	d := NewDeployer(ms, testLogger())

	err := d.Deploy(context.Background(), DeployRequest{
		Name:  "secure-app",
		Image: "app:latest",
		Secrets: []SecretMount{
			{
				DockerSecretID: "secret-id-1",
				SecretName:     "db-password",
				Target:         "db_pass",
				UID:            "1000",
				GID:            "1000",
				Mode:           os.FileMode(0400),
			},
			{
				DockerSecretID: "secret-id-2",
				SecretName:     "api-key",
			},
		},
	})
	require.NoError(t, err)
	require.Len(t, ms.createdSpecs, 1)

	secrets := ms.createdSpecs[0].TaskTemplate.ContainerSpec.Secrets
	require.Len(t, secrets, 2)

	assert.Equal(t, "secret-id-1", secrets[0].SecretID)
	assert.Equal(t, "db_pass", secrets[0].File.Name)
	assert.Equal(t, "1000", secrets[0].File.UID)
	assert.Equal(t, os.FileMode(0400), secrets[0].File.Mode)

	assert.Equal(t, "api-key", secrets[1].File.Name, "defaults to SecretName when Target empty")
	assert.Equal(t, os.FileMode(0444), secrets[1].File.Mode, "defaults to 0444")
}

func TestDeployWithVolumes(t *testing.T) {
	ms := newMockSwarm()
	d := NewDeployer(ms, testLogger())

	err := d.Deploy(context.Background(), DeployRequest{
		Name:  "data-app",
		Image: "app:latest",
		Volumes: []VolumeMount{
			{VolumeName: "data-vol", ContainerPath: "/data", ReadOnly: false},
			{VolumeName: "config-vol", ContainerPath: "/config", ReadOnly: true},
		},
	})
	require.NoError(t, err)
	require.Len(t, ms.createdSpecs, 1)

	mounts := ms.createdSpecs[0].TaskTemplate.ContainerSpec.Mounts
	require.Len(t, mounts, 2)

	assert.Equal(t, mount.TypeVolume, mounts[0].Type)
	assert.Equal(t, "data-vol", mounts[0].Source)
	assert.Equal(t, "/data", mounts[0].Target)
	assert.False(t, mounts[0].ReadOnly)

	assert.Equal(t, "config-vol", mounts[1].Source)
	assert.Equal(t, "/config", mounts[1].Target)
	assert.True(t, mounts[1].ReadOnly)
}

func TestDeployDefaultValues(t *testing.T) {
	ms := newMockSwarm()
	d := NewDeployer(ms, testLogger())

	err := d.Deploy(context.Background(), DeployRequest{
		Name:  "defaults",
		Image: "app:latest",
	})
	require.NoError(t, err)
	require.Len(t, ms.createdSpecs, 1)

	spec := ms.createdSpecs[0]
	replicas := spec.Mode.Replicated.Replicas
	assert.Equal(t, uint64(1), *replicas, "default 1 replica")
	assert.Equal(t, "start-first", spec.UpdateConfig.Order)
	assert.NotNil(t, spec.RollbackConfig)
}

func TestDeployManagerPlacement(t *testing.T) {
	ms := newMockSwarm()
	d := NewDeployer(ms, testLogger())

	err := d.Deploy(context.Background(), DeployRequest{
		Name:      "infra",
		Image:     "postgres:16",
		Placement: PlacementManager,
	})
	require.NoError(t, err)
	require.Len(t, ms.createdSpecs, 1)

	constraints := ms.createdSpecs[0].TaskTemplate.Placement.Constraints
	assert.Contains(t, constraints, "node.role == manager")
}

func TestDeployCustomConstraints(t *testing.T) {
	ms := newMockSwarm()
	d := NewDeployer(ms, testLogger())

	err := d.Deploy(context.Background(), DeployRequest{
		Name:        "constrained",
		Image:       "app:latest",
		Constraints: []string{"node.labels.gpu == true"},
	})
	require.NoError(t, err)
	require.Len(t, ms.createdSpecs, 1)

	constraints := ms.createdSpecs[0].TaskTemplate.Placement.Constraints
	assert.Contains(t, constraints, "node.labels.gpu == true")
}

func TestRemoveService(t *testing.T) {
	ms := newMockSwarm()
	ms.services["hive-app-myapp"] = &swarm.Service{
		ID:   "svc-123",
		Spec: swarm.ServiceSpec{Annotations: swarm.Annotations{Name: "hive-app-myapp"}},
	}

	d := NewDeployer(ms, testLogger())
	err := d.Remove(context.Background(), "myapp")
	require.NoError(t, err)
	assert.Contains(t, ms.removedIDs, "svc-123")
}

func TestRemoveServiceNotFound(t *testing.T) {
	ms := newMockSwarm()
	d := NewDeployer(ms, testLogger())

	err := d.Remove(context.Background(), "nonexistent")
	require.NoError(t, err)
	assert.Empty(t, ms.removedIDs)
}

func TestDeployWithEnvVars(t *testing.T) {
	ms := newMockSwarm()
	d := NewDeployer(ms, testLogger())

	err := d.Deploy(context.Background(), DeployRequest{
		Name:  "env-app",
		Image: "app:latest",
		Env:   map[string]string{"DB_HOST": "localhost", "DB_PORT": "5432"},
	})
	require.NoError(t, err)
	require.Len(t, ms.createdSpecs, 1)

	env := ms.createdSpecs[0].TaskTemplate.ContainerSpec.Env
	assert.Len(t, env, 2)
}

func TestDeployNetworkAttached(t *testing.T) {
	ms := newMockSwarm()
	d := NewDeployer(ms, testLogger())

	err := d.Deploy(context.Background(), DeployRequest{
		Name:  "net-app",
		Image: "app:latest",
	})
	require.NoError(t, err)
	require.Len(t, ms.createdSpecs, 1)

	networks := ms.createdSpecs[0].TaskTemplate.Networks
	require.Len(t, networks, 1)
	assert.Equal(t, "hive-net", networks[0].Target)
}
