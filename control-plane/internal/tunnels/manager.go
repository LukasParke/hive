package tunnels

import (
	"context"
	"errors"
	"fmt"
	"strings"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moby/moby/api/types/swarm"

	"github.com/luke/hive/control-plane/internal/cloudflare"
	swarmclient "github.com/luke/hive/control-plane/internal/swarm"
)

// SwarmAPI is the subset of the swarm client the manager needs.
// *swarmclient.Client satisfies it; tests supply a fake.
type SwarmAPI interface {
	CreateService(ctx context.Context, spec swarm.ServiceSpec) (string, error)
	GetService(ctx context.Context, id string) (swarm.Service, error)
	UpdateService(ctx context.Context, id string, version uint64, spec swarm.ServiceSpec) error
	RemoveService(ctx context.Context, id string) error
	ListTasks(ctx context.Context, serviceID string) ([]swarm.Task, error)
	CreateSecret(ctx context.Context, spec swarm.SecretSpec) (string, error)
	RemoveSecret(ctx context.Context, id string) error
	ListSecrets(ctx context.Context) ([]swarm.Secret, error)
}

var _ SwarmAPI = (*swarmclient.Client)(nil)

// ClientFactory builds a Cloudflare API client for the given API token.
type ClientFactory func(apiToken string) cloudflare.API

// Manager drives the full tunnel lifecycle against Cloudflare, the
// encrypted secrets store and the swarm.
type Manager struct {
	Repo      Repository
	Swarm     SwarmAPI
	Store     CredentialStore
	NewClient ClientFactory
}

// NewManager wires a Manager over the production sqlc repository and the
// given credential store (secrets.Runtime()). A nil store makes
// operations that need credentials fail fast instead of storing
// plaintext.
func NewManager(pool *pgxpool.Pool, swarm SwarmAPI, store CredentialStore, factory ClientFactory) *Manager {
	return &Manager{Repo: NewSQLRepo(pool), Swarm: swarm, Store: store, NewClient: factory}
}

// Create provisions the upstream tunnel, stores its credentials and API
// token encrypted, deploys the connector service, publishes DNS routes
// for every ingress hostname and persists the row as deployed.
func (m *Manager) Create(ctx context.Context, p CreateParams) (*View, error) {
	if m.Store == nil {
		return nil, fmt.Errorf("create tunnel %s: %w", p.Name, ErrNoCredentials)
	}
	if err := ValidateName(p.Name); err != nil {
		return nil, err
	}
	if err := ValidateIngress(p.Ingress); err != nil {
		return nil, err
	}
	if p.AccountID == "" {
		return nil, InvalidInput("accountId is required")
	}
	if p.APIToken == "" {
		return nil, InvalidInput("apiToken is required")
	}
	if _, err := m.Repo.GetByName(ctx, p.Name); err == nil {
		return nil, fmt.Errorf("%w: name %q", ErrConflict, p.Name)
	} else if !errors.Is(err, ErrNotFound) {
		return nil, fmt.Errorf("look up tunnel %s: %w", p.Name, err)
	}

	cf := m.NewClient(p.APIToken)
	ref, err := cf.CreateTunnel(ctx, p.AccountID, p.Name)
	if err != nil {
		return nil, fmt.Errorf("cloudflare create tunnel %s: %w", p.Name, err)
	}

	row := &Row{
		Name:                 p.Name,
		CfTunnelID:           ref.ID,
		AccountID:            p.AccountID,
		ZoneID:               p.ZoneID,
		CredentialSecretName: credentialSecretKey(ref.ID),
		Ingress:              append([]IngressRule{}, p.Ingress...),
		DNSRecords:           map[string]string{},
		Status:               StatusCreating,
	}

	if err := m.Store.Put(ctx, row.CredentialSecretName, SecretType, ref.CredentialsJSON); err != nil {
		return nil, fmt.Errorf("store tunnel credentials: %w", err)
	}
	if err := m.Store.Put(ctx, apiTokenSecretKey(ref.ID), SecretType, []byte(p.APIToken)); err != nil {
		return nil, fmt.Errorf("store tunnel api token: %w", err)
	}
	if err := m.Repo.Create(ctx, row); err != nil {
		return nil, fmt.Errorf("persist tunnel %s: %w", p.Name, err)
	}
	if err := m.failRow(ctx, row, func() error { return m.deployService(ctx, row, ref.CredentialsJSON) }); err != nil {
		return nil, err
	}
	if err := m.failRow(ctx, row, func() error { return m.publishDNS(ctx, row) }); err != nil {
		return nil, err
	}
	if err := m.Repo.SetStatus(ctx, row.ID, StatusDeployed, ""); err != nil {
		return nil, fmt.Errorf("mark tunnel %s deployed: %w", p.Name, err)
	}
	row.Status = StatusDeployed
	return m.Get(ctx, row.ID)
}

// failRow runs step and, on failure, marks the row errored before
// returning the wrapped failure.
func (m *Manager) failRow(ctx context.Context, row *Row, step func() error) error {
	err := step()
	if err == nil {
		return nil
	}
	wrapped := fmt.Errorf("deploy tunnel %s: %w", row.Name, err)
	if statusErr := m.Repo.SetStatus(ctx, row.ID, StatusError, wrapped.Error()); statusErr != nil {
		return fmt.Errorf("%v (also failed to record error state: %w)", wrapped, statusErr)
	}
	return wrapped
}

// deployService renders the cloudflared config, uploads the credentials
// JSON and rendered config as swarm secrets at revision 1 and creates the
// connector service on hive_internal.
func (m *Manager) deployService(ctx context.Context, row *Row, credentialsJSON []byte) error {
	credName := credentialSecretName(row.Name)
	credID, err := m.ensureSwarmSecret(ctx, credName, credentialsJSON)
	if err != nil {
		return err
	}
	config := RenderConfig(row.CfTunnelID, "/run/secrets/"+credName, row.Ingress)
	configID, err := m.ensureSwarmSecret(ctx, configSecretName(row.Name, 1), []byte(config))
	if err != nil {
		return err
	}
	spec, err := buildServiceSpec(row.Name, 1, configID, credID)
	if err != nil {
		return err
	}
	if _, err := m.Swarm.CreateService(ctx, spec); err != nil {
		return fmt.Errorf("create connector service %s: %w", serviceName(row.Name), err)
	}
	return nil
}

// swarmSecretID looks up a swarm secret by exact name; empty when absent.
func (m *Manager) swarmSecretID(ctx context.Context, name string) string {
	secrets, err := m.Swarm.ListSecrets(ctx)
	if err != nil {
		return ""
	}
	for _, s := range secrets {
		if s.Spec.Name == name {
			return s.ID
		}
	}
	return ""
}

// UpdateIngress replaces the ingress rule list: re-renders the config,
// rotates the config secret revision, force-restarts the connector via a
// label-bumped service update, diffs DNS routes and persists the result.
func (m *Manager) UpdateIngress(ctx context.Context, id string, rules []IngressRule) (*View, error) {
	if m.Store == nil {
		return nil, fmt.Errorf("update tunnel ingress: %w", ErrNoCredentials)
	}
	if err := ValidateIngress(rules); err != nil {
		return nil, err
	}
	row, err := m.Repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	token, err := m.loadToken(ctx, row.CfTunnelID)
	if err != nil {
		return nil, err
	}
	cf := m.NewClient(token)

	svc, err := m.Swarm.GetService(ctx, serviceName(row.Name))
	switch {
	case cerrdefs.IsNotFound(err):
		return nil, fmt.Errorf("connector service %s: %w", serviceName(row.Name), ErrNotFound)
	case err != nil:
		return nil, fmt.Errorf("inspect connector service %s: %w", serviceName(row.Name), err)
	}
	nextRev := revisionLabel(svc.Spec.Annotations.Labels) + 1 //nolint:staticcheck // test fixture

	config := RenderConfig(row.CfTunnelID, "/run/secrets/"+credentialSecretName(row.Name), rules)
	configName := configSecretName(row.Name, nextRev)
	configID, err := m.ensureSwarmSecret(ctx, configName, []byte(config))
	if err != nil {
		return nil, fmt.Errorf("rotate config secret: %w", err)
	}
	credID := m.swarmSecretID(ctx, credentialSecretName(row.Name))
	if credID == "" {
		return nil, fmt.Errorf("connector credentials secret %s not found", credentialSecretName(row.Name))
	}

	spec, err := buildServiceSpec(row.Name, nextRev, configID, credID)
	if err != nil {
		return nil, err
	}
	if err := m.Swarm.UpdateService(ctx, svc.ID, svc.Version.Index, spec); err != nil {
		_ = m.Swarm.RemoveSecret(ctx, configID)
		return nil, fmt.Errorf("redeploy connector service %s: %w", serviceName(row.Name), err)
	}
	m.pruneConfigRevisions(ctx, row.Name, nextRev)

	if err := m.diffDNS(ctx, cf, row, rules); err != nil {
		return nil, err
	}
	if err := m.Repo.UpdateIngress(ctx, id, rules); err != nil {
		return nil, fmt.Errorf("persist tunnel ingress: %w", err)
	}
	return m.Get(ctx, id)
}

// Delete tears the tunnel down: best-effort DNS record removal, Cloudflare
// tunnel deletion, connector service removal, swarm secret cleanup,
// encrypted credential purge and row deletion.
func (m *Manager) Delete(ctx context.Context, id string) error {
	row, err := m.Repo.Get(ctx, id)
	if err != nil {
		return err
	}
	var cf cloudflare.API
	if m.Store != nil {
		if token, err := m.loadToken(ctx, row.CfTunnelID); err == nil {
			cf = m.NewClient(token)
		}
	}
	if cf != nil && row.ZoneID != "" {
		for _, recordID := range row.DNSRecords {
			_ = cf.DeleteDNSRecord(ctx, row.ZoneID, recordID) // best-effort
		}
	}
	if cf != nil {
		if err := cf.DeleteTunnel(ctx, row.AccountID, row.CfTunnelID); err != nil {
			return fmt.Errorf("cloudflare delete tunnel %s: %w", row.Name, err)
		}
	}
	if err := m.Swarm.RemoveService(ctx, serviceName(row.Name)); err != nil && !cerrdefs.IsNotFound(err) {
		return fmt.Errorf("remove connector service %s: %w", serviceName(row.Name), err)
	}
	m.pruneConfigRevisions(ctx, row.Name, -1)
	_ = m.Swarm.RemoveSecret(ctx, credentialSecretName(row.Name)) // best-effort
	if err := m.Repo.ForgetSecrets(ctx, []string{
		row.CredentialSecretName,
		apiTokenSecretKey(row.CfTunnelID),
	}); err != nil {
		return fmt.Errorf("purge stored tunnel secrets: %w", err)
	}
	if err := m.Repo.Delete(ctx, id); err != nil {
		return fmt.Errorf("delete tunnel row %s: %w", row.Name, err)
	}
	return nil
}

// Get returns a tunnel with live connector health.
func (m *Manager) Get(ctx context.Context, id string) (*View, error) {
	row, err := m.Repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	connector, err := m.connectorStatus(ctx, row)
	if err != nil {
		return nil, err
	}
	return &View{Row: row, Connector: connector}, nil
}

// List returns every tunnel with live connector health.
func (m *Manager) List(ctx context.Context) ([]*View, error) {
	rows, err := m.Repo.List(ctx)
	if err != nil {
		return nil, err
	}
	views := make([]*View, 0, len(rows))
	for _, row := range rows {
		connector, err := m.connectorStatus(ctx, row)
		if err != nil {
			return nil, err
		}
		views = append(views, &View{Row: row, Connector: connector})
	}
	return views, nil
}

// Status returns live connector health without the full row.
func (m *Manager) Status(ctx context.Context, id string) (ConnectorStatus, error) {
	view, err := m.Get(ctx, id)
	if err != nil {
		return ConnectorStatus{}, err
	}
	return view.Connector, nil
}

// connectorStatus combines the swarm task counts with the
// Cloudflare-reported tunnel status. Cloudflare is consulted
// best-effort: an unreachable API leaves CloudflareStatus empty rather
// than failing reads of the dashboard.
func (m *Manager) connectorStatus(ctx context.Context, row *Row) (ConnectorStatus, error) {
	status := ConnectorStatus{}
	svc, err := m.Swarm.GetService(ctx, serviceName(row.Name))
	switch {
	case cerrdefs.IsNotFound(err):
		return status, nil
	case err != nil:
		return ConnectorStatus{}, fmt.Errorf("inspect connector service %s: %w", serviceName(row.Name), err)
	}
	if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil {
		status.DesiredReplicas = *svc.Spec.Mode.Replicated.Replicas
	}
	tasks, err := m.Swarm.ListTasks(ctx, svc.ID)
	if err != nil {
		return ConnectorStatus{}, fmt.Errorf("list connector tasks %s: %w", serviceName(row.Name), err)
	}
	for _, t := range tasks {
		if t.Status.State == swarm.TaskStateRunning {
			status.RunningReplicas++
		}
	}
	if m.Store != nil {
		if token, err := m.loadToken(ctx, row.CfTunnelID); err == nil {
			if cfStatus, err := m.NewClient(token).GetTunnel(ctx, row.AccountID, row.CfTunnelID); err == nil {
				status.CloudflareStatus = cfStatus
			}
		}
	}
	return status, nil
}

// loadToken decrypts the stored Cloudflare API token for a tunnel.
func (m *Manager) loadToken(ctx context.Context, cfTunnelID string) (string, error) {
	if m.Store == nil {
		return "", ErrNoCredentials
	}
	raw, err := m.Store.Get(ctx, apiTokenSecretKey(cfTunnelID), SecretType)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrNoCredentials, err)
	}
	return string(raw), nil
}

// ensureSwarmSecret replaces the named swarm secret with fresh data.
// Swarm secrets are immutable, so an existing copy is removed first;
// callers must only call this while no running task pins the old value.
func (m *Manager) ensureSwarmSecret(ctx context.Context, name string, data []byte) (string, error) {
	secrets, err := m.Swarm.ListSecrets(ctx)
	if err != nil {
		return "", fmt.Errorf("list swarm secrets: %w", err)
	}
	for _, s := range secrets {
		if s.Spec.Name == name {
			_ = m.Swarm.RemoveSecret(ctx, s.ID) // best-effort; recreated below
		}
	}
	id, err := m.Swarm.CreateSecret(ctx, swarm.SecretSpec{
		Annotations: swarm.Annotations{Name: name},
		Data:        data,
	})
	if err != nil {
		return "", fmt.Errorf("create swarm secret %s: %w", name, err)
	}
	return id, nil
}

// pruneConfigRevisions removes stale rendered-config revisions other than
// keepRev. With keepRev < 0 every revision is removed (delete path).
func (m *Manager) pruneConfigRevisions(ctx context.Context, name string, keepRev int) {
	prefix := configSecretPrefix(name)
	secrets, err := m.Swarm.ListSecrets(ctx)
	if err != nil {
		return // best-effort cleanup
	}
	for _, s := range secrets {
		if !strings.HasPrefix(s.Spec.Name, prefix) {
			continue
		}
		if keepRev >= 0 && s.Spec.Name == configSecretName(name, keepRev) {
			continue
		}
		_ = m.Swarm.RemoveSecret(ctx, s.ID)
	}
}

// publishDNS routes every ingress hostname through the zone and records
// the created CNAME record IDs. Wildcards are passed through unchanged:
// the Cloudflare API publishes `*.zone` CNAMEs like any other record.
func (m *Manager) publishDNS(ctx context.Context, row *Row) error {
	if row.ZoneID == "" {
		return nil
	}
	token, err := m.loadToken(ctx, row.CfTunnelID)
	if err != nil {
		return err
	}
	cf := m.NewClient(token)
	records := map[string]string{}
	for _, r := range row.Ingress {
		recordID, err := cf.CreateDNSRoute(ctx, row.ZoneID, r.Hostname, row.CfTunnelID)
		if err != nil {
			return fmt.Errorf("publish dns route for %s: %w", r.Hostname, err)
		}
		records[r.Hostname] = recordID
	}
	if err := m.Repo.UpdateDNSRecords(ctx, row.ID, records); err != nil {
		return fmt.Errorf("record dns routes: %w", err)
	}
	row.DNSRecords = records
	return nil
}

// diffDNS reconciles published routes against the new rule set: new
// hostnames get CNAMEs, dropped hostnames get their records deleted.
func (m *Manager) diffDNS(ctx context.Context, cf cloudflare.API, row *Row, rules []IngressRule) error {
	if row.ZoneID == "" {
		return nil
	}
	desired := map[string]bool{}
	for _, r := range rules {
		desired[r.Hostname] = true
	}
	next := map[string]string{}
	for host, recordID := range row.DNSRecords {
		if desired[host] {
			next[host] = recordID
			continue
		}
		if err := cf.DeleteDNSRecord(ctx, row.ZoneID, recordID); err != nil {
			return fmt.Errorf("remove dns route for %s: %w", host, err)
		}
	}
	for _, r := range rules {
		if _, ok := next[r.Hostname]; ok {
			continue
		}
		recordID, err := cf.CreateDNSRoute(ctx, row.ZoneID, r.Hostname, row.CfTunnelID)
		if err != nil {
			return fmt.Errorf("publish dns route for %s: %w", r.Hostname, err)
		}
		next[r.Hostname] = recordID
	}
	if err := m.Repo.UpdateDNSRecords(ctx, row.ID, next); err != nil {
		return fmt.Errorf("record dns routes: %w", err)
	}
	row.DNSRecords = next
	return nil
}
