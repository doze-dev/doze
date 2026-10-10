package modules

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/doze-dev/doze-sdk/binaries"
	"github.com/doze-dev/doze-sdk/modindex"
	dozeplugin "github.com/doze-dev/doze-sdk/plugin"
)

// registryKeys maps a test registry's root directory to the key it is signed
// with, so newTestManager can stand it in for the compiled-in one.
var registryKeys = map[string]ed25519.PublicKey{}

// release describes one published module release for the test registry.
type release struct {
	version  string
	protocol int
	engines  []string
}

// signedRegistry lays out a schema-1 file:// registry on disk for one
// namespace/module and returns (base URL, publisher public key b64). Each
// release's archive carries a bin/<name>-plugin executable; artifacts and the
// index itself are validly ed25519-signed.
func signedRegistry(t *testing.T, ns, name string, priv ed25519.PrivateKey, releases ...release) (base, pubB64 string) {
	t.Helper()
	if len(releases) == 0 {
		releases = []release{{version: "0.1.0", protocol: dozeplugin.ProtocolVersion}}
	}
	root := t.TempDir()
	plat, err := binaries.HostPlatform()
	if err != nil {
		t.Fatal(err)
	}
	pubB64 = base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey))

	nsDir := filepath.Join(root, ns)
	modDir := filepath.Join(nsDir, name)
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatal(err)
	}
	registryKeys[root] = priv.Public().(ed25519.PublicKey)

	idx := &modindex.Index{
		Schema: modindex.Schema, Module: name, Namespace: ns,
		Releases: map[string]modindex.Release{},
		Channels: map[string]string{},
	}
	stable := ""
	for _, r := range releases {
		archive := tarGzPlugin(t, name, r.version)
		arName := name + "-plugin-" + r.version + "-" + plat.Triple + ".tar.gz"
		if err := os.WriteFile(filepath.Join(modDir, arName), archive, 0o644); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(archive)
		shaHex := hex.EncodeToString(sum[:])
		idx.Releases[r.version] = modindex.Release{
			Protocol: r.protocol,
			Engines:  r.engines,
			Artifacts: map[string]modindex.Artifact{
				plat.Triple: {
					URL:    "file://" + filepath.Join(modDir, arName),
					SHA256: shaHex,
					Sig:    base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(shaHex))),
				},
			},
		}
		if stable == "" || modindex.CompareVersions(r.version, stable) > 0 {
			stable = r.version
		}
	}
	idx.Channels["stable"] = stable
	if err := modindex.Sign(idx, priv); err != nil {
		t.Fatal(err)
	}
	writeIndex(t, modDir, idx)
	return "file://" + root, pubB64
}

func writeIndex(t *testing.T, modDir string, idx *modindex.Index) {
	t.Helper()
	b, err := yaml.Marshal(idx)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modDir, "index.yaml"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func tarGzPlugin(t *testing.T, name, version string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("#!/bin/sh\necho plugin " + version + "\n")
	hdr := &tar.Header{Name: "bin/" + name + "-plugin", Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func newTestManager(t *testing.T, base string) *Manager {
	t.Helper()
	home := t.TempDir()
	lockPath := filepath.Join(t.TempDir(), "doze.lock")
	m, err := NewManager(home)
	if err != nil {
		t.Fatal(err)
	}
	m.base = base // point at the on-disk registry, and trust the key it was signed with
	m.key = registryKeys[strings.TrimPrefix(base, "file://")]
	m.UseLock(func() string { return lockPath })
	return m
}

func TestResolveSignedModule(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	base, _ := signedRegistry(t, "doze", "valkey", priv,
		release{version: "0.2.0", protocol: dozeplugin.ProtocolVersion, engines: []string{"8", "9"}})
	m := newTestManager(t, base)
	m.Require("valkey", "9")

	exe, err := m.Resolve(context.Background(), "valkey")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !strings.HasSuffix(exe, "valkey-plugin") {
		t.Fatalf("plugin exe = %q, want …valkey-plugin", exe)
	}

	lock, err := binaries.LoadLock(m.lockPath())
	if err != nil {
		t.Fatal(err)
	}
	// The module pin carries the release's compatibility metadata.
	pin, ok := lock.GetModule("doze/valkey")
	if !ok {
		t.Fatalf("module pin doze/valkey not recorded: %+v", lock.Modules)
	}
	if pin.Version != "0.2.0" || pin.Protocol != dozeplugin.ProtocolVersion {
		t.Fatalf("pin = %+v", pin)
	}
	if len(pin.Engines) != 2 || pin.Engines[1] != "9" {
		t.Fatalf("pin engines = %v", pin.Engines)
	}

	// A second resolve is served from the pin + cache (no index re-selection).
	if _, err := m.Resolve(context.Background(), "valkey"); err != nil {
		t.Fatalf("pinned resolve: %v", err)
	}
}

func TestSelectionPrefersNewestCompatible(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	// stable head speaks a FUTURE protocol; an older release speaks ours.
	base, _ := signedRegistry(t, "doze", "postgres", priv,
		release{version: "0.2.0", protocol: dozeplugin.ProtocolVersion, engines: []string{"16", "17"}},
		release{version: "0.3.0", protocol: dozeplugin.ProtocolVersion + 1, engines: []string{"16", "17", "18"}})
	m := newTestManager(t, base)

	if _, err := m.Resolve(context.Background(), "postgres"); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	pin, _, ok := m.Pinned("postgres")
	if !ok || pin.Version != "0.2.0" {
		t.Fatalf("pin = %+v, want fallback to protocol-compatible 0.2.0", pin)
	}
}

func TestEngineSupportGates(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	base, _ := signedRegistry(t, "doze", "postgres", priv,
		release{version: "0.2.0", protocol: dozeplugin.ProtocolVersion, engines: []string{"14", "15", "16", "17", "18"}})
	m := newTestManager(t, base)

	// Fresh selection with an unsupported major names the latest and its support.
	m.Require("postgres", "19")
	_, err := m.Resolve(context.Background(), "postgres")
	if err == nil || !strings.Contains(err.Error(), "supports") {
		t.Fatalf("want engine-support selection error, got %v", err)
	}

	// A pinned module rejects a later-declared unsupported major with the
	// upgrade command.
	m2 := newTestManager(t, base)
	m2.Require("postgres", "16")
	if _, err := m2.Resolve(context.Background(), "postgres"); err != nil {
		t.Fatalf("resolve at 16: %v", err)
	}
	m2.Require("postgres", "19")
	_, err = m2.Resolve(context.Background(), "postgres")
	if err == nil || !strings.Contains(err.Error(), "doze modules upgrade postgres") {
		t.Fatalf("want pinned-path upgrade hint, got %v", err)
	}
	// CheckSupport (the post-decode pass) reports the same, per version.
	if err := m2.CheckSupport("postgres", "19"); err == nil || !strings.Contains(err.Error(), "doze modules upgrade postgres") {
		t.Fatalf("CheckSupport(19) = %v", err)
	}
	if err := m2.CheckSupport("postgres", "16.14"); err != nil {
		t.Fatalf("CheckSupport(16.14) = %v, want nil (major gate)", err)
	}
}

func TestProtocolGates(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	// Every release speaks a future protocol -> "upgrade doze".
	base, _ := signedRegistry(t, "doze", "valkey", priv,
		release{version: "0.9.0", protocol: dozeplugin.ProtocolVersion + 1})
	m := newTestManager(t, base)
	_, err := m.Resolve(context.Background(), "valkey")
	if err == nil || !strings.Contains(err.Error(), "upgrade doze") {
		t.Fatalf("want protocol error asking to upgrade doze, got %v", err)
	}
}

func TestUpgradeMovesPin(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	base, _ := signedRegistry(t, "doze", "valkey", priv,
		release{version: "0.1.0", protocol: dozeplugin.ProtocolVersion, engines: []string{"8", "9"}})
	m := newTestManager(t, base)
	if _, err := m.Resolve(context.Background(), "valkey"); err != nil {
		t.Fatal(err)
	}

	// Publish 0.2.0 into the same registry dir and re-sign the index.
	root := strings.TrimPrefix(base, "file://")
	base2, _ := signedRegistryInto(t, root, "doze", "valkey", priv,
		release{version: "0.1.0", protocol: dozeplugin.ProtocolVersion, engines: []string{"8", "9"}},
		release{version: "0.2.0", protocol: dozeplugin.ProtocolVersion, engines: []string{"8", "9", "10"}})
	_ = base2

	// The pin holds until an explicit upgrade.
	if _, err := m.Resolve(context.Background(), "valkey"); err != nil {
		t.Fatalf("pinned resolve after new release: %v", err)
	}
	pin, _, _ := m.Pinned("valkey")
	if pin.Version != "0.1.0" {
		t.Fatalf("pin drifted to %s without upgrade", pin.Version)
	}

	from, to, changed, err := m.Upgrade(context.Background(), "valkey")
	if err != nil || !changed || from != "0.1.0" || to != "0.2.0" {
		t.Fatalf("Upgrade = %s -> %s changed=%v err=%v", from, to, changed, err)
	}
	pin, _, _ = m.Pinned("valkey")
	if pin.Version != "0.2.0" || len(pin.Engines) != 3 {
		t.Fatalf("pin after upgrade = %+v", pin)
	}

	// Idempotent second upgrade.
	_, _, changed, err = m.Upgrade(context.Background(), "valkey")
	if err != nil || changed {
		t.Fatalf("second upgrade changed=%v err=%v", changed, err)
	}
}

// signedRegistryInto is signedRegistry writing into an existing root (so a test
// can grow a registry a manager already points at).
func signedRegistryInto(t *testing.T, root, ns, name string, priv ed25519.PrivateKey, releases ...release) (base, pubB64 string) {
	t.Helper()
	plat, _ := binaries.HostPlatform()
	pubB64 = base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey))
	modDir := filepath.Join(root, ns, name)
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatal(err)
	}
	idx := &modindex.Index{Schema: modindex.Schema, Module: name, Namespace: ns, Releases: map[string]modindex.Release{}, Channels: map[string]string{}}
	stable := ""
	for _, r := range releases {
		archive := tarGzPlugin(t, name, r.version)
		arName := name + "-plugin-" + r.version + "-" + plat.Triple + ".tar.gz"
		if err := os.WriteFile(filepath.Join(modDir, arName), archive, 0o644); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(archive)
		shaHex := hex.EncodeToString(sum[:])
		idx.Releases[r.version] = modindex.Release{
			Protocol: r.protocol, Engines: r.engines,
			Artifacts: map[string]modindex.Artifact{plat.Triple: {
				URL:    "file://" + filepath.Join(modDir, arName),
				SHA256: shaHex,
				Sig:    base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(shaHex))),
			}},
		}
		if stable == "" || modindex.CompareVersions(r.version, stable) > 0 {
			stable = r.version
		}
	}
	idx.Channels["stable"] = stable
	if err := modindex.Sign(idx, priv); err != nil {
		t.Fatal(err)
	}
	writeIndex(t, modDir, idx)
	return "file://" + root, pubB64
}

func TestRejectUnsignedIndex(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	base, _ := signedRegistry(t, "doze", "valkey", priv)
	// Strip the index signature: metadata is no longer attested -> reject.
	idxPath := filepath.Join(strings.TrimPrefix(base, "file://"), "doze", "valkey", "index.yaml")
	body, _ := os.ReadFile(idxPath)
	var keep []string
	for _, ln := range strings.Split(string(body), "\n") {
		if !strings.HasPrefix(ln, "signature:") {
			keep = append(keep, ln)
		}
	}
	os.WriteFile(idxPath, []byte(strings.Join(keep, "\n")), 0o644)

	m := newTestManager(t, base)
	if _, err := m.Resolve(context.Background(), "valkey"); err == nil {
		t.Fatal("expected unsigned index to be rejected")
	} else if !strings.Contains(err.Error(), "unsigned") {
		t.Fatalf("error = %v, want an index-signature failure", err)
	}
}

func TestRejectTamperedMetadata(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	base, _ := signedRegistry(t, "doze", "postgres", priv,
		release{version: "0.2.0", protocol: dozeplugin.ProtocolVersion, engines: []string{"16"}})
	// Claim postgres 19 support without re-signing: the index sig must fail.
	idxPath := filepath.Join(strings.TrimPrefix(base, "file://"), "doze", "postgres", "index.yaml")
	body, _ := os.ReadFile(idxPath)
	tampered := strings.Replace(string(body), `"16"`, `"19"`, 1)
	if tampered == string(body) {
		t.Fatal("fixture: engines list not found to tamper")
	}
	os.WriteFile(idxPath, []byte(tampered), 0o644)

	m := newTestManager(t, base)
	m.Require("postgres", "19")
	if _, err := m.Resolve(context.Background(), "postgres"); err == nil {
		t.Fatal("expected tampered metadata to be rejected")
	} else if !strings.Contains(err.Error(), "signature") {
		t.Fatalf("error = %v, want an index-signature failure", err)
	}
}

// The registry key is compiled in, not fetched: a registry signed by any
// other key is rejected outright, with nothing to trust on first use.
func TestRejectRegistrySignedByAnotherKey(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	base, _ := signedRegistry(t, "doze", "valkey", priv)
	m := newTestManager(t, base)

	other, _, _ := ed25519.GenerateKey(nil)
	m.key = other
	if _, err := m.Resolve(context.Background(), "valkey"); err == nil {
		t.Fatal("a registry signed by a key doze does not hold must be rejected")
	} else if !strings.Contains(err.Error(), "signature") {
		t.Fatalf("error = %v, want a signature failure", err)
	}
	// And nothing about it is written down for next time.
	if lock, err := binaries.LoadLock(m.lockPath()); err == nil && len(lock.Modules) != 0 {
		t.Fatalf("a rejected module must not be pinned: %+v", lock.Modules)
	}
}

func TestOneRegistryOneNamespace(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if m.base != RegistryBase {
		t.Fatalf("base = %q, want the doze registry", m.base)
	}
	want, err := binaries.ParsePublicKey(RegistryKey)
	if err != nil || !m.key.Equal(want) {
		t.Fatalf("a fresh manager must hold the compiled-in registry key (err %v)", err)
	}
	// The registry cannot be pointed elsewhere from the environment.
	t.Setenv("DOZE_MODULES_MIRROR", "file:///somewhere/else")
	if m2, _ := NewManager(t.TempDir()); m2.base != RegistryBase {
		t.Fatalf("DOZE_MODULES_MIRROR must be ignored, got base %q", m2.base)
	}
	if got := m.sourceFor("valkey"); got != "doze/valkey" {
		t.Fatalf("source = %q", got)
	}
	if _, err := m.Inspect("acme/redis", ""); err == nil || !strings.Contains(err.Error(), "one registry") {
		t.Fatalf("a module outside the doze namespace must be refused, got %v", err)
	}
}
