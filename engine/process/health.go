package process

import (
	"context"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze/internal/health"
)

// CheckHealth implements engine.HealthChecker: one probe pass for the periodic
// liveness poll, delegating to the shared core probes. No probe — or a
// one-shot log readiness signal that can't be re-run — is healthy as long as
// the process is up (the runtime only probes running instances).
func (Driver) CheckHealth(ctx context.Context, inst engine.Instance) error {
	cfg, ok := inst.Spec.(*Config)
	if !ok {
		return nil
	}
	spec := cfg.readySpec(cfg.listenPort(inst))
	if spec == nil || spec.Kind == "log_line" {
		return nil
	}
	return health.Probe(ctx, spec, nil)
}
