// Package hostboot wires the process-global engine host: the module manager,
// the plugin manager, the engine plugin resolver, and the six config.Set* hooks
// that let declared engine blocks be decoded and validated by their pinned
// modules. Both entry points into a doze process share it — the `doze` CLI
// (cmd/doze) and the embeddable facade (root package `doze`) — so the wiring
// lives here exactly once and can't drift between them.
//
// The config.Set* hooks are process-global, so at most one Host may be active
// per process. Init is ref-counted: repeated Init with the SAME Home returns the
// same Host (incrementing the count); a DIFFERENT Home while one is active is an
// error. Close decrements; the plugins are reaped when the count reaches zero.
package hostboot

import (
	"fmt"
	"os"
	"sync"

	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze-sdk/plugin"
	"github.com/doze-dev/doze/engine/process"
	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/modules"
)

// Options configures a Host.
type Options struct {
	// Home is the shared cache root (binaries + fetched modules) — DOZE_HOME or
	// ~/.doze. Required.
	Home string
	// Logf receives engine/convergence progress. Required (used for module +
	// process logging); nil defaults to a no-op.
	Logf func(string, ...any)
	// LockPath returns where module pins are recorded (doze.lock). It is resolved
	// lazily because the config path isn't known until a command runs. Required.
	LockPath func() string
	// PersistLock reports whether the current operation may write module pins to
	// doze.lock — read commands leave it byte-identical. Required.
	PersistLock func() bool
	// Warnf reports a non-fatal setup warning (modules disabled). nil prints to
	// stderr in the CLI's historical format.
	Warnf func(string, ...any)
}

// Host owns the process-global engine wiring. Close reaps the plugin processes.
type Host struct {
	home      string
	pluginMgr *plugin.Manager
	modMgr    *modules.Manager // nil if modules couldn't be initialized
	resolver  plugin.Resolver  // the resolver chain: DOZE_<TYPE>_PLUGIN, then modules
}

// ResolvePlugin resolves an engine type to its plugin binary path (and any extra
// env), via the same chain the engine uses — the DOZE_<TYPE>_PLUGIN override
// first, then the fetched-from-doze-modules cache. Used to run a plugin's
// __describe. Returns ok=false when no plugin provides the engine.
func (h *Host) ResolvePlugin(engineType string) (path string, env []string, ok bool) {
	if h.resolver == nil {
		return "", nil, false
	}
	return h.resolver(engineType)
}

var (
	mu       sync.Mutex
	current  *Host
	refcount int
)

// Init builds (or, when already active for the same Home, re-attaches to) the
// process-global Host. Call Close once per successful Init.
func Init(opts Options) (*Host, error) {
	if opts.Home == "" {
		return nil, fmt.Errorf("hostboot: Home is required")
	}
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	if opts.LockPath == nil || opts.PersistLock == nil {
		return nil, fmt.Errorf("hostboot: LockPath and PersistLock are required")
	}

	mu.Lock()
	defer mu.Unlock()
	if current != nil {
		if current.home != opts.Home {
			return nil, fmt.Errorf("hostboot: already initialized for home %q; cannot re-init for %q in the same process", current.home, opts.Home)
		}
		refcount++
		return current, nil
	}

	// Surface engine convergence warnings through the caller's logger. Importing
	// engine/process (transitively engine/*) registers the built-in drivers.
	process.Logf = opts.Logf

	// Out-of-process engine modules: resolve a plugin binary (local
	// DOZE_<TYPE>_PLUGIN override first, then a fetched-from-doze-modules module),
	// keep it warm for config eval + boot, and reap it when the host closes.
	resolvers := []plugin.Resolver{plugin.EnvResolver()}
	var modMgr *modules.Manager
	if mgr, err := modules.NewManager(opts.Home); err != nil {
		warnf(opts, "doze: modules disabled: %v", err)
	} else {
		modMgr = mgr
		modMgr.SetLogger(opts.Logf)
		// LockPath returns the full doze.lock path (dir + LockFileName), resolved
		// lazily since the config path isn't known until a command runs.
		modMgr.UseLock(opts.LockPath)
		modMgr.PersistWhen(opts.PersistLock)
		config.SetModulesConfigurer(func(mc config.ModulesConfig) {
			modMgr.Configure(mc.Mirror, mc.Enabled, mc.Sources, mc.Versions)
		})
		config.SetEngineRequirer(modMgr.Require)
		config.SetModuleSupportChecker(modMgr.CheckSupport)
		config.SetLookupErrorReporter(modMgr.LastError)
		config.SetEngineNamesProvider(modMgr.KnownTypes)
		config.SetRemoteDecodeHint(func(engineType string) string {
			pin, source, ok := modMgr.Pinned(engineType)
			if !ok {
				return ""
			}
			hint := fmt.Sprintf("this block is decoded by module %s %s (pinned in doze.lock)", source, pin.Version)
			if up := modMgr.UpgradeHint(engineType); up != "" {
				hint += "; " + up
			}
			return hint
		})
		resolvers = append(resolvers, modMgr.Lookup)
	}
	chain := plugin.Chain(resolvers...)
	pluginMgr := plugin.NewManager(chain)
	engine.SetPluginResolver(pluginMgr.Lookup)

	current = &Host{home: opts.Home, pluginMgr: pluginMgr, modMgr: modMgr, resolver: chain}
	refcount = 1
	return current, nil
}

// Modules returns the module manager, or nil when modules are disabled.
func (h *Host) Modules() *modules.Manager { return h.modMgr }

// Close decrements the ref-count; at zero it reaps the plugin processes and
// clears the process-global wiring so a later Init can start fresh.
func (h *Host) Close() error {
	mu.Lock()
	defer mu.Unlock()
	if current == nil {
		return nil
	}
	refcount--
	if refcount > 0 {
		return nil
	}
	h.pluginMgr.Close()
	current = nil
	refcount = 0
	return nil
}

func warnf(opts Options, format string, args ...any) {
	if opts.Warnf != nil {
		opts.Warnf(format, args...)
		return
	}
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}
