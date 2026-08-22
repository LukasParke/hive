package common

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	apicxt "github.com/luke/hive/control-plane/internal/api/ctx"
	"github.com/luke/hive/control-plane/internal/rbac"
)

// WriteJSON serializes v as JSON with the given HTTP status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes a JSON error body with the given status and code.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, map[string]string{"error": code, "message": message})
}

// RandomToken returns a hex-encoded crypto/rand token of size bytes.
func RandomToken(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// SHA256Hex returns the hex-encoded SHA-256 digest of raw.
func SHA256Hex(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// ToUUID parses s into a pgtype.UUID.
func ToUUID(s string) (pgtype.UUID, error) {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}, err
	}
	return u, nil
}

// ResolveOrgID determines the acting organization from the X-Organization-Id
// header, falling back to the user's sole membership.
func ResolveOrgID(w http.ResponseWriter, r *http.Request, pool *pgxpool.Pool, userID string) (string, bool) {
	orgID := strings.TrimSpace(r.Header.Get("X-Organization-Id"))
	if orgID != "" {
		return orgID, true
	}

	rows, err := queryOrgIDs(r.Context(), pool, userID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to resolve organization")
		return "", false
	}
	defer rows.Close()

	var orgIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			WriteError(w, http.StatusInternalServerError, "internal_error", "failed to resolve organization")
			return "", false
		}
		orgIDs = append(orgIDs, id)
	}
	if len(orgIDs) == 1 {
		return orgIDs[0], true
	}
	if len(orgIDs) == 0 {
		WriteError(w, http.StatusBadRequest, "bad_request", "missing X-Organization-Id header and user has no organizations")
		return "", false
	}
	WriteError(w, http.StatusBadRequest, "bad_request", "missing X-Organization-Id header")
	return "", false
}

// queryOrgIDs fetches the user's organization ids, most recent first. It is a
// package-level variable so tests can inject row-scanning failures.
var queryOrgIDs = func(ctx context.Context, p *pgxpool.Pool, userID string) (pgx.Rows, error) {
	return p.Query(ctx, `
		select organization_id::text
		from organization_members
		where user_id = $1::uuid
		order by created_at desc
		limit 2
	`, userID)
}

// RequireOrgAccess resolves the acting organization and verifies the caller
// holds one of the allowed roles in it.
func RequireOrgAccess(w http.ResponseWriter, r *http.Request, pool *pgxpool.Pool, roles ...rbac.Role) (string, bool) {
	claims, ok := apicxt.ClaimsFromContext(r.Context())
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauthorized", "missing or invalid authentication")
		return "", false
	}
	orgID, ok := ResolveOrgID(w, r, pool, claims.UserID)
	if !ok {
		return "", false
	}
	if err := rbac.Require(pool, orgID, claims.UserID, roles...); err != nil {
		WriteError(w, http.StatusForbidden, "forbidden", "insufficient permissions")
		return "", false
	}
	return orgID, true
}
