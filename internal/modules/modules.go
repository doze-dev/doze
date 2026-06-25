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
	"strings"
	"sync"

	"github.com/nerdmenot/doze/internal/binaries"
	"github.com/nerdmenot/doze/internal/engine"
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

	mu     sync.Mutex
	misses map[string]bool // engine types with no published module (negative cache)
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
	return &Manager{bin: bm, plat: plat, misses: map[string]bool{}}, nil
}

// SetLogger installs a progress logger for downloads.
func (m *Manager) SetLogger(f func(string, ...any)) { m.bin.Logf = f }

// Resolve fetches (or finds cached) the plugin binary for module name at the
// given version spec ("" = the default channel) and returns its executable path.
func (m *Manager) Resolve(ctx context.Context, name, version string) (string, error) {
	if version == "" {
		version = DefaultVersion
	}
	full, err := m.bin.ResolveMajor(name, version)
	if err != nil {
		return "", err
	}
	binDir, _, err := m.bin.Ensure(ctx, name, full, m.plat, "")
	if err != nil {
		return "", err
	}
	// Convention: the archive ships the plugin as bin/<name>-plugin.
	exe := filepath.Join(binDir, name+"-plugin")
	if fi, err := os.Stat(exe); err == nil && !fi.IsDir() {
		return exe, nil
	}
	if p := firstExecutable(binDir); p != "" {
		return p, nil
	}
	return "", fmt.Errorf("module %s %s has no plugin executable", name, full)
}

// Lookup adapts Resolve to the plugin resolver contract: engine type -> module of
// the same name. A type with no published module is remembered so it isn't
// re-fetched every lookup, and the caller falls back to any in-tree driver.
func (m *Manager) Lookup(engineType string) (path string, env []string, ok bool) {
	m.mu.Lock()
	missed := m.misses[engineType]
	m.mu.Unlock()
	if missed {
		return "", nil, false
	}
	p, err := m.Resolve(context.Background(), engineType, "")
	if err != nil {
		m.mu.Lock()
		m.misses[engineType] = true
		m.mu.Unlock()
		return "", nil, false
	}
	return p, nil, true
}

// Enabled reports whether module fetching is active. Until doze-modules is
// published it is opt-in via DOZE_MODULES_MIRROR, so the public (not-yet-existing)
// default mirror doesn't add a failed round-trip per engine type.
func Enabled() bool { return os.Getenv("DOZE_MODULES_MIRROR") != "" }

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
