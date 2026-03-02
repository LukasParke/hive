package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/nats-io/nats.go"

	"github.com/lholliger/hive/internal/api/middleware"
	"github.com/lholliger/hive/internal/preview"
	"github.com/lholliger/hive/internal/store"
	"github.com/lholliger/hive/pkg/encryption"
)

func CreateGitSource(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Provider string `json:"provider"`
		Token    string `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if body.Provider == "" || body.Token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider and token are required"})
		return
	}

	orgID := middleware.GetOrgID(r.Context())
	tokenEncrypted, err := encryption.Encrypt([]byte(body.Token))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "encryption failed"})
		return
	}

	s := storeFromRequest(r)
	gs := &store.GitSource{
		OrgID:          orgID,
		Provider:       body.Provider,
		TokenEncrypted: tokenEncrypted,
	}
	if err := s.CreateGitSource(r.Context(), gs); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": gs.ID, "provider": gs.Provider})
}

func ListGitSources(w http.ResponseWriter, r *http.Request) {
	orgID := middleware.GetOrgID(r.Context())
	s := storeFromRequest(r)
	sources, err := s.ListGitSources(r.Context(), orgID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var result []map[string]string
	for _, gs := range sources {
		result = append(result, map[string]string{
			"id":         gs.ID,
			"provider":   gs.Provider,
			"created_at": gs.CreatedAt.Format("2006-01-02T15:04:05Z"),
		})
	}
	writeJSON(w, http.StatusOK, result)
}

func GitWebhook(nc *nats.Conn, s *store.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sourceID := chi.URLParam(r, "sourceId")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body failed"})
			return
		}

		var webhookSecret string
		if s != nil {
			gs, err := s.GetGitSource(r.Context(), sourceID)
			if err == nil && gs != nil && len(gs.WebhookSecretEncrypted) > 0 {
				dec, err := encryption.Decrypt(gs.WebhookSecretEncrypted)
				if err == nil {
					webhookSecret = string(dec)
				}
			}
		}

		provider := "github"
		if s != nil {
			gs, _ := s.GetGitSource(r.Context(), sourceID)
			if gs != nil && gs.ProviderName != "" {
				provider = strings.ToLower(gs.ProviderName)
			} else if gs != nil {
				provider = strings.ToLower(gs.Provider)
			}
		}

		if provider == "github" || provider == "gitlab" {
			ghSig := r.Header.Get("X-Hub-Signature-256")
			glToken := r.Header.Get("X-Gitlab-Token")
			if provider == "github" && webhookSecret != "" && ghSig != "" {
				if !verifyGitHubSignature(body, ghSig, webhookSecret) {
					writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid signature"})
					return
				}
			} else if provider == "gitlab" && webhookSecret != "" && glToken != "" {
				if glToken != webhookSecret {
					writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
					return
				}
			}
		}

		ghEvent := r.Header.Get("X-GitHub-Event")

		var payload struct {
			Ref        string `json:"ref"`
			Action     string `json:"action"`
			Number     int    `json:"number"`
			Repository struct {
				CloneURL string `json:"clone_url"`
				FullName string `json:"full_name"`
			} `json:"repository"`
			HeadCommit struct {
				ID      string `json:"id"`
				Message string `json:"message"`
			} `json:"head_commit"`
			PullRequest *struct {
				Number int    `json:"number"`
				State  string `json:"state"`
				Head   struct {
					Ref string `json:"ref"`
					SHA string `json:"sha"`
				} `json:"head"`
			} `json:"pull_request"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid payload"})
			return
		}

		cloneURL := payload.Repository.CloneURL
		if cloneURL == "" {
			cloneURL = "https://github.com/" + payload.Repository.FullName + ".git"
		}

		// Handle PR events for preview environments
		if (ghEvent == "pull_request" || payload.PullRequest != nil) && s != nil && cloneURL != "" {
			pr := payload.PullRequest
			if pr != nil {
				apps, err := s.ListAppsByGitRepo(r.Context(), cloneURL)
				if err == nil {
					previewMgr := preview.New(s, nc, nil)
					for _, app := range apps {
						if !app.PreviewEnvironments {
							continue
						}
						switch payload.Action {
						case "opened", "synchronize", "reopened":
							_, _ = previewMgr.Create(r.Context(), &app, pr.Head.Ref, pr.Number)
						case "closed":
							existing, _ := previewMgr.FindByPR(r.Context(), app.ID, pr.Number)
							if existing != nil {
								_ = previewMgr.Destroy(r.Context(), existing.ID)
							}
						}
					}
				}
				writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "event": "pull_request"})
				return
			}
		}

		branch := parseBranchFromRef(payload.Ref)

		queued := 0
		if s != nil && cloneURL != "" {
			apps, err := s.ListAppsByGitRepo(r.Context(), cloneURL)
			if err == nil {
				for _, app := range apps {
					autoBranch := app.AutoDeployBranch
					if autoBranch == "" {
						autoBranch = "main"
					}
					if branch == autoBranch {
						deployment := &store.Deployment{AppID: app.ID, Status: "building", CommitSHA: payload.HeadCommit.ID}
						if err := s.CreateDeployment(r.Context(), deployment); err != nil {
							continue
						}
						if err := s.UpdateAppStatus(r.Context(), app.ID, "deploying"); err != nil {
							log.Printf("failed to update app status: %v", err)
						}
						job, _ := json.Marshal(map[string]string{
							"action":        "deploy",
							"app_id":        app.ID,
							"deployment_id": deployment.ID,
							"deploy_type":   "git",
							"image":         "",
							"git_repo":      app.GitRepo,
							"git_branch":    branch,
							"dockerfile":    app.DockerfilePath,
							"name":          app.Name,
							"domain":        app.Domain,
						})
						nc.Publish("hive.build", job)
						queued++
					}
				}
			}
		}

		if queued == 0 && s == nil {
			job, _ := json.Marshal(map[string]string{
				"action": "webhook", "source_id": sourceID, "repo": cloneURL, "ref": payload.Ref, "commit": payload.HeadCommit.ID,
			})
			nc.Publish("hive.build", job)
			queued = 1
		}

		writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "queued": strconv.Itoa(queued)})
	}
}

func parseBranchFromRef(ref string) string {
	if strings.HasPrefix(ref, "refs/heads/") {
		return ref[11:]
	}
	return ref
}

func verifyGitHubSignature(payload []byte, signature, secret string) bool {
	if secret == "" {
		return true
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}
