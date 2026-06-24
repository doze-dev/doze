package runtime

import (
	"context"
	"fmt"

	"github.com/nerdmenot/doze/internal/engine"
	"github.com/nerdmenot/doze/internal/state"
)

// Apply converges the named instance (or all, when name is "") to its declared
// state and prunes any previously-applied objects no longer declared, recording
// the result in the state file. The backends are left running (the daemon holds
// them; a standalone caller stops them afterward).
func (r *Runtime) Apply(ctx context.Context, name string) error {
	unlock, err := state.Lock(r.statePath)
	if err != nil {
		return err
	}
	defer unlock()
	st, err := state.Load(r.statePath)
	if err != nil {
		return err
	}
	for _, n := range r.targetNames(name) {
		if !r.hasStructure(n) {
			continue // no structure to apply; it boots lazily or via `doze start <instance>`
		}
		if err := r.Up(ctx, n); err != nil { // boot + converge (creates/updates)
			return err
		}
		if err := r.reconcileObjects(ctx, n, st); err != nil {
			return err
		}
	}
	st.Outputs = r.outputs()
	return st.Save(r.statePath)
}

// hasStructure reports whether an instance's engine manages structural objects
// (implements engine.Inventory) — the ones apply/destroy act on. Structureless
// engines (Valkey, Kvrocks, DocumentDB) boot lazily and are skipped by apply.
func (r *Runtime) hasStructure(name string) bool {
	decl := r.cfg.Lookup(name)
	if decl == nil {
		return false
	}
	drv, ok := engine.Lookup(decl.Type)
	if !ok {
		return false
	}
	_, ok = drv.(engine.Inventory)
	return ok
}

// reconcileObjects records the instance's desired objects in st and prunes the
// ones it previously applied but no longer declares. The instance must already be
// booted (Apply calls Up first).
func (r *Runtime) reconcileObjects(ctx context.Context, name string, st *state.State) error {
	decl := r.cfg.Lookup(name)
	if decl == nil {
		return fmt.Errorf("instance %q is not declared", name)
	}
	drv, ok := engine.Lookup(decl.Type)
	if !ok {
		return fmt.Errorf("no driver for engine %q", decl.Type)
	}
	inv, ok := drv.(engine.Inventory)
	if !ok {
		return nil // engine has no structural objects to track
	}
	inst := r.instanceFor(decl, drv)
	desired := inv.Objects(inst)
	removed := state.Removed(st.Objects(name), desired)
	if pr, ok := drv.(engine.Pruner); ok && len(removed) > 0 {
		tc, err := drv.Resolve(ctx, decl.Version, r.plat, r.lock, r.mgr)
		if err != nil {
			return err
		}
		if err := pr.Prune(ctx, inst, tc, inst.Endpoint, removed); err != nil {
			return err
		}
	}
	st.Set(name, desired)
	return nil
}

// Destroy prunes every previously-applied object for the named instance (or all,
// when name is ""), clearing them from the state file. It boots each instance as
// needed to run the drops, then stops it.
func (r *Runtime) Destroy(ctx context.Context, name string) error {
	unlock, err := state.Lock(r.statePath)
	if err != nil {
		return err
	}
	defer unlock()
	st, err := state.Load(r.statePath)
	if err != nil {
		return err
	}
	var targets []string
	if name != "" {
		targets = []string{name}
	} else {
		for n := range st.Instances {
			targets = append(targets, n)
		}
	}
	for _, n := range targets {
		prior := st.Objects(n)
		if len(prior) == 0 {
			continue
		}
		if err := r.pruneAll(ctx, n, prior); err != nil {
			return err
		}
		st.Set(n, nil)
	}
	if name == "" {
		st.Outputs = nil
	}
	return st.Save(r.statePath)
}

// pruneAll boots the instance and drops all of prior (in reverse/drop order),
// then stops it.
func (r *Runtime) pruneAll(ctx context.Context, name string, prior []engine.Object) error {
	decl := r.cfg.Lookup(name)
	if decl == nil {
		// Instance removed from config but still in state: nothing to boot/drop
		// against. Clear it from state (handled by the caller).
		return nil
	}
	drv, ok := engine.Lookup(decl.Type)
	if !ok {
		return nil
	}
	pr, ok := drv.(engine.Pruner)
	if !ok {
		return nil
	}
	if _, err := r.Boot(ctx, name); err != nil {
		return err
	}
	defer func() { _ = r.Stop(ctx, name) }()
	tc, err := drv.Resolve(ctx, decl.Version, r.plat, r.lock, r.mgr)
	if err != nil {
		return err
	}
	inst := r.instanceFor(decl, drv)
	// prior is stored in create order; drop in reverse.
	return pr.Prune(ctx, inst, tc, inst.Endpoint, state.Reverse(prior))
}

// targetNames returns the requested instance, or all declared instances.
func (r *Runtime) targetNames(name string) []string {
	if name != "" {
		return []string{name}
	}
	names := make([]string, 0, len(r.cfg.Instances))
	for _, decl := range r.cfg.Instances {
		names = append(names, decl.Name)
	}
	return names
}

// outputs renders the config's declared outputs into a name→value map for state.
func (r *Runtime) outputs() map[string]string {
	if len(r.cfg.Outputs) == 0 {
		return nil
	}
	out := make(map[string]string, len(r.cfg.Outputs))
	for name, o := range r.cfg.Outputs {
		out[name] = o.Value
	}
	return out
}
