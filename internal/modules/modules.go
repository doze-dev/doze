// Package modules fetches out-of-process engine plugins ("modules") from the
// doze-modules monorepo's per-module releases, reusing the same download / verify
// / content-addressed cache machinery as engine binaries (internal/binaries). A
// module is one plugin executable; doze resolves an engine type to a module
// name@version, fetches the binary for the host platform, caches it under
// ~/.doze/modules, and launches it like any plugin.
package modules

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/nerdmenot/doze-sdk/binaries"
	"github.com/nerdmenot/doze-sdk/engine"
)

// DefaultModuleRoot is the doze-modules release base each module's index.yaml +
// archives are served under (<root>/<name>/…), mirroring the doze-binaries layout.
const DefaultModuleRoot = "https://github.com/NerdMeNot/doze-modules/releases/download"

// DefaultVersion is the index channel used when a module isn't otherwise pinned —
// the index maps it to a full version (versions: { default: "0.1.0" }).
const DefaultVersion = "default"

// Manager resolves + fetches plugin modules. It wraps a binaries.Manager pointed
// at the modules mirror (so all the verify/cache/atomic-install logic is shared)
// and remembers which engine types have no module so a miss costs one lookup.
type Manager struct {
	bin  *binaries.Manager
	plat engine.Platform

	lockPath func() string // resolves the project doze.lock path (lazily), for pinning

	mu       sync.Mutex
	enabled  bool              // fetch modules at all (env mirror set, or modules{} enabled)
	versions map[string]string // engine type -> pinned module version (from modules{})
	misses   map[string]bool   // engine types with no published module (negative cache)
}

// NewManager builds a module Manager caching under <home>/modules. The mirror is
// DOZE_MODULES_MIRROR when set (a local/dev mirror, including a file:// path),
// else the public doze-modules releases.
func NewManager(home string) (*Manager, error) {
	plat, err := binaries.HostPlatform()
	if err != nil {
		return nil, err
	}
	root := DefaultModuleRoot
	if v := os.Getenv("DOZE_MODULES_MIRROR"); v != "" {
		root = v
	}
	bm := binaries.NewManager(filepath.Join(home, "modules"))
	bm.MirrorRoot = root
	return &Manager{
		bin:      bm,
		plat:     plat,
		enabled:  os.Getenv("DOZE_MODULES") != "off", // default-on: core ships no backing engines
		versions: map[string]string{},
		misses:   map[string]bool{},
	}, nil
}

// Configure applies a decoded modules{} block: an optional mirror override,
// whether fetching is enabled, and per-engine version pins. It runs before any
// instance's driver is resolved (see config.SetModulesConfigurer).
func (m *Manager) Configure(mirror string, enabled bool, versions map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if mirror != "" {
		m.bin.MirrorRoot = mirror
	}
	if enabled {
		m.enabled = true
	}
	for k, v := range versions {
		if v != "" {
			m.versions[k] = v
		}
	}
}

// SetLogger installs a progress logger for downloads.
func (m *Manager) SetLogger(f func(string, ...any)) { m.bin.Logf = f }

// UseLock makes Resolve pin each fetched module in the project doze.lock (under
// modules:), and verify re-fetches against the locked checksum. lockPath resolves
// the lockfile lazily (the config dir isn't known when the Manager is built).
func (m *Manager) UseLock(lockPath func() string) { m.lockPath = lockPath }

// Resolve fetches (or finds cached) the plugin binary for module name at the
// given version spec ("" = the default channel) and returns its executable path.
// When a lock is configured it freezes the resolved version + checksum and
// verifies subsequent fetches against it (the doze-modules pin layer).
func (m *Manager) Resolve(ctx context.Context, name, version string) (string, error) {
	if version == "" {
		version = DefaultVersion
	}
	lock := m.loadLock()

	// A frozen pin wins: honor its resolved version + checksum so a moving
	// "default" channel can't drift and a tampered archive is rejected.
	if pin, ok := lock.GetModule(name, version); ok && pin.Resolved != "" {
		binDir, _, err := m.bin.Ensure(ctx, name, pin.Resolved, m.plat, pin.Hashes[m.plat.Triple])
		if err != nil {
			return "", err
		}
		return pluginExe(binDir, name, pin.Resolved)
	}

	full, err := m.bin.ResolveMajor(name, version)
	if err != nil {
		return "", err
	}
	binDir, digest, err := m.bin.Ensure(ctx, name, full, m.plat, "")
	if err != nil {
		return "", err
	}
	m.recordPin(lock, name, version, full, digest)
	return pluginExe(binDir, name, full)
}

// pluginExe finds the plugin executable in binDir (convention bin/<name>-plugin).
func pluginExe(binDir, name, full string) (string, error) {
	exe := filepath.Join(binDir, name+"-plugin")
	if fi, err := os.Stat(exe); err == nil && !fi.IsDir() {
		return exe, nil
	}
	if p := firstExecutable(binDir); p != "" {
		return p, nil
	}
	return "", fmt.Errorf("module %s %s has no plugin executable", name, full)
}

// loadLock loads the project doze.lock for module pinning, or a detached lock
// (records dropped) when no lock path is configured.
func (m *Manager) loadLock() *binaries.Lock {
	if m.lockPath == nil {
		return nil
	}
	lock, err := binaries.LoadLock(m.lockPath())
	if err != nil {
		return nil
	}
	return lock
}

// recordPin freezes (name, spec) -> full + this platform's checksum and saves.
func (m *Manager) recordPin(lock *binaries.Lock, name, spec, full, digest string) {
	if lock == nil {
		return
	}
	lock.RecordModule(name, spec, engine.Pin{
		Resolved: full, Source: "module",
		Hashes: map[string]string{m.plat.Triple: digest},
	})
	_ = lock.Save()
}

// Lookup adapts Resolve to the plugin resolver contract: engine type -> module of
// the same name. A type with no published module is remembered so it isn't
// re-fetched every lookup, and the caller falls back to any in-tree driver.
func (m *Manager) Lookup(engineType string) (path string, env []string, ok bool) {
	// Never fetch a module for an engine that's compiled in (process): the in-tree
	// driver is authoritative, and a stray published module must not shadow it.
	if isInTree(engineType) {
		return "", nil, false
	}
	m.mu.Lock()
	if !m.enabled || m.misses[engineType] {
		m.mu.Unlock()
		return "", nil, false
	}
	version := m.versions[engineType]
	m.mu.Unlock()
	p, err := m.Resolve(context.Background(), engineType, version)
	if err != nil {
		m.mu.Lock()
		m.misses[engineType] = true
		m.mu.Unlock()
		return "", nil, false
	}
	return p, nil, true
}

// isInTree reports whether an engine type is compiled into doze core (registered
// in-tree, e.g. process). Such types are never fetched as modules.
func isInTree(engineType string) bool {
	return slices.Contains(engine.Types(), engineType)
}

// Enabled reports whether module fetching is on. It is default-on: core compiles
// in only the process primitive, so every other engine is a module fetched from
// doze-modules. Opt out with DOZE_MODULES=off (offline / process-only); override
// the source with DOZE_MODULES_MIRROR or a modules{} block.
func Enabled() bool { return os.Getenv("DOZE_MODULES") != "off" }

// Mirror returns the configured module mirror base.
func (m *Manager) Mirror() string { return m.bin.MirrorRoot }

// Cached returns the path + version of a cached build of module name for the host
// platform (newest by directory listing), or ok=false if none is cached. It does
// no network — for inspection (`doze modules`).
func (m *Manager) Cached(name string) (path, version string, ok bool) {
	base := filepath.Join(m.bin.Home, name)
	entries, _ := os.ReadDir(base)
	suffix := "-" + m.plat.Triple
	for _, e := range entries {
		if !e.IsDir() || !strings.HasSuffix(e.Name(), suffix) {
			continue
		}
		exe := filepath.Join(base, e.Name(), "bin", name+"-plugin")
		if fi, err := os.Stat(exe); err == nil && !fi.IsDir() {
			return exe, strings.TrimSuffix(e.Name(), suffix), true
		}
	}
	return "", "", false
}

func firstExecutable(dir string) string {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if fi, err := e.Info(); err == nil && fi.Mode()&0o111 != 0 {
			return filepath.Join(dir, e.Name())
		}
	}
	return ""
}
