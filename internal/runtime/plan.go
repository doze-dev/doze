package runtime

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/nerdmenot/doze/internal/engine"
	"github.com/nerdmenot/doze/internal/health"
	"github.com/nerdmenot/doze/internal/supervisor"
)

// executePlan runs a driver's SpawnPlan and returns a supervised process: it
// starts each spec in dependency order, gates each on its readiness probe, runs any
// post-ready hooks, and (for a multi-spec plan) wraps them as one Composite unit.
// On any failure it tears down whatever it already started. The returned Process is
// owned and supervised by the caller exactly like a legacy Spawn result, so the
// hardened restart/reap path is unchanged.
func (r *Runtime) executePlan(ctx context.Context, plan engine.SpawnPlan) (engine.Process, error) {
	order, err := orderSpecs(plan.Specs)
	if err != nil {
		return nil, err
	}
	started := make([]*supervisor.Process, 0, len(order))
	stopAll := func() {
		for i := len(started) - 1; i >= 0; i-- {
			_ = started[i].Stop(context.Background())
		}
	}

	for _, spec := range order {
		cmd := exec.Command(spec.Bin, spec.Args...)
		cmd.Dir = spec.Dir
		cmd.Env = spec.Env
		start := supervisor.Start
		if spec.Tree {
			start = supervisor.StartTree
		}
		p, err := start(cmd)
		if err != nil {
			stopAll()
			return nil, fmt.Errorf("starting %q: %w", spec.Name, err)
		}
		started = append(started, p)

		if err := health.WaitReady(ctx, spec.Ready, p.Alive, p.Logs); err != nil {
			stopAll()
			return nil, fmt.Errorf("%s not ready: %w\n%s", spec.Name, err, strings.Join(p.Logs(), "\n"))
		}
		for _, hook := range spec.Hooks {
			hc := exec.CommandContext(ctx, "sh", "-c", hook)
			hc.Dir, hc.Env = spec.Dir, spec.Env
			if out, hErr := hc.CombinedOutput(); hErr != nil {
				stopAll()
				return nil, fmt.Errorf("%s hook %q: %w: %s", spec.Name, hook, hErr, strings.TrimSpace(string(out)))
			}
		}
	}

	switch len(started) {
	case 0:
		return nil, fmt.Errorf("spawn plan has no specs")
	case 1:
		return started[0], nil // single spec keeps full Process features (incl. LogsSince)
	default:
		return supervisor.NewComposite(started), nil
	}
}

// orderSpecs returns the specs in dependency order (each after the specs it lists
// in After), erroring on an unknown reference or a cycle.
func orderSpecs(specs []engine.SpawnSpec) ([]engine.SpawnSpec, error) {
	byName := make(map[string]engine.SpawnSpec, len(specs))
	for _, s := range specs {
		byName[s.Name] = s
	}
	var order []engine.SpawnSpec
	state := map[string]int{} // 0 unvisited, 1 on-stack, 2 done
	var visit func(name string) error
	visit = func(name string) error {
		s, ok := byName[name]
		if !ok {
			return fmt.Errorf("spawn spec depends on unknown spec %q", name)
		}
		switch state[name] {
		case 1:
			return fmt.Errorf("spawn spec dependency cycle at %q", name)
		case 2:
			return nil
		}
		state[name] = 1
		for _, dep := range s.After {
			if err := visit(dep); err != nil {
				return err
			}
		}
		state[name] = 2
		order = append(order, s)
		return nil
	}
	for _, s := range specs {
		if err := visit(s.Name); err != nil {
			return nil, err
		}
	}
	return order, nil
}
