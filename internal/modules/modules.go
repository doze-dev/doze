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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/doze-dev/doze-sdk/binaries"
	"github.com/doze-dev/doze-sdk/engine"
	"github.com/doze-dev/doze-sdk/modindex"
	dozeplugin "github.com/doze-dev/doze-sdk/plugin"
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

// DefaultChannel is the release channel a fresh (unpinned) resolve follows in
// the module index. Module versions are a different axis from the engine
// version a user declares: selection picks the newest release compatible with
// this doze's plugin protocol and the declared engine majors, preferring the
// channel head.
const DefaultChannel = "stable"

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
	persist  func() bool   // whether the running command may write doze.lock (nil = never)
	logf     func(string, ...any)

	mu      sync.Mutex
	enabled bool              // fetch modules at all
	base    string            // registry base URL (default or override)
	sources map[string]string // engine type -> source address override (from modules{})
	// versions holds per-engine exact module-version pins from the modules{}
	// block — the escape hatch for holding back a regressed release. Empty for
	// almost everyone: selection + doze.lock do the work.
	versions map[string]string
	// requires accumulates the engine MAJORS declared in config per engine type
	// (fed by config.Hooks.RequireEngine before the first driver lookup), so fresh
	// module selection picks a release that supports what the project declares.
	requires map[string]map[string]bool
	misses   map[string]bool // engine types with no published module (negative cache)
	// verifyErrs holds the last real failure (signature/checksum/transport) for a
	// type, as opposed to a genuine "not published" miss. Surfaced by LastError so
	// a fetch/verification failure reads as itself, not as "unknown engine type".
	verifyErrs map[string]error

	nsm  map[string]*binaries.Manager // memoized per-namespace fetchers (keyed by namespace)
	keys map[string]ed25519.PublicKey // verified publisher keys (keyed by namespace)

	knownTypes *[]string // memoized catalog type names for suggestions (nil = not yet fetched)
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
		home:       home,
		plat:       plat,
		logf:       func(string, ...any) {},
		enabled:    os.Getenv("DOZE_MODULES") != "off", // default-on: core ships no backing engines
		base:       base,
		sources:    map[string]string{},
		versions:   map[string]string{},
		requires:   map[string]map[string]bool{},
		misses:     map[string]bool{},
		verifyErrs: map[string]error{},
		nsm:        map[string]*binaries.Manager{},
		keys:       map[string]ed25519.PublicKey{},
	}, nil
}

// Configure applies a decoded modules{} block: an optional registry override,
// whether fetching is enabled, per-engine source overrides, and per-engine exact
// module-version pins. It runs before any instance's driver is resolved.
func (m *Manager) Configure(mirror string, enabled bool, sources, versions map[string]string) {
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
	for k, v := range sources {
		if v != "" {
			m.sources[k] = v
		}
	}
	for k, v := range versions {
		if v != "" {
			m.versions[k] = v
		}
	}
}

// Require records that config declares engine type at the given engine version,
// so module selection only accepts releases supporting its major. Called by the
// config loader (via config.Hooks.RequireEngine) before the driver lookup that
// triggers the fetch.
func (m *Manager) Require(engineType, version string) {
	if version == "" || version == "builtin" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.requires[engineType] == nil {
		m.requires[engineType] = map[string]bool{}
	}
	m.requires[engineType][modindex.Major(version)] = true
}

// requiredMajors returns the sorted engine majors declared for a type.
func (m *Manager) requiredMajors(engineType string) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.requires[engineType]))
	for major := range m.requires[engineType] {
		out = append(out, major)
	}
	sort.Strings(out)
	return out
}

// SetLogger installs a progress logger for downloads.
func (m *Manager) SetLogger(f func(string, ...any)) { m.mu.Lock(); m.logf = f; m.mu.Unlock() }

// UseLock makes Resolve pin each fetched module + namespace key in the project
// doze.lock, and verify re-fetches against the locked checksum + pinned key.
func (m *Manager) UseLock(lockPath func() string) { m.lockPath = lockPath }

// PersistWhen gates doze.lock writes on f: pins and TOFU keys are only saved
// while f reports true. Read commands (status, lint, doctor, …) must never
// mutate the lockfile — they still resolve and verify, but any new pin lives
// only in this process; commands that materialize state (up, sync, wake, …)
// opt in and freeze the choice for the team.
func (m *Manager) PersistWhen(f func() bool) { m.persist = f }

// mayPersist reports whether the running command may write the lockfile.
// Unwired (nil) means yes — persistence is the default contract; cmd/doze
// wires PersistWhen so read commands opt out.
func (m *Manager) mayPersist() bool { return m.persist == nil || m.persist() }

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

// Resolve fetches (or finds cached) the plugin binary for an engine type and
// returns its executable path. The MODULE version is chosen here, never by the
// user: a doze.lock pin wins (gated offline against this doze's plugin protocol
// and the declared engine majors); otherwise the newest compatible release from
// the signed index is selected and pinned. It resolves the type's source, pins
// the namespace publisher key (trust-on-first-use), verifies the index-level
// signature, and verifies each artifact's signature before running it.
func (m *Manager) Resolve(ctx context.Context, engineType string) (string, error) {
	source := m.sourceFor(engineType)
	ns, name, err := splitSource(source)
	if err != nil {
		return "", err
	}
	lock := m.loadLock()
	m.mu.Lock()
	want := m.versions[engineType] // modules{} exact-version knob, "" for almost everyone
	m.mu.Unlock()
	majors := m.requiredMajors(engineType)

	// Pinned path: the lock's pin wins so a moving channel can't drift. Its
	// compatibility gates run offline from the metadata frozen at pin time, and
	// a warm cache never touches the network — not even for keys.json.
	if pin, ok := lock.GetModule(source); ok && (want == "" || want == pin.Version) {
		if pin.Protocol != 0 && pin.Protocol != dozeplugin.ProtocolVersion {
			return "", fmt.Errorf("module %s %s (pinned in doze.lock) speaks plugin protocol %d; this doze requires %d — run 'doze modules upgrade %s'",
				source, pin.Version, pin.Protocol, dozeplugin.ProtocolVersion, engineType)
		}
		if len(pin.Engines) > 0 {
			for _, major := range majors {
				if !slices.Contains(pin.Engines, major) {
					return "", fmt.Errorf("%s %s needs a newer %s module: pinned %s supports %s — run 'doze modules upgrade %s'",
						engineType, major, source, pin.Version, strings.Join(pin.Engines, ", "), engineType)
				}
			}
		}
		// Warm cache: nothing to fetch, not even the index.
		if binDir, ok := m.cachedBinDir(ns, name, pin.Version); ok {
			return pluginExe(binDir, name, pin.Version)
		}
		// Cold cache: the index supplies the artifact URL; the lock's checksum
		// stays authoritative.
		bm, err := m.nsManager(ns, lock)
		if err != nil {
			return "", err
		}
		idx, err := m.fetchIndex(bm, ns, name)
		if err != nil {
			return "", err
		}
		rel, ok := idx.Releases[pin.Version]
		if !ok {
			return "", fmt.Errorf("module %s %s (pinned in doze.lock) is no longer published — run 'doze modules upgrade %s'", source, pin.Version, engineType)
		}
		art, ok := rel.Artifacts[m.plat.Triple]
		if !ok {
			return "", fmt.Errorf("module %s %s has no artifact for %s", source, pin.Version, m.plat.Triple)
		}
		binDir, _, err := bm.EnsureArtifact(ctx, name, pin.Version, m.plat, art.URL, art.SHA256, art.Sig, pin.Hashes[m.plat.Triple])
		if err != nil {
			return "", err
		}
		return pluginExe(binDir, name, pin.Version)
	}

	// Fresh path (no pin, or the modules{} knob overrides it): select from the
	// verified index and freeze the choice.
	bm, err := m.nsManager(ns, lock)
	if err != nil {
		return "", err
	}
	idx, err := m.fetchIndex(bm, ns, name)
	if err != nil {
		return "", err
	}
	version, rel, err := m.selectRelease(idx, engineType, want, majors)
	if err != nil {
		return "", err
	}
	art, ok := rel.Artifacts[m.plat.Triple]
	if !ok {
		return "", fmt.Errorf("module %s %s has no artifact for %s", source, version, m.plat.Triple)
	}
	binDir, _, err := bm.EnsureArtifact(ctx, name, version, m.plat, art.URL, art.SHA256, art.Sig, "")
	if err != nil {
		return "", err
	}
	m.recordPin(lock, source, version, rel)
	return pluginExe(binDir, name, version)
}

// selectRelease picks a module release: the modules{} exact pin when set (still
// protocol- and engine-gated, with precise errors), else modindex.Select's
// newest-compatible policy.
func (m *Manager) selectRelease(idx *modindex.Index, engineType, want string, majors []string) (string, modindex.Release, error) {
	if want == "" {
		return modindex.Select(idx, dozeplugin.ProtocolVersion, majors, DefaultChannel)
	}
	rel, ok := idx.Releases[want]
	if !ok {
		return "", modindex.Release{}, fmt.Errorf("module %s/%s has no release %s (modules{} pins it); published: %s",
			idx.Namespace, idx.Module, want, strings.Join(releaseVersions(idx), ", "))
	}
	if rel.Protocol != dozeplugin.ProtocolVersion {
		return "", modindex.Release{}, fmt.Errorf("module %s/%s %s (pinned in modules{}) speaks plugin protocol %d; this doze requires %d",
			idx.Namespace, idx.Module, want, rel.Protocol, dozeplugin.ProtocolVersion)
	}
	if !modindex.Supports(rel, majors) {
		return "", modindex.Release{}, fmt.Errorf("module %s/%s %s (pinned in modules{}) supports %s %s, not the declared version(s) %s",
			idx.Namespace, idx.Module, want, engineType, strings.Join(rel.Engines, ", "), strings.Join(majors, ", "))
	}
	return want, rel, nil
}

// fetchIndex fetches a module's schema-1 index and verifies its index-level
// signature against the namespace's (TOFU-pinned) publisher key — so protocol,
// engine support, and channel heads are attacker-controlled nowhere.
func (m *Manager) fetchIndex(bm *binaries.Manager, ns, name string) (*modindex.Index, error) {
	url := strings.TrimRight(m.base, "/") + "/" + ns + "/" + name + "/index.yaml"
	body, err := bm.Fetch(url)
	if err != nil {
		return nil, fmt.Errorf("fetching module index %s: %w", url, err)
	}
	// A static registry (Cloudflare Pages) answers a missing path with its HTML
	// 404 page — which can arrive with a success status. That is "no such
	// module", not a corrupt index; classify it as a miss so the caller falls
	// back to the unknown-engine path (and its did-you-mean).
	if looksLikeHTML(body) {
		return nil, fmt.Errorf("module %s/%s is not published in the registry (%s serves no index)", ns, name, url)
	}
	idx, err := modindex.Parse(body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", url, err)
	}
	if idx.Module != name {
		return nil, fmt.Errorf("%s: index is for module %q, expected %q", url, idx.Module, name)
	}
	m.mu.Lock()
	key := m.keys[ns]
	m.mu.Unlock()
	if key == nil {
		return nil, fmt.Errorf("no verified publisher key for namespace %q", ns)
	}
	if err := modindex.Verify(idx, key); err != nil {
		return nil, fmt.Errorf("module index for %s/%s: %w", ns, name, err)
	}
	return idx, nil
}

// cachedBinDir reports the content-addressed cache dir for (source, version) on
// this platform, if its bin dir is populated. No network.
func (m *Manager) cachedBinDir(ns, name, version string) (string, bool) {
	binDir := filepath.Join(m.home, "modules", ns, name, version+"-"+m.plat.Triple, "bin")
	entries, err := os.ReadDir(binDir)
	if err != nil || len(entries) == 0 {
		return "", false
	}
	return binDir, true
}

func releaseVersions(idx *modindex.Index) []string {
	out := make([]string, 0, len(idx.Releases))
	for v := range idx.Releases {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return modindex.CompareVersions(out[i], out[j]) > 0 })
	return out
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
	if looksLikeHTML(body) {
		return nil, fmt.Errorf("namespace %q is not published in the registry (%s serves no keys.json)", ns, url)
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
		if m.mayPersist() {
			_ = lock.Save()
		}
	}
	m.keys[ns] = key
	return key, nil
}

// looksLikeHTML sniffs a response body that should have been YAML/JSON but is a
// web page — a static host's 404 fallback.
func looksLikeHTML(body []byte) bool {
	trimmed := strings.TrimSpace(string(body))
	if len(trimmed) > 64 {
		trimmed = trimmed[:64]
	}
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(lower, "<!doctype") || strings.HasPrefix(lower, "<html")
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

// recordPin freezes a module source to the selected release: version, protocol,
// engine support, and every published triple's checksum (from the verified
// index, so teammates on other platforms verify against the same pin). A brand
// new pin gets a commit nudge — the lock is the team's reproducibility contract,
// and the moment it's born is the moment to say so.
func (m *Manager) recordPin(lock *binaries.Lock, source, version string, rel modindex.Release) {
	if lock == nil {
		return
	}
	_, existed := lock.GetModule(source)
	hashes := make(map[string]string, len(rel.Artifacts))
	for triple, art := range rel.Artifacts {
		hashes[triple] = "sha256:" + strings.ToLower(art.SHA256)
	}
	lock.RecordModule(source, binaries.ModulePin{
		Version:  version,
		Protocol: rel.Protocol,
		Engines:  append([]string(nil), rel.Engines...),
		Hashes:   hashes,
	})
	// Read commands resolve but never write: the pin lives in this process only,
	// and doze.lock stays byte-identical until a mutating command freezes it.
	if !m.mayPersist() {
		return
	}
	_ = lock.Save()
	if !existed {
		m.logf("pinned %s %s in %s — commit the lockfile so your team and CI get this exact build", source, version, filepath.Base(lock.Path()))
	}
}

// Pinned returns the doze.lock pin for an engine type's source, with the source
// address, if one exists.
func (m *Manager) Pinned(engineType string) (binaries.ModulePin, string, bool) {
	source := m.sourceFor(engineType)
	lock := m.loadLock()
	pin, ok := lock.GetModule(source)
	return pin, source, ok
}

// CheckSupport validates one declared (engineType, engine version) against the
// pinned module's supported engine majors — the post-decode pass that catches a
// block whose version the already-resolved module can't serve (the first block
// of a type drives selection; later blocks are only caught here). Offline: it
// reads only the lock.
func (m *Manager) CheckSupport(engineType, version string) error {
	if version == "" || version == "builtin" || isInTree(engineType) {
		return nil
	}
	if os.Getenv("DOZE_"+strings.ToUpper(engineType)+"_PLUGIN") != "" {
		return nil // a local override bypasses the registry and its metadata
	}
	pin, source, ok := m.Pinned(engineType)
	if !ok || len(pin.Engines) == 0 {
		return nil
	}
	if major := modindex.Major(version); !slices.Contains(pin.Engines, major) {
		return fmt.Errorf("%s %s needs a newer %s module: pinned %s supports %s — run 'doze modules upgrade %s'",
			engineType, major, source, pin.Version, strings.Join(pin.Engines, ", "), engineType)
	}
	return nil
}

// Upgrade re-resolves an engine type's module from the registry, ignoring the
// existing pin (but honoring a modules{} exact-version knob), downloads and
// verifies the selected release, and rewrites the pin. It returns the old and
// new versions; changed is false when the pin was already at the head.
func (m *Manager) Upgrade(ctx context.Context, engineType string) (from, to string, changed bool, err error) {
	source := m.sourceFor(engineType)
	ns, name, err := splitSource(source)
	if err != nil {
		return "", "", false, err
	}
	lock := m.loadLock()
	bm, err := m.nsManager(ns, lock)
	if err != nil {
		return "", "", false, err
	}
	old, hadPin := lock.GetModule(source)

	idx, err := m.fetchIndex(bm, ns, name)
	if err != nil {
		return "", "", false, err
	}
	m.mu.Lock()
	want := m.versions[engineType]
	m.mu.Unlock()
	version, rel, err := m.selectRelease(idx, engineType, want, m.requiredMajors(engineType))
	if err != nil {
		return old.Version, "", false, err
	}
	if hadPin && old.Version == version {
		return old.Version, version, false, nil
	}
	art, ok := rel.Artifacts[m.plat.Triple]
	if !ok {
		return old.Version, "", false, fmt.Errorf("module %s %s has no artifact for %s", source, version, m.plat.Triple)
	}
	if _, _, err := bm.EnsureArtifact(ctx, name, version, m.plat, art.URL, art.SHA256, art.Sig, ""); err != nil {
		return old.Version, "", false, err
	}
	m.recordPin(lock, source, version, rel)
	return old.Version, version, true, nil
}

// UpgradeHint reports, best-effort, whether a newer compatible release exists
// for an engine type's pinned module — appended to config-decode errors so "the
// module doesn't know this argument" comes with its likely fix. Network errors
// yield "" (the hint must never make an error path worse).
func (m *Manager) UpgradeHint(engineType string) string {
	pin, source, ok := m.Pinned(engineType)
	if !ok {
		return ""
	}
	ns, name, err := splitSource(source)
	if err != nil {
		return ""
	}
	bm, err := m.nsManager(ns, m.loadLock())
	if err != nil {
		return ""
	}
	idx, err := m.fetchIndex(bm, ns, name)
	if err != nil {
		return ""
	}
	m.mu.Lock()
	want := m.versions[engineType]
	m.mu.Unlock()
	version, _, err := m.selectRelease(idx, engineType, want, m.requiredMajors(engineType))
	if err != nil || version == pin.Version || modindex.CompareVersions(version, pin.Version) <= 0 {
		return ""
	}
	return fmt.Sprintf("a newer module (%s) is available — run 'doze modules upgrade %s'", version, engineType)
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
	m.mu.Unlock()
	p, err := m.Resolve(context.Background(), engineType)
	if err != nil {
		m.mu.Lock()
		m.misses[engineType] = true
		if notPublished(err) {
			// Genuinely no such module: the caller falls back to an in-tree driver,
			// then reports "unknown engine type", which is the right message.
			m.logf("module %s: not published in the registry (%v)", engineType, err)
		} else {
			// A real failure (bad signature, checksum mismatch, network): record it
			// so LastError can surface it verbatim instead of a misleading
			// "unknown engine type", and log it loudly.
			m.verifyErrs[engineType] = err
			m.logf("module %s failed to load: %v", engineType, err)
		}
		m.mu.Unlock()
		return "", nil, false
	}
	return p, nil, true
}

// LastError returns the real fetch/verification failure recorded for an engine
// type (signature/checksum/transport), or nil if the type simply has no published
// module. Callers that would otherwise report "unknown engine type" can consult
// this to surface the actual cause.
func (m *Manager) LastError(engineType string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.verifyErrs[engineType]
}

// notPublished reports whether err means the module is genuinely absent from the
// registry (a 404 / missing index) rather than a real verification or transport
// failure. Absence is an expected miss (fall back to in-tree); everything else —
// a bad signature, a checksum mismatch, a network error — is a real problem the
// user should see.
func notPublished(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	for _, sig := range []string{"signature", "checksum", "sha256", "verify", "key mismatch", "untrusted"} {
		if strings.Contains(s, sig) {
			return false // a real verification failure, not a plain miss
		}
	}
	for _, miss := range []string{"not published", "not found", "404", "no such", "no module", "does not exist", "unknown engine"} {
		if strings.Contains(s, miss) {
			return true
		}
	}
	// Default: treat an unrecognized error as a real failure (surface it) rather
	// than silently masking it as a miss.
	return false
}

// isInTree reports whether an engine type is compiled into doze core (registered
// in-tree, e.g. process). Such types are never fetched as modules.
func isInTree(engineType string) bool {
	return slices.Contains(engine.Types(), engineType)
}

// InTree reports whether an engine type is compiled into doze core (and so is
// never module-fetched, pinned, or upgraded).
func InTree(engineType string) bool { return isInTree(engineType) }

// Inspection is the result of inspecting a registry source without launching it.
type Inspection struct {
	Source      string
	Namespace   string
	Name        string
	Version     string   // the inspected release (the stable head unless overridden)
	Protocol    int      // plugin protocol that release speaks
	Engines     []string // engine majors that release supports (empty = versionless)
	Releases    []string // every published release, newest first
	IndexSigned bool     // the index-level signature verifies
	Platforms   []PlatformStatus
}

// PlatformStatus is one artifact's per-triple provenance.
type PlatformStatus struct {
	Triple string
	URL    string
	SHA256 string
	Signed bool // sig present AND verifies against the namespace publisher key
}

// Inspect fetches a source's publisher key (pinning it trust-on-first-use) and
// its module index, then reports the release's compatibility metadata and each
// platform artifact's signature status — the same checks Resolve enforces before
// running a module, surfaced for `doze modules info`. version "" inspects the
// stable head. It downloads no archives.
func (m *Manager) Inspect(source, version string) (*Inspection, error) {
	ns, name, err := splitSource(source)
	if err != nil {
		return nil, err
	}
	lock := m.loadLock()
	bm, err := m.nsManager(ns, lock)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	key := m.keys[ns]
	m.mu.Unlock()

	// Fetch + parse WITHOUT the hard signature gate: info exists to report a
	// broken index, so verification is a field here, not a precondition.
	url := strings.TrimRight(m.base, "/") + "/" + ns + "/" + name + "/index.yaml"
	body, err := bm.Fetch(url)
	if err != nil {
		return nil, fmt.Errorf("fetching module index %s: %w", url, err)
	}
	if looksLikeHTML(body) {
		return nil, fmt.Errorf("module %s is not published in the registry (%s serves no index)", source, url)
	}
	idx, err := modindex.Parse(body)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", url, err)
	}
	if version == "" {
		if version = idx.Channels[DefaultChannel]; version == "" {
			return nil, fmt.Errorf("module %s has no %q channel", source, DefaultChannel)
		}
	}
	rel, ok := idx.Releases[version]
	if !ok {
		return nil, fmt.Errorf("module %s has no release %s; published: %s", source, version, strings.Join(releaseVersions(idx), ", "))
	}
	insp := &Inspection{
		Source: source, Namespace: ns, Name: name,
		Version: version, Protocol: rel.Protocol, Engines: rel.Engines,
		Releases:    releaseVersions(idx),
		IndexSigned: key != nil && modindex.Verify(idx, key) == nil,
	}
	for triple, art := range rel.Artifacts {
		insp.Platforms = append(insp.Platforms, PlatformStatus{
			Triple: triple, URL: art.URL, SHA256: art.SHA256,
			Signed: verifyArtifactSig(key, art.SHA256, art.Sig),
		})
	}
	sort.Slice(insp.Platforms, func(i, j int) bool { return insp.Platforms[i].Triple < insp.Platforms[j].Triple })
	return insp, nil
}

// verifyArtifactSig reports whether sigB64 is a valid signature over the artifact's
// hex sha256 by the namespace publisher key.
func verifyArtifactSig(key ed25519.PublicKey, sha256hex, sigB64 string) bool {
	if key == nil || sigB64 == "" || sha256hex == "" {
		return false
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return false
	}
	return ed25519.Verify(key, []byte(strings.ToLower(sha256hex)), sig)
}

// Enabled reports whether module fetching is on. It is default-on: core compiles
// in only the process primitive, so every other engine is a module fetched from
// the registry. Opt out with DOZE_MODULES=off (offline / process-only); override
// the source with DOZE_MODULES_MIRROR or a modules{} block.
func Enabled() bool { return os.Getenv("DOZE_MODULES") != "off" }

// Mirror returns the configured registry base.
func (m *Manager) Mirror() string { m.mu.Lock(); defer m.mu.Unlock(); return m.base }

// Catalog is the registry's machine-readable index (served at <base>/index.json):
// every published namespace and the modules it offers. It's the discovery source
// for `doze modules search` and the init wizard — no module list is compiled in.
type Catalog struct {
	Namespaces map[string]CatalogNamespace `json:"namespaces"`
}

// CatalogNamespace is one publisher's slice of the catalog.
type CatalogNamespace struct {
	Official bool                     `json:"official"`
	Modules  map[string]CatalogModule `json:"modules"`
}

// CatalogModule is one published module's discovery facts. Version, Protocol,
// and EngineVersions come from the signed index's stable release (code-derived
// via dzm); the prose is author-declared meta.
type CatalogModule struct {
	Source         string   `json:"source"`
	Version        string   `json:"version"`  // module version at the stable head
	Protocol       int      `json:"protocol"` // plugin protocol that release speaks
	Tagline        string   `json:"tagline"`
	Category       string   `json:"category"`
	EngineVersions []string `json:"engineVersions"`
	Port           int      `json:"port"`
	Label          string   `json:"label"`
	Platforms      []string `json:"platforms"`
	Signed         bool     `json:"signed"`
}

// Catalog fetches and parses the registry's index.json catalog.
func (m *Manager) Catalog() (*Catalog, error) {
	url := strings.TrimRight(m.base, "/") + "/index.json"
	body, err := binaries.NewManager(m.home).Fetch(url)
	if err != nil {
		return nil, fmt.Errorf("fetching registry catalog (%s): %w", url, err)
	}
	var c Catalog
	if err := json.Unmarshal(body, &c); err != nil {
		return nil, fmt.Errorf("parsing registry catalog: %w", err)
	}
	return &c, nil
}

// CatalogEntry is a flattened catalog module with its namespace, for listing.
type CatalogEntry struct {
	CatalogModule
	Namespace string
	Name      string
	Official  bool
}

// Meta fetches a module's generated meta.yaml from the registry — the
// documentation `doze modules docs` renders. Raw bytes: the CLI owns the shape.
func (m *Manager) Meta(source string) ([]byte, error) {
	ns, name, err := splitSource(source)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(m.base, "/") + "/" + ns + "/" + name + "/meta.yaml"
	body, err := binaries.NewManager(m.home).Fetch(url)
	if err != nil {
		return nil, fmt.Errorf("fetching module docs (%s): %w", url, err)
	}
	if looksLikeHTML(body) {
		return nil, fmt.Errorf("module %s has no published docs (%s) — is the source spelled right? try `doze modules search`", source, url)
	}
	return body, nil
}

// KnownTypes returns the engine-type names the registry catalog offers, for
// "did you mean" suggestions on a typo'd block type. Best-effort with a short
// timeout (it runs on an error path) and memoized: one network attempt per
// process, empty on any failure.
func (m *Manager) KnownTypes() []string {
	m.mu.Lock()
	if m.knownTypes != nil {
		defer m.mu.Unlock()
		return *m.knownTypes
	}
	base := m.base
	m.mu.Unlock()

	names := []string{}
	bm := binaries.NewManager(m.home)
	bm.HTTP = &http.Client{Timeout: 3 * time.Second}
	if body, err := bm.Fetch(strings.TrimRight(base, "/") + "/index.json"); err == nil {
		var c Catalog
		if json.Unmarshal(body, &c) == nil {
			for _, ns := range c.Namespaces {
				for name := range ns.Modules {
					names = append(names, name)
				}
			}
		}
	}
	sort.Strings(names)
	m.mu.Lock()
	m.knownTypes = &names
	m.mu.Unlock()
	return names
}

// CatalogModules returns every module across all namespaces, official first then
// alphabetical — the flat list `doze modules search` and the wizard render.
func (m *Manager) CatalogModules() ([]CatalogEntry, error) {
	cat, err := m.Catalog()
	if err != nil {
		return nil, err
	}
	var out []CatalogEntry
	for ns, n := range cat.Namespaces {
		for name, mod := range n.Modules {
			out = append(out, CatalogEntry{CatalogModule: mod, Namespace: ns, Name: name, Official: n.Official})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Official != out[j].Official {
			return out[i].Official
		}
		return out[i].Source < out[j].Source
	})
	return out, nil
}

// Cached returns the path + version of a cached build of the module for engine
// type, for the host platform (newest version, so it matches what a fresh
// resolve would prefer — NOT first-by-listing, which is the oldest), or
// ok=false if none is cached. It does no network — for inspection (`doze modules`).
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
		v := strings.TrimSuffix(e.Name(), suffix)
		if ok && modindex.CompareVersions(v, version) <= 0 {
			continue
		}
		exe := filepath.Join(base, e.Name(), "bin", name+"-plugin")
		if fi, err := os.Stat(exe); err == nil && !fi.IsDir() {
			path, version, ok = exe, v, true
		}
	}
	return path, version, ok
}

// CachedVersion reports the cached plugin path for an exact (engineType,
// version) on the host platform, or ok=false — the check `doze modules list`
// uses against the doze.lock pin.
func (m *Manager) CachedVersion(engineType, version string) (string, bool) {
	m.mu.Lock()
	source := m.sourceFor(engineType)
	m.mu.Unlock()
	ns, name, err := splitSource(source)
	if err != nil {
		return "", false
	}
	exe := filepath.Join(m.home, "modules", ns, name, version+"-"+m.plat.Triple, "bin", name+"-plugin")
	if fi, err := os.Stat(exe); err == nil && !fi.IsDir() {
		return exe, true
	}
	return "", false
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
