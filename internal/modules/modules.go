// Package modules fetches out-of-process engine plugins ("modules") from the doze
// module registry, reusing the same download / verify / content-addressed cache
// machinery as engine binaries (doze-sdk/binaries). A module is one plugin
// executable published at a registry source address <namespace>/<name>. doze maps
// an engine type to a source (default doze/<type>, overridable in a modules{}
// block), pins the namespace's ed25519 publisher key on first use, and accepts an
// artifact only if it carries a valid signature from that key — so a public
// registry is signed by default and key rotation can't happen silently.
package modules

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/doze-dev/doze-sdk/binaries"
	"github.com/doze-dev/doze-sdk/engine"
)

// DefaultRegistryBase is the doze registry base every source resolves under:
// <base>/<namespace>/keys.json and <base>/<namespace>/<name>/index.yaml. It is the
// doze-registry site on Cloudflare Pages (which serves the signed discovery layer;
// the index.yaml artifact URLs point at the archive host, e.g. GitHub Releases).
// Override with DOZE_MODULES_MIRROR (a local/dev mirror, including a file:// path)
// or a modules{} block's mirror.
const DefaultRegistryBase = "https://doze.nerdmenot.in/registry"

// DefaultNamespace is the namespace an engine type's source defaults to when no
// explicit source is given: type "postgres" -> "doze/postgres".
const DefaultNamespace = "doze"

// DefaultVersion is the index channel used when a module isn't otherwise pinned —
// the index maps it to a full version (versions: { default: "0.1.0" }).
const DefaultVersion = "default"

// keysDoc is the per-namespace keys.json: the publisher's base64 ed25519 key.
type keysDoc struct {
	Namespace string `json:"namespace"`
	Key       string `json:"key"`
}

// Manager resolves + fetches plugin modules from the signed registry. It builds a
// per-namespace binaries.Manager on demand (sharing all the verify/cache/atomic-
// install logic, scoped to <home>/modules/<namespace> and the namespace's signing
// key) and remembers which engine types have no module so a miss costs one lookup.
type Manager struct {
	home string
	plat engine.Platform

	lockPath func() string // resolves the project doze.lock path (lazily), for pinning
	logf     func(string, ...any)

	mu       sync.Mutex
	enabled  bool              // fetch modules at all
	base     string            // registry base URL (default or override)
	versions map[string]string // engine type -> pinned module version (from modules{})
	sources  map[string]string // engine type -> source address override (from modules{})
	misses   map[string]bool   // engine types with no published module (negative cache)

	nsm  map[string]*binaries.Manager // memoized per-namespace fetchers (keyed by namespace)
	keys map[string]ed25519.PublicKey // verified publisher keys (keyed by namespace)
}

// NewManager builds a module Manager caching under <home>/modules. The registry
// base is DOZE_MODULES_MIRROR when set, else the public doze-registry.
func NewManager(home string) (*Manager, error) {
	plat, err := binaries.HostPlatform()
	if err != nil {
		return nil, err
	}
	base := DefaultRegistryBase
	if v := os.Getenv("DOZE_MODULES_MIRROR"); v != "" {
		base = v
	}
	return &Manager{
		home:     home,
		plat:     plat,
		logf:     func(string, ...any) {},
		enabled:  os.Getenv("DOZE_MODULES") != "off", // default-on: core ships no backing engines
		base:     base,
		versions: map[string]string{},
		sources:  map[string]string{},
		misses:   map[string]bool{},
		nsm:      map[string]*binaries.Manager{},
		keys:     map[string]ed25519.PublicKey{},
	}, nil
}

// Configure applies a decoded modules{} block: an optional registry override,
// whether fetching is enabled, per-engine version pins, and per-engine source
// overrides. It runs before any instance's driver is resolved.
func (m *Manager) Configure(mirror string, enabled bool, versions, sources map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if mirror != "" {
		m.base = mirror
		m.nsm = map[string]*binaries.Manager{} // re-scope namespace fetchers to the new base
		m.keys = map[string]ed25519.PublicKey{}
	}
	if enabled {
		m.enabled = true
	}
	for k, v := range versions {
		if v != "" {
			m.versions[k] = v
		}
	}
	for k, v := range sources {
		if v != "" {
			m.sources[k] = v
		}
	}
}

// SetLogger installs a progress logger for downloads.
func (m *Manager) SetLogger(f func(string, ...any)) { m.mu.Lock(); m.logf = f; m.mu.Unlock() }

// UseLock makes Resolve pin each fetched module + namespace key in the project
// doze.lock, and verify re-fetches against the locked checksum + pinned key.
func (m *Manager) UseLock(lockPath func() string) { m.lockPath = lockPath }

// sourceFor returns the registry source address for an engine type: an explicit
// modules{} override, else doze/<type>.
func (m *Manager) sourceFor(engineType string) string {
	if s := m.sources[engineType]; s != "" {
		return s
	}
	return DefaultNamespace + "/" + engineType
}

// splitSource parses "<namespace>/<name>" into its two non-empty parts.
func splitSource(source string) (ns, name string, err error) {
	ns, name, ok := strings.Cut(source, "/")
	if !ok || ns == "" || name == "" || strings.Contains(name, "/") {
		return "", "", fmt.Errorf("invalid module source %q: want <namespace>/<name>", source)
	}
	return ns, name, nil
}

// Resolve fetches (or finds cached) the plugin binary for engine type at the given
// version spec ("" = the default channel) and returns its executable path. It
// resolves the type's source, pins the namespace publisher key (trust-on-first-
// use), and verifies the artifact's signature against it. When a lock is
// configured it freezes the resolved version + checksum and the namespace key.
func (m *Manager) Resolve(ctx context.Context, engineType, version string) (string, error) {
	if version == "" {
		version = DefaultVersion
	}
	source := m.sourceFor(engineType)
	ns, name, err := splitSource(source)
	if err != nil {
		return "", err
	}
	lock := m.loadLock()

	bm, err := m.nsManager(ns, lock)
	if err != nil {
		return "", err
	}

	// A frozen pin wins: honor its resolved version + checksum so a moving
	// "default" channel can't drift and a tampered archive is rejected.
	if pin, ok := lock.GetModule(source, version); ok && pin.Resolved != "" {
		binDir, _, err := bm.Ensure(ctx, name, pin.Resolved, m.plat, pin.Hashes[m.plat.Triple])
		if err != nil {
			return "", err
		}
		return pluginExe(binDir, name, pin.Resolved)
	}

	full, err := bm.ResolveMajor(name, version)
	if err != nil {
		return "", err
	}
	binDir, digest, err := bm.Ensure(ctx, name, full, m.plat, "")
	if err != nil {
		return "", err
	}
	m.recordPin(lock, source, version, full, digest)
	return pluginExe(binDir, name, full)
}

// nsManager returns the binaries.Manager scoped to a registry namespace: cache
// under <home>/modules/<ns>, mirror at <base>/<ns> (so Ensure(name) fetches
// <base>/<ns>/<name>/index.yaml), and the namespace's verified publisher key as
// its SigningKey. The key is fetched + pinned on first use.
func (m *Manager) nsManager(ns string, lock *binaries.Lock) (*binaries.Manager, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if bm, ok := m.nsm[ns]; ok {
		return bm, nil
	}
	key, err := m.keyForLocked(ns, lock)
	if err != nil {
		return nil, err
	}
	bm := binaries.NewManager(filepath.Join(m.home, "modules", ns))
	bm.MirrorRoot = strings.TrimRight(m.base, "/") + "/" + ns
	bm.SigningKey = key
	bm.Logf = m.logf
	m.nsm[ns] = bm
	return bm, nil
}

// keyForLocked fetches a namespace's publisher key from <base>/<ns>/keys.json and
// applies trust-on-first-use against the lock: a previously pinned key that
// differs is a hard error (possible registry compromise); an unpinned key is
// recorded. Caller holds m.mu.
func (m *Manager) keyForLocked(ns string, lock *binaries.Lock) (ed25519.PublicKey, error) {
	if k, ok := m.keys[ns]; ok {
		return k, nil
	}
	url := strings.TrimRight(m.base, "/") + "/" + ns + "/keys.json"
	// A throwaway fetcher just for keys.json (reuses the file:// + http transport).
	body, err := binaries.NewManager(m.home).Fetch(url)
	if err != nil {
		return nil, fmt.Errorf("fetching publisher key for namespace %q (%s): %w", ns, url, err)
	}
	var doc keysDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", url, err)
	}
	if doc.Key == "" {
		return nil, fmt.Errorf("namespace %q publishes no key in %s", ns, url)
	}
	if pinned, ok := lock.GetKey(ns); ok && pinned != doc.Key {
		return nil, fmt.Errorf("publisher key for namespace %q changed (locked %s…, registry %s…); "+
			"if this rotation is intentional, clear the keys entry for %q in doze.lock",
			ns, short(pinned), short(doc.Key), ns)
	}
	key, err := binaries.ParsePublicKey(doc.Key)
	if err != nil {
		return nil, fmt.Errorf("namespace %q key: %w", ns, err)
	}
	if lock != nil {
		lock.RecordKey(ns, doc.Key)
		_ = lock.Save()
	}
	m.keys[ns] = key
	return key, nil
}

func short(b64 string) string {
	if len(b64) > 12 {
		return b64[:12]
	}
	return b64
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

// loadLock loads the project doze.lock for module pinning, or nil when no lock
// path is configured (records dropped).
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

// recordPin freezes (source, spec) -> full + this platform's checksum and saves.
func (m *Manager) recordPin(lock *binaries.Lock, source, spec, full, digest string) {
	if lock == nil {
		return
	}
	lock.RecordModule(source, spec, engine.Pin{
		Resolved: full, Source: source,
		Hashes: map[string]string{m.plat.Triple: digest},
	})
	_ = lock.Save()
}

// Lookup adapts Resolve to the plugin resolver contract: engine type -> its module
// source. A type with no published module is remembered so it isn't re-fetched
// every lookup, and the caller falls back to any in-tree driver.
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
		m.logf("module %s unavailable: %v", engineType, err)
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
// the registry. Opt out with DOZE_MODULES=off (offline / process-only); override
// the source with DOZE_MODULES_MIRROR or a modules{} block.
func Enabled() bool { return os.Getenv("DOZE_MODULES") != "off" }

// Mirror returns the configured registry base.
func (m *Manager) Mirror() string { m.mu.Lock(); defer m.mu.Unlock(); return m.base }

// Cached returns the path + version of a cached build of the module for engine
// type, for the host platform (newest by directory listing), or ok=false if none
// is cached. It does no network — for inspection (`doze modules`).
func (m *Manager) Cached(engineType string) (path, version string, ok bool) {
	m.mu.Lock()
	source := m.sourceFor(engineType)
	m.mu.Unlock()
	ns, name, err := splitSource(source)
	if err != nil {
		return "", "", false
	}
	base := filepath.Join(m.home, "modules", ns, name)
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
