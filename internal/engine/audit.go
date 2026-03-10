package engine

import (
	"net/http"

	"github.com/lholliger/hive/internal/store"
)

// auditLog records an audit event. Failures are logged but never break the request.
func (s *Server) auditLog(r *http.Request, action, resource, resourceID, details string) {
	user := UserFromContext(r.Context())
	if user == nil {
		return
	}
	al := &store.AuditLog{
		UserID:     user.UserID,
		OrgID:      user.OrgID,
		Action:     action,
		Resource:   resource,
		ResourceID: resourceID,
		Details:    details,
	}
	if err := s.store.CreateAuditLog(r.Context(), al); err != nil {
		s.log.Warnw("audit log failed", "error", err, "action", action, "resource", resource)
	}
}
