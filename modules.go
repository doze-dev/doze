package doze

import (
	"context"
	"fmt"

	"github.com/doze-dev/doze/internal/hostboot"
	"github.com/doze-dev/doze/internal/modules"
)

// ModuleSet is the module-toolchain view of a stack: which engine modules the
// config uses, what versions they're pinned to, and moving those pins forward.
// It backs `doze modules`. Daemon-less. Close it when done.
type ModuleSet struct {
	host *hostboot.Host
	mgr  *modules.Manager
	// declaredTypes is the set of engine types the config declares (module +
	// built-in; List filters to the ones the manager recognizes as modules).
	declaredTypes []string
}

// ModuleInfo is one module engine the config uses.
type ModuleInfo struct {
	Engine  string // engine type ("postgres", "kafka", …)
	Version string // pinned version, "" if not pinned
	Source  string // module source address ("doze/postgres")
	Pinned  bool   // whether a pin exists in doze.lock
}

// Modules loads the config's module toolchain WITHOUT starting a daemon. The
// module manager is configured during config decode, so pins are readable
// immediately.
func Modules(opts Options) (*ModuleSet, error) {
	cfg, host, _, err := loadHostAndConfig(opts)
	if err != nil {
		return nil, err
	}
	mgr := host.Modules()
	if mgr == nil {
		host.Close()
		return nil, fmt.Errorf("doze: modules are disabled in this environment")
	}
	seen := map[string]bool{}
	var types []string
	for _, d := range cfg.Instances {
		if !seen[d.Type] {
			seen[d.Type] = true
			types = append(types, d.Type)
		}
	}
	return &ModuleSet{host: host, mgr: mgr, declaredTypes: types}, nil
}

// Close releases the engine host.
func (ms *ModuleSet) Close() error { return ms.host.Close() }

// List returns the module engines the config uses, with their pinned versions.
// Built-in engines (process) that aren't modules are skipped.
func (ms *ModuleSet) List() []ModuleInfo {
	var out []ModuleInfo
	for _, t := range ms.declaredTypes {
		pin, source, ok := ms.mgr.Pinned(t)
		if !ok {
			continue // not a module engine (e.g. the built-in process)
		}
		out = append(out, ModuleInfo{Engine: t, Version: pin.Version, Source: source, Pinned: true})
	}
	return out
}

// Upgrade moves an engine's pin to the newest compatible release, returning the
// old and new versions and whether anything changed.
func (ms *ModuleSet) Upgrade(ctx context.Context, engine string) (from, to string, changed bool, err error) {
	return ms.mgr.Upgrade(ctx, engine)
}
