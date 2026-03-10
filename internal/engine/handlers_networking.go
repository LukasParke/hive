package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/lholliger/hive/internal/tunnel"
)

func (s *Server) apiGetNetworkingSettings(w http.ResponseWriter, r *http.Request) {
	_, err := requireViewer(r)
	if handleErr(w, err) {
		return
	}

	ingressMode := "port_forward"
	tunnelToken := ""
	tunnelRunning := false

	cfAPIToken := ""

	if s.store != nil {
		if v, err := s.store.GetSetting(r.Context(), "ingress_mode"); err == nil {
			ingressMode = v
		}
		if v, err := s.store.GetSetting(r.Context(), "cf_tunnel_token"); err == nil && v != "" {
			if len(v) > 8 {
				tunnelToken = v[:4] + "..." + v[len(v)-4:]
			} else {
				tunnelToken = "***"
			}
		}
		if v, err := s.store.GetSetting(r.Context(), "cf_api_token"); err == nil && v != "" {
			if len(v) > 8 {
				cfAPIToken = v[:4] + "..." + v[len(v)-4:]
			} else {
				cfAPIToken = "***"
			}
		}
	}

	tunnelCNAME := ""
	if s.cfManager != nil {
		tunnelRunning = s.cfManager.IsRunning(r.Context())
		tunnelCNAME = s.cfManager.CNAMETarget()
	}
	if tunnelCNAME == "" && s.store != nil {
		if t, err := s.store.GetSetting(r.Context(), "cf_tunnel_token"); err == nil && t != "" {
			if tid := tunnel.ParseTunnelID(t); tid != "" {
				tunnelCNAME = tid + ".cfargotunnel.com"
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ingress_mode":        ingressMode,
		"tunnel_token":        tunnelToken,
		"tunnel_running":      tunnelRunning,
		"cf_api_token":        cfAPIToken,
		"tunnel_cname_target": tunnelCNAME,
	})
}

func (s *Server) apiUpdateNetworkingSettings(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}

	var req struct {
		IngressMode string `json:"ingress_mode"`
		TunnelToken string `json:"tunnel_token"`
		CFAPIToken  string `json:"cf_api_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "invalid request body", nil)
		return
	}

	ctx := r.Context()
	prevIngressMode := ""
	if s.store != nil {
		if v, err := s.store.GetSetting(ctx, "ingress_mode"); err == nil {
			prevIngressMode = v
		}
	}

	if req.IngressMode != "" && s.store != nil {
		if err := s.store.SetSetting(ctx, "ingress_mode", req.IngressMode); err != nil {
			s.log.Warnf("save ingress_mode: %v", err)
		}
	}

	if req.TunnelToken != "" && s.store != nil {
		if err := s.store.SetSetting(ctx, "cf_tunnel_token", req.TunnelToken); err != nil {
			s.log.Warnf("save cf_tunnel_token: %v", err)
		}
		s.cfg.CFTunnelToken = req.TunnelToken
	}

	if req.CFAPIToken != "" && s.store != nil {
		if err := s.store.SetSetting(ctx, "cf_api_token", req.CFAPIToken); err != nil {
			s.log.Warnf("save cf_api_token: %v", err)
		}
		if err := s.updateTraefikCFToken(ctx, req.CFAPIToken); err != nil {
			writeAPIError(w, http.StatusBadGateway, "bad_gateway", "failed to propagate CF token to Traefik: "+err.Error(), nil)
			return
		}
	}

	if s.cfManager != nil {
		switch req.IngressMode {
		case "cloudflare_tunnel", "both":
			if err := s.cfManager.EnsureTunnel(ctx); err != nil {
				s.log.Errorf("ensure tunnel: %v", err)
				writeAPIError(w, http.StatusInternalServerError, "internal_error", "failed to deploy tunnel: "+err.Error(), nil)
				return
			}
		case "port_forward":
			if err := s.cfManager.RemoveTunnel(ctx); err != nil {
				s.log.Warnf("remove tunnel: %v", err)
			}
		}
	}

	if req.IngressMode != "" && req.IngressMode != prevIngressMode {
		go func() {
			reconcileCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			if err := s.ReconcileManagedDNSRecords(reconcileCtx); err != nil {
				s.log.Warnf("dns reconcile after ingress mode change: %v", err)
			}
		}()
	}

	s.auditLog(r, "update", "networking_settings", "", "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "updated"})
}

func (s *Server) updateTraefikCFToken(ctx context.Context, token string) error {
	if s.sc == nil {
		return nil
	}
	svc, err := s.sc.GetService(ctx, "hive-traefik")
	if err != nil || svc == nil {
		return fmt.Errorf("get hive-traefik service: %w", err)
	}

	envKey := "CF_DNS_API_TOKEN="
	var newEnv []string
	found := false
	for _, e := range svc.Spec.TaskTemplate.ContainerSpec.Env {
		if strings.HasPrefix(e, envKey) {
			newEnv = append(newEnv, envKey+token)
			found = true
		} else {
			newEnv = append(newEnv, e)
		}
	}
	if !found {
		newEnv = append(newEnv, envKey+token)
	}
	svc.Spec.TaskTemplate.ContainerSpec.Env = newEnv

	if err := s.sc.UpdateService(ctx, svc.ID, svc.Version, svc.Spec); err != nil {
		return fmt.Errorf("update hive-traefik: %w", err)
	}
	s.log.Info("propagated CF_DNS_API_TOKEN to hive-traefik service")
	return nil
}

func (s *Server) EnsureTraefikCFToken(ctx context.Context) error {
	if s.store == nil || s.sc == nil {
		return nil
	}
	token, err := s.store.GetSetting(ctx, "cf_api_token")
	if err != nil || token == "" {
		return nil
	}
	return s.updateTraefikCFToken(ctx, token)
}

func (s *Server) EnsureCloudflaredTunnel(ctx context.Context) error {
	if s.cfManager == nil {
		return nil
	}
	return s.cfManager.EnsureTunnel(ctx)
}

func (s *Server) apiTestTunnelConnection(w http.ResponseWriter, r *http.Request) {
	_, err := requireMember(r)
	if handleErr(w, err) {
		return
	}

	if s.cfManager == nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"running": false,
			"message": "swarm client not available",
		})
		return
	}

	running := s.cfManager.IsRunning(r.Context())
	msg := "tunnel is not running"
	if running {
		msg = "tunnel is running"
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"running": running,
		"message": msg,
	})
}
