package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/lholliger/hive/internal/dns"
	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/internal/tunnel"
)

// ensureAppDNSRecord creates, updates, or deletes DNS records when an app's domain changes.
// It runs asynchronously from a goroutine so it doesn't block the HTTP response.
func (s *Server) ensureAppDNSRecord(ctx context.Context, app *store.App, oldDomain, orgID string) {
	newDomain := app.Domain

	if oldDomain == newDomain {
		return
	}

	// Delete old record if domain changed or was cleared
	if oldDomain != "" {
		s.deleteDNSRecordForDomain(ctx, app.ID, oldDomain, orgID)
	}

	// Create new record if domain is set
	if newDomain != "" {
		s.createDNSRecordForDomain(ctx, app, orgID)
	}
}

// cleanupAppDNSRecords removes all managed DNS records for an app (used on app deletion).
func (s *Server) cleanupAppDNSRecords(ctx context.Context, appID, orgID string) {
	records, err := s.store.ListDNSRecordsByAppID(ctx, appID)
	if err != nil {
		s.log.Warnf("dns cleanup: list records for app %s: %v", appID, err)
		return
	}

	for _, rec := range records {
		if !rec.Managed || rec.ExternalID == "" {
			_ = s.store.DeleteDNSRecord(ctx, rec.ID)
			continue
		}

		provider, err := s.dnsProviderFromRecord(ctx, &rec)
		if err != nil {
			s.log.Warnf("dns cleanup: get provider for record %s: %v", rec.ID, err)
			_ = s.store.DeleteDNSRecord(ctx, rec.ID)
			continue
		}

		if err := provider.DeleteRecord(ctx, rec.ExternalID); err != nil {
			s.log.Warnf("dns cleanup: delete CF record %s for domain %s: %v", rec.ExternalID, rec.Domain, err)
		} else {
			s.log.Infof("dns cleanup: deleted DNS record for %s (app %s)", rec.Domain, appID)
		}
		_ = s.store.DeleteDNSRecord(ctx, rec.ID)
	}
}

func (s *Server) createDNSRecordForDomain(ctx context.Context, app *store.App, orgID string) {
	defaultProvider, err := s.store.GetDefaultDNSProvider(ctx, orgID)
	if err != nil {
		s.log.Debugf("dns auto: no default DNS provider for org %s, skipping", orgID)
		return
	}

	provider, cfgMap, err := s.instantiateDNSProvider(ctx, defaultProvider)
	if err != nil {
		s.log.Warnf("dns auto: instantiate provider %s: %v", defaultProvider.ID, err)
		return
	}

	recordType, value, proxied, err := s.dnsTarget(ctx)
	if err != nil {
		s.log.Warnf("dns auto: determine target: %v", err)
		return
	}

	_ = cfgMap

	externalID, err := provider.CreateRecord(ctx, app.Domain, recordType, value, proxied)
	if err != nil {
		s.log.Errorf("dns auto: create record for %s: %v", app.Domain, err)
		return
	}

	rec := &store.DNSRecord{
		ProviderID: defaultProvider.ID,
		AppID:      app.ID,
		Domain:     app.Domain,
		RecordType: recordType,
		Value:      value,
		Proxied:    proxied,
		Managed:    true,
		ExternalID: externalID,
	}
	if err := s.store.UpsertDNSRecord(ctx, rec); err != nil {
		s.log.Errorf("dns auto: save dns_record for %s: %v", app.Domain, err)
		return
	}
	s.log.Infof("dns auto: created %s record for %s -> %s (app %s)", recordType, app.Domain, value, app.ID)
}

func (s *Server) deleteDNSRecordForDomain(ctx context.Context, appID, domain, orgID string) {
	rec, err := s.store.GetDNSRecordByAppDomain(ctx, appID, domain)
	if err != nil {
		return
	}
	if !rec.Managed || rec.ExternalID == "" {
		_ = s.store.DeleteDNSRecord(ctx, rec.ID)
		return
	}

	provider, err := s.dnsProviderFromRecord(ctx, rec)
	if err != nil {
		s.log.Warnf("dns auto: get provider for deletion: %v", err)
		_ = s.store.DeleteDNSRecord(ctx, rec.ID)
		return
	}

	if err := provider.DeleteRecord(ctx, rec.ExternalID); err != nil {
		s.log.Warnf("dns auto: delete CF record %s for %s: %v", rec.ExternalID, domain, err)
	} else {
		s.log.Infof("dns auto: deleted DNS record for %s (app %s)", domain, appID)
	}
	_ = s.store.DeleteDNSRecord(ctx, rec.ID)
}

// dnsTarget determines the record type and value based on the current ingress mode.
func (s *Server) dnsTarget(ctx context.Context) (recordType, value string, proxied bool, err error) {
	ingressMode := "port_forward"
	if v, e := s.store.GetSetting(ctx, "ingress_mode"); e == nil && v != "" {
		ingressMode = v
	}

	switch ingressMode {
	case "cloudflare_tunnel", "both":
		tunnelToken, _ := s.store.GetSetting(ctx, "cf_tunnel_token")
		tid := tunnel.ParseTunnelID(tunnelToken)
		if tid == "" {
			return "", "", false, fmt.Errorf("cloudflare tunnel configured but tunnel ID could not be extracted from token")
		}
		return "CNAME", tid + ".cfargotunnel.com", true, nil

	default:
		ip, err := s.getPublicIP(ctx)
		if err != nil {
			return "", "", false, fmt.Errorf("determine public IP: %w", err)
		}
		return "A", ip, false, nil
	}
}

func (s *Server) getPublicIP(ctx context.Context) (string, error) {
	if ip, err := s.store.GetSetting(ctx, "public_ip"); err == nil && ip != "" {
		return ip, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.ipify.org", nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()

	var buf [64]byte
	n, _ := resp.Body.Read(buf[:])
	ip := string(buf[:n])
	if ip == "" {
		return "", fmt.Errorf("empty response from ipify")
	}

	_ = s.store.SetSetting(ctx, "public_ip", ip)
	return ip, nil
}

func (s *Server) instantiateDNSProvider(_ context.Context, p *store.DNSProvider) (dns.Provider, map[string]string, error) {
	var cfgMap map[string]string
	if p.ConfigEncrypted != nil {
		if err := json.Unmarshal(p.ConfigEncrypted, &cfgMap); err != nil {
			return nil, nil, fmt.Errorf("unmarshal provider config: %w", err)
		}
	}
	if cfgMap == nil {
		return nil, nil, fmt.Errorf("provider %s has no config", p.ID)
	}

	prov, err := dns.NewProvider(p.Type, cfgMap)
	if err != nil {
		return nil, nil, err
	}
	return prov, cfgMap, nil
}

func (s *Server) dnsProviderFromRecord(ctx context.Context, rec *store.DNSRecord) (dns.Provider, error) {
	p, err := s.store.GetDNSProvider(ctx, rec.ProviderID)
	if err != nil {
		return nil, err
	}
	prov, _, err := s.instantiateDNSProvider(ctx, p)
	return prov, err
}

// HasManagedDNSRecord checks if a managed DNS record exists for the given app.
func (s *Server) HasManagedDNSRecord(ctx context.Context, appID string) bool {
	records, err := s.store.ListDNSRecordsByAppID(ctx, appID)
	if err != nil || len(records) == 0 {
		return false
	}
	for _, r := range records {
		if r.Managed {
			return true
		}
	}
	return false
}

// HasDefaultDNSProvider checks if a default DNS provider is configured for the org.
func (s *Server) HasDefaultDNSProvider(ctx context.Context, orgID string) bool {
	_, err := s.store.GetDefaultDNSProvider(ctx, orgID)
	return err == nil
}

// ReconcileManagedDNSRecords performs immediate reconciliation for all managed
// records and also ensures domain-backed apps have a managed record.
func (s *Server) ReconcileManagedDNSRecords(ctx context.Context) error {
	if err := s.ValidateDNSRecords(ctx); err != nil {
		s.log.Warnf("dns reconcile: validation errors: %v", err)
	}
	apps, err := s.store.ListAllApps(ctx)
	if err != nil {
		return fmt.Errorf("dns reconcile: list apps: %w", err)
	}
	for _, app := range apps {
		if app.Domain == "" {
			continue
		}
		project, pErr := s.store.GetProject(ctx, app.ProjectID)
		if pErr != nil || project == nil {
			continue
		}
		if _, recErr := s.store.GetDNSRecordByAppDomain(ctx, app.ID, app.Domain); recErr == nil {
			continue
		} else if recErr != nil && recErr != sql.ErrNoRows {
			continue
		}
		s.createDNSRecordForDomain(ctx, &app, project.OrgID)
	}
	return nil
}
