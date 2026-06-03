package organizations

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/luke/hive/control-plane/internal/api/common"
	apimiddleware "github.com/luke/hive/control-plane/internal/api/middleware"
)

type Handler struct {
	Pool *pgxpool.Pool
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{Pool: pool}
}

func (h *Handler) ListOrganizations(w http.ResponseWriter, r *http.Request) {
	claims, ok := apimiddleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	rows, err := h.Pool.Query(r.Context(), `
		select o.id::text, o.name, o.slug, om.role::text
		from organizations o
		join organization_members om on om.organization_id = o.id
		where om.user_id = $1::uuid
		order by o.created_at desc
	`, claims.UserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	type item struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Slug string `json:"slug"`
		Role string `json:"role"`
	}
	var out []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.ID, &it.Name, &it.Slug, &it.Role); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		out = append(out, it)
	}
	common.WriteJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (h *Handler) CreateOrganization(w http.ResponseWriter, r *http.Request) {
	claims, ok := apimiddleware.ClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, `{"message":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	var req struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" || req.Slug == "" {
		http.Error(w, `{"message":"invalid payload"}`, http.StatusBadRequest)
		return
	}
	tx, err := h.Pool.Begin(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer tx.Rollback(r.Context())

	var orgID string
	if err := tx.QueryRow(r.Context(), `
		insert into organizations(name, slug) values ($1, $2) returning id::text
	`, req.Name, req.Slug).Scan(&orgID); err != nil {
		// UX stabilization: if the org already exists and user is already a member, return it.
		var existingID, existingName, existingSlug string
		existingErr := tx.QueryRow(r.Context(), `
			select o.id::text, o.name, o.slug
			from organizations o
			join organization_members m on m.organization_id = o.id
			where m.user_id = $1::uuid
			  and (o.slug = $2 or o.name = $3)
			limit 1
		`, claims.UserID, req.Slug, req.Name).Scan(&existingID, &existingName, &existingSlug)
		if existingErr == nil {
			if err := tx.Commit(r.Context()); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			common.WriteJSON(w, http.StatusOK, map[string]string{
				"id":   existingID,
				"name": existingName,
				"slug": existingSlug,
			})
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if _, err := tx.Exec(r.Context(), `
		insert into organization_members(organization_id, user_id, role) values ($1::uuid, $2::uuid, 'owner')
	`, orgID, claims.UserID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	common.WriteJSON(w, http.StatusCreated, map[string]string{"id": orgID, "name": req.Name, "slug": req.Slug})
}
