package process

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/doze-dev/doze-sdk/engine"
)

// PreStart implements engine.Hooked: run the `setup` commands after dependencies
// are up but before the app spawns (e.g. database migrations). A failure aborts
// the boot.
func (d Driver) PreStart(ctx context.Context, inst engine.Instance) error {
	return d.runSteps(ctx, inst, "setup")
}

// PostStart implements engine.Hooked: run `on_ready` after the app becomes ready.
func (d Driver) PostStart(ctx context.Context, inst engine.Instance) error {
	return d.runSteps(ctx, inst, "on_ready")
}

// PreStop implements engine.Hooked: run `stop` before the app is signalled.
func (d Driver) PreStop(ctx context.Context, inst engine.Instance) error {
	return d.runSteps(ctx, inst, "stop")
}

// runSteps executes one attribute's commands sequentially via `sh -c`, in the
// app's dir and with its full merged environment. The first non-zero exit
// stops the sequence and returns an error.
func (d Driver) runSteps(ctx context.Context, inst engine.Instance, attr string) error {
	cfg, ok := inst.Spec.(*Config)
	if !ok {
		return nil
	}
	var cmds []string
	switch attr {
	case "setup":
		cmds = cfg.Setup
	case "on_ready":
		cmds = cfg.OnReady
	case "stop":
		cmds = cfg.Stop
	}
	env := cfg.envFor(inst)
	for _, line := range cmds {
		if strings.TrimSpace(line) == "" {
			continue
		}
		cmd := exec.CommandContext(ctx, "sh", "-c", line)
		cmd.Dir = cfg.Dir
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s step %q failed: %w\n%s", attr, line, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}
