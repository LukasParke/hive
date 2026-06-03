package common

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	apicxt "github.com/luke/hive/control-plane/internal/api/ctx"
	"github.com/luke/hive/control-plane/internal/rbac"
)

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, map[string]string{"error": code, "message": message})
}

func RandomToken(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func SHA256Hex(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func ToUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, err
	}
	return u, nil
}

func RequireOrgAccess(w http.ResponseWriter, r *http.Request, pool *pgxpool.Pool, roles ...rbac.Role) (string, bool) {
	claims, ok := apicxt.ClaimsFromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid authentication")
		return "", false
	}
	orgID := strings.TrimSpace(r.Header.Get("X-Organization-Id"))
	if orgID == "" {
		WriteError(w, http.StatusBadRequest, "bad_request", "missing X-Organization-Id header")
		return "", false
	}
	if err := rbac.Require(pool, orgID, claims.UserID, roles...); err != nil {
		WriteError(w, http.StatusForbidden, "forbidden", "insufficient permissions")
		return "", false
	}
	return orgID, true
}
