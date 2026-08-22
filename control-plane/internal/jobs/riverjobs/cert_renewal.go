package riverjobs

import (
	"context"
	"log/slog"

	"github.com/riverqueue/river"
)

// CertRenewer renews the control-plane client certificate when its
// remaining validity drops below the implementation's threshold (72h).
// It is satisfied by the agentclient/CA helper wired at startup.
type CertRenewer interface {
	RenewControlPlaneCert(ctx context.Context) error
}

// CertRenewalWorker periodically renews the control-plane client cert.
type CertRenewalWorker struct {
	river.WorkerDefaults[CertRenewalJobArgs]
	Renewer CertRenewer // optional; nil skips renewal (e.g. no CA configured)
}

// Work processes a certificate renewal job.
func (w *CertRenewalWorker) Work(ctx context.Context, job *river.Job[CertRenewalJobArgs]) error {
	if w.Renewer == nil {
		slog.DebugContext(ctx, "cert renewal skipped: no renewer configured")
		return nil
	}
	return w.Renewer.RenewControlPlaneCert(ctx)
}
