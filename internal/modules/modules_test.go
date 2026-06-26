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
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/doze-dev/doze-sdk/binaries"
)

// signedRegistry lays out a file:// registry on disk for one namespace/module and
// returns (base URL, publisher public key b64). The archive carries a bin/<name>-
// plugin executable and a valid ed25519 signature over its sha256.
func signedRegistry(t *testing.T, ns, name string, priv ed25519.PrivateKey) (base, pubB64 string) {
	t.Helper()
	root := t.TempDir()
	plat, err := binaries.HostPlatform()
	if err != nil {
		t.Fatal(err)
	}
	pubB64 = base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey))

	// keys.json
	nsDir := filepath.Join(root, ns)
	modDir := filepath.Join(nsDir, name)
	if err := os.MkdirAll(modDir, 0o755); err != nil {
		t.Fatal(err)
	}
	keys, _ := json.Marshal(keysDoc{Namespace: ns, Key: pubB64})
	if err := os.WriteFile(filepath.Join(nsDir, "keys.json"), keys, 0o644); err != nil {
		t.Fatal(err)
	}

	// archive: a tar.gz with bin/<name>-plugin
	archive := tarGzPlugin(t, name)
	full := "0.1.0"
	arName := name + "-" + full + "-" + plat.Triple + ".tar.gz"
	if err := os.WriteFile(filepath.Join(modDir, arName), archive, 0o644); err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(archive)
	shaHex := hex.EncodeToString(sum[:])
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(shaHex)))

	// index.yaml (the binaries.Manifest shape), artifact URL relative to the module dir.
	index := "engines:\n" +
		"  " + name + ":\n" +
		"    versions:\n" +
		"      default: \"" + full + "\"\n" +
		"    artifacts:\n" +
		"      \"" + full + "\":\n" +
		"        " + plat.Triple + ":\n" +
		"          url: " + arName + "\n" +
		"          sha256: " + shaHex + "\n" +
		"          sig: " + sig + "\n"
	if err := os.WriteFile(filepath.Join(modDir, "index.yaml"), []byte(index), 0o644); err != nil {
		t.Fatal(err)
	}
	return "file://" + root, pubB64
}

func tarGzPlugin(t *testing.T, name string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	body := []byte("#!/bin/sh\necho plugin\n")
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
	m.base = base // bypass env; point at the on-disk registry
	m.UseLock(func() string { return lockPath })
	return m
}

func TestResolveSignedModule(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	base, pub := signedRegistry(t, "doze", "valkey", priv)
	m := newTestManager(t, base)

	exe, err := m.Resolve(context.Background(), "valkey", "")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if !strings.HasSuffix(exe, "valkey-plugin") {
		t.Fatalf("plugin exe = %q, want …valkey-plugin", exe)
	}

	// The publisher key must be pinned in the lock (trust-on-first-use).
	lock, err := binaries.LoadLock(m.lockPath())
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := lock.GetKey("doze"); !ok || got != pub {
		t.Fatalf("lock key for doze = %q (ok=%v), want %q", got, ok, pub)
	}
	// And the module pin must be keyed by the source address.
	if _, ok := lock.GetModule("doze/valkey", "default"); !ok {
		t.Fatalf("module pin doze/valkey not recorded: %+v", lock.Modules)
	}
}

func TestRejectUnsignedModule(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	base, _ := signedRegistry(t, "doze", "valkey", priv)
	// Strip the sig line from the index so the artifact is unsigned.
	idx := filepath.Join(strings.TrimPrefix(base, "file://"), "doze", "valkey", "index.yaml")
	body, _ := os.ReadFile(idx)
	var keep []string
	for _, ln := range strings.Split(string(body), "\n") {
		if !strings.Contains(ln, "sig:") {
			keep = append(keep, ln)
		}
	}
	os.WriteFile(idx, []byte(strings.Join(keep, "\n")), 0o644)

	m := newTestManager(t, base)
	if _, err := m.Resolve(context.Background(), "valkey", ""); err == nil {
		t.Fatal("expected unsigned module to be rejected")
	} else if !strings.Contains(err.Error(), "unsigned") && !strings.Contains(err.Error(), "signature") {
		t.Fatalf("error = %v, want a signature failure", err)
	}
}

func TestRejectKeyRotation(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	base, _ := signedRegistry(t, "doze", "valkey", priv)
	m := newTestManager(t, base)
	if _, err := m.Resolve(context.Background(), "valkey", ""); err != nil {
		t.Fatalf("first resolve: %v", err)
	}

	// Swap the publisher key in the registry; the pinned key must now block it.
	_, priv2, _ := ed25519.GenerateKey(nil)
	pub2 := base64.StdEncoding.EncodeToString(priv2.Public().(ed25519.PublicKey))
	keys, _ := json.Marshal(keysDoc{Namespace: "doze", Key: pub2})
	os.WriteFile(filepath.Join(strings.TrimPrefix(base, "file://"), "doze", "keys.json"), keys, 0o644)

	// Fresh manager (cold key cache) so it re-reads keys.json against the lock.
	m2 := newTestManager(t, base)
	m2.UseLock(m.lockPath)
	if _, err := m2.Resolve(context.Background(), "valkey", ""); err == nil {
		t.Fatal("expected rotated key to be rejected by the TOFU pin")
	} else if !strings.Contains(err.Error(), "key for namespace") {
		t.Fatalf("error = %v, want a key-rotation rejection", err)
	}
}

func TestSourceOverride(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	base, _ := signedRegistry(t, "acme", "redis", priv)
	m := newTestManager(t, base)
	// Engine type "valkey" served by acme/redis via a modules{} source override.
	m.Configure("", true, map[string]string{"valkey": "acme/redis"})

	exe, err := m.Resolve(context.Background(), "valkey", "")
	if err != nil {
		t.Fatalf("Resolve with source override: %v", err)
	}
	if !strings.HasSuffix(exe, "redis-plugin") {
		t.Fatalf("plugin exe = %q, want …redis-plugin", exe)
	}
}
