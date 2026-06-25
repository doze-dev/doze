package process

import (
	"context"

	"github.com/nerdmenot/doze/internal/engine"
	"github.com/nerdmenot/doze/internal/health"
)

// CheckHealth implements engine.HealthChecker: one probe pass for the periodic
// liveness poll, delegating to the shared core probes. No health block — or a
// one-shot log_line readiness signal that can't be re-run — is healthy as long as
// the process is up (the runtime only probes running instances).
func (Driver) CheckHealth(ctx context.Context, inst engine.Instance) error {
	cfg, ok := inst.Spec.(*Config)
	if !ok || cfg.Health == nil || cfg.Health.Kind == "log_line" {
		return nil
	}
	return health.Probe(ctx, cfg.readySpec(), nil)
}
