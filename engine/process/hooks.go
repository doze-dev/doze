package process

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/nerdmenot/doze-sdk/engine"
)

// PreStart implements engine.Hooked: run the pre_start commands after dependencies
// are up but before the app spawns (e.g. database migrations). A failure aborts
// the boot.
func (d Driver) PreStart(ctx context.Context, inst engine.Instance) error {
	return d.runHooks(ctx, inst, "pre_start")
}

// PostStart implements engine.Hooked: run after the app becomes ready.
func (d Driver) PostStart(ctx context.Context, inst engine.Instance) error {
	return d.runHooks(ctx, inst, "post_start")
}

// PreStop implements engine.Hooked: run before the app is signalled to stop.
func (d Driver) PreStop(ctx context.Context, inst engine.Instance) error {
	return d.runHooks(ctx, inst, "pre_stop")
}

// runHooks executes one phase's commands sequentially via `sh -c`, in the
// instance's cwd and with its full merged environment. The first non-zero exit
// stops the sequence and returns an error.
func (d Driver) runHooks(ctx context.Context, inst engine.Instance, phase string) error {
	cfg, ok := inst.Spec.(*Config)
	if !ok {
		return nil
	}
	var cmds []string
	switch phase {
	case "pre_start":
		cmds = cfg.Hooks.PreStart
	case "post_start":
		cmds = cfg.Hooks.PostStart
	case "pre_stop":
		cmds = cfg.Hooks.PreStop
	}
	env := cfg.mergedEnv()
	for _, line := range cmds {
		if strings.TrimSpace(line) == "" {
			continue
		}
		cmd := exec.CommandContext(ctx, "sh", "-c", line)
		cmd.Dir = cfg.Cwd
		cmd.Env = env
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s hook %q failed: %w\n%s", phase, line, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}
