package engine

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/lholliger/hive/internal/store"
)

// ValidateDNSRecords checks all managed DNS records against the DNS provider
// and repairs any drift (missing records, wrong values).
func (s *Server) ValidateDNSRecords(ctx context.Context) error {
	if s.store == nil {
		return nil
	}

	records, err := s.store.ListAllManagedDNSRecords(ctx)
	if err != nil {
		return fmt.Errorf("list managed DNS records: %w", err)
	}
	if len(records) == 0 {
		return nil
	}

	expectedType, expectedValue, expectedProxied, targetErr := s.dnsTarget(ctx)

	validated := 0
	repaired := 0
	errors := 0

	for _, rec := range records {
		if rec.ExternalID == "" {
			continue
		}

		provider, err := s.dnsProviderFromRecord(ctx, &rec)
		if err != nil {
			s.log.Debugf("dns validation: skip record %s (provider error: %v)", rec.Domain, err)
			errors++
			continue
		}

		remoteRecords, err := provider.ListRecords(ctx, rec.Domain)
		if err != nil {
			s.log.Warnf("dns validation: list records for %s: %v", rec.Domain, err)
			errors++
			continue
		}

		found := false
		for _, remote := range remoteRecords {
			if remote.ExternalID == rec.ExternalID {
				found = true

				if targetErr == nil && remote.Value != expectedValue {
					s.log.Infof("dns validation: repairing %s (value %s -> %s)", rec.Domain, remote.Value, expectedValue)
					if err := provider.UpdateRecord(ctx, rec.ExternalID, rec.Domain, expectedType, expectedValue, expectedProxied); err != nil {
						s.log.Warnf("dns validation: repair %s: %v", rec.Domain, err)
						errors++
					} else {
						rec.Value = expectedValue
						rec.RecordType = expectedType
						rec.Proxied = expectedProxied
						_ = s.store.UpsertDNSRecord(ctx, &rec)
						repaired++
					}
				}
				break
			}
		}

		if !found {
			app, aErr := s.store.GetApp(ctx, rec.AppID)
			if aErr == nil && app != nil && app.Domain == rec.Domain {
				orgID := "default"
				if project, pErr := s.store.GetProject(ctx, app.ProjectID); pErr == nil && project != nil && project.OrgID != "" {
					orgID = project.OrgID
				}
				s.log.Infof("dns validation: re-creating missing record for %s", rec.Domain)
				s.createDNSRecordForDomain(ctx, app, orgID)
				repaired++
			} else {
				_ = s.store.DeleteDNSRecord(ctx, rec.ID)
			}
		}

		validated++
	}

	s.log.Debugf("dns validation: %d checked, %d repaired, %d errors", validated, repaired, errors)
	if errors > 0 {
		return fmt.Errorf("%d records had validation errors", errors)
	}
	return nil
}

// CheckTunnelHealth verifies the Cloudflare tunnel service is running and redeploys if needed.
func (s *Server) CheckTunnelHealth(ctx context.Context) error {
	if s.cfManager == nil {
		return nil
	}

	if s.store != nil {
		token, err := s.store.GetSetting(ctx, "cf_tunnel_token")
		if err != nil || token == "" {
			return nil
		}
	}

	if s.cfg.CFTunnelToken == "" {
		return nil
	}

	if s.cfManager.IsRunning(ctx) {
		return nil
	}

	s.log.Warn("tunnel health: cloudflared tunnel not running, redeploying")
	if err := s.cfManager.EnsureTunnel(ctx); err != nil {
		return fmt.Errorf("redeploy tunnel: %w", err)
	}
	s.log.Info("tunnel health: cloudflared tunnel redeployed")
	return nil
}

// CheckTraefikHealth verifies the Traefik service is running with expected replicas
// and triggers a force-update if tasks are failing.
func (s *Server) CheckTraefikHealth(ctx context.Context) error {
	if s.sc == nil {
		return nil
	}

	svc, err := s.sc.GetService(ctx, "hive-traefik")
	if err != nil || svc == nil {
		return fmt.Errorf("traefik service not found")
	}

	tasks, err := s.sc.ServiceTasks(ctx, svc.ID)
	if err != nil {
		return fmt.Errorf("list traefik tasks: %w", err)
	}

	running := 0
	for _, t := range tasks {
		if t.Status.State == "running" {
			running++
		}
	}

	desired := 1
	if svc.Spec.Mode.Replicated != nil && svc.Spec.Mode.Replicated.Replicas != nil {
		desired = int(*svc.Spec.Mode.Replicated.Replicas)
	}

	if running < desired {
		s.log.Warnf("traefik health: %d/%d running, triggering force-update", running, desired)
		svc.Spec.TaskTemplate.ForceUpdate++
		if err := s.sc.UpdateService(ctx, svc.ID, svc.Version, svc.Spec); err != nil {
			return fmt.Errorf("force-update traefik: %w", err)
		}
		s.log.Info("traefik health: force-updated traefik service")
	}

	return nil
}

// SyncAllTemplateSources syncs all configured external template sources.
func (s *Server) SyncAllTemplateSources(ctx context.Context) error {
	if s.store == nil {
		return nil
	}

	sources, err := s.store.ListTemplateSources(ctx, "default")
	if err != nil {
		return fmt.Errorf("list template sources: %w", err)
	}
	if len(sources) == 0 {
		return nil
	}

	synced := 0
	for _, src := range sources {
		if err := s.syncOneTemplateSource(ctx, &src); err != nil {
			s.log.Warnf("template sync: source %s: %v", src.Name, err)
		} else {
			synced++
		}
	}

	s.log.Debugf("template sync: %d/%d sources synced", synced, len(sources))
	return nil
}

func (s *Server) syncOneTemplateSource(ctx context.Context, src *store.TemplateSource) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", src.URL, nil)
	if err != nil {
		return fmt.Errorf("create request for %s: %w", src.URL, err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", src.URL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("fetch %s: status %d", src.URL, resp.StatusCode)
	}

	_ = s.store.UpdateTemplateSyncTime(ctx, src.ID)
	return nil
}
