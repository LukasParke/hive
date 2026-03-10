package engine

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/lholliger/hive/internal/store"
)

var errForbiddenOrgAccess = errors.New("forbidden")

func (s *Server) requireProjectAccess(ctx context.Context, projectID, orgID string) (*store.Project, error) {
	p, err := s.store.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if p == nil || p.OrgID != orgID {
		return nil, errForbiddenOrgAccess
	}
	return p, nil
}

func (s *Server) requireAppAccess(ctx context.Context, appID, orgID string) (*store.App, error) {
	a, err := s.store.GetApp(ctx, appID)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, sql.ErrNoRows
	}
	p, err := s.requireProjectAccess(ctx, a.ProjectID, orgID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, fmt.Errorf("app project not found")
	}
	return a, nil
}

func (s *Server) requireStackAccess(ctx context.Context, stackID, orgID string) (*store.Stack, error) {
	st, err := s.store.GetStack(ctx, stackID)
	if err != nil {
		return nil, err
	}
	if st == nil {
		return nil, sql.ErrNoRows
	}
	if _, err := s.requireProjectAccess(ctx, st.ProjectID, orgID); err != nil {
		return nil, err
	}
	return st, nil
}

func (s *Server) requireDNSProviderAccess(ctx context.Context, providerID, orgID string) (*store.DNSProvider, error) {
	p, err := s.store.GetDNSProvider(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if p == nil || p.OrgID != orgID {
		return nil, errForbiddenOrgAccess
	}
	return p, nil
}

func (s *Server) requireDNSRecordAccess(ctx context.Context, recordID, orgID string) (*store.DNSRecord, *store.DNSProvider, error) {
	rec, err := s.store.GetDNSRecord(ctx, recordID)
	if err != nil {
		return nil, nil, err
	}
	provider, err := s.requireDNSProviderAccess(ctx, rec.ProviderID, orgID)
	if err != nil {
		return nil, nil, err
	}
	return rec, provider, nil
}
