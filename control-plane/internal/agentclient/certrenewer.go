package agentclient

import (
	"context"
	"fmt"

	"github.com/luke/hive/control-plane/internal/ca"
)

// ControlPlaneCertRenewer implements the riverjobs CertRenewer contract by
// re-issuing the control-plane client certificate whenever the persisted one
// has less than CertRenewalMinValidity (72h) of validity remaining. The
// fresh material is persisted to the secrets store so new connections pick
// it up on next load.
type ControlPlaneCertRenewer struct {
	Authority *ca.Authority
	Store     ca.SecretStore
}

// RenewControlPlaneCert renews the control-plane client certificate.
func (r *ControlPlaneCertRenewer) RenewControlPlaneCert(ctx context.Context) error {
	if r.Authority == nil {
		return fmt.Errorf("cert renewal: no CA authority configured")
	}
	if _, err := LoadOrCreateClientCertWithMinValidity(ctx, r.Authority, r.Store, CertRenewalMinValidity); err != nil {
		return fmt.Errorf("cert renewal: %w", err)
	}
	return nil
}
