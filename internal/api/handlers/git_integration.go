package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/lholliger/hive/internal/api/middleware"
	"github.com/lholliger/hive/internal/git"
	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/pkg/config"
	"github.com/lholliger/hive/pkg/encryption"
)

func ListGitRepos(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sourceID := chi.URLParam(r, "sourceId")
		s := storeFromRequest(r)
		gs, err := s.GetGitSource(r.Context(), sourceID)
		if err != nil || gs == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "git source not found"})
			return
		}
		orgID := middleware.GetOrgID(r.Context())
		if orgID != "" && gs.OrgID != orgID {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		token, err := encryption.Decrypt(gs.TokenEncrypted)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to decrypt token"})
			return
		}
		provider := providerFromSource(string(token), gs)
		if provider == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported provider"})
			return
		}
		repos, err := provider.ListRepos(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, repos)
	}
}

func ListGitRepoBranches(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "sourceId")
	repo, err := url.PathUnescape(chi.URLParam(r, "repo"))
	if err != nil || repo == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid repo"})
		return
	}
	s := storeFromRequest(r)
	gs, err := s.GetGitSource(r.Context(), sourceID)
	if err != nil || gs == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "git source not found"})
		return
	}
	orgID := middleware.GetOrgID(r.Context())
	if orgID != "" && gs.OrgID != orgID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	token, err := encryption.Decrypt(gs.TokenEncrypted)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to decrypt token"})
		return
	}
	provider := providerFromSource(string(token), gs)
	if provider == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported provider"})
		return
	}
	branches, err := provider.ListBranches(r.Context(), repo)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, branches)
}

func RegisterWebhook(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sourceID := chi.URLParam(r, "sourceId")
		repo, err := url.PathUnescape(chi.URLParam(r, "repo"))
		if err != nil || repo == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid repo"})
			return
		}
		s := storeFromRequest(r)
		gs, err := s.GetGitSource(r.Context(), sourceID)
		if err != nil || gs == nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "git source not found"})
			return
		}
		orgID := middleware.GetOrgID(r.Context())
		if orgID != "" && gs.OrgID != orgID {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		if id, ok := gs.WebhookIDs[repo]; ok && id != "" {
			writeJSON(w, http.StatusOK, map[string]string{"webhook_id": id, "status": "already registered"})
			return
		}
		token, err := encryption.Decrypt(gs.TokenEncrypted)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to decrypt token"})
			return
		}
		provider := providerFromSource(string(token), gs)
		if provider == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported provider"})
			return
		}
		base := cfg.WebhookBaseURL
		if base == "" {
			base = "http://localhost:8080"
		}
		base = strings.TrimSuffix(base, "/")
		callbackURL := base + "/api/v1/webhooks/" + sourceID
		var secret string
		if len(gs.WebhookSecretEncrypted) > 0 {
			dec, err := encryption.Decrypt(gs.WebhookSecretEncrypted)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to decrypt webhook secret"})
				return
			}
			secret = string(dec)
		} else {
			secret = genWebhookSecret()
			secretEnc, err := encryption.Encrypt([]byte(secret))
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "encryption failed"})
				return
			}
			gs.WebhookSecretEncrypted = secretEnc
			if err := s.UpdateGitSourceWebhookSecret(r.Context(), sourceID, secretEnc); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		}
		webhookID, err := provider.CreateWebhook(r.Context(), repo, callbackURL, secret)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if err := s.AddRepoWebhookID(r.Context(), sourceID, repo, webhookID); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"webhook_id": webhookID, "status": "registered"})
	}
}

func DetectBuildType(w http.ResponseWriter, r *http.Request) {
	sourceID := chi.URLParam(r, "sourceId")
	repo, err := url.PathUnescape(chi.URLParam(r, "repo"))
	if err != nil || repo == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid repo"})
		return
	}
	branch := r.URL.Query().Get("branch")
	if branch == "" {
		branch = "main"
	}
	s := storeFromRequest(r)
	gs, err := s.GetGitSource(r.Context(), sourceID)
	if err != nil || gs == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "git source not found"})
		return
	}
	orgID := middleware.GetOrgID(r.Context())
	if orgID != "" && gs.OrgID != orgID {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	token, err := encryption.Decrypt(gs.TokenEncrypted)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to decrypt token"})
		return
	}
	provider := providerFromSource(string(token), gs)
	if provider == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported provider"})
		return
	}
	buildType, err := provider.DetectBuildType(r.Context(), repo, branch)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"build_type": buildType})
}

func providerFromSource(token string, gs *store.GitSource) git.Provider {
	p := gs.ProviderName
	if p == "" {
		p = gs.Provider
	}
	switch strings.ToLower(p) {
	case "github":
		return git.NewGitHubProvider(token)
	case "gitlab":
		return git.NewGitLabProvider(token)
	default:
		return nil
	}
}

func genWebhookSecret() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
