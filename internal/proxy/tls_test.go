package proxy

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"testing"
)

func TestBuildServerTLSGeneratesAndCaches(t *testing.T) {
	dir := t.TempDir()

	conf, err := BuildServerTLS("", "", dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(conf.Certificates) != 1 {
		t.Fatalf("expected one certificate, got %d", len(conf.Certificates))
	}
	// The cert/key should be cached on disk.
	if !fileExists(filepath.Join(dir, "doze.crt")) || !fileExists(filepath.Join(dir, "doze.key")) {
		t.Fatal("generated cert/key were not cached")
	}

	// It should validate for localhost and carry the loopback SANs.
	leaf, err := x509.ParseCertificate(conf.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := leaf.VerifyHostname("localhost"); err != nil {
		t.Errorf("cert should be valid for localhost: %v", err)
	}
	if leaf.NotAfter.Before(leaf.NotBefore) {
		t.Error("cert validity window is inverted")
	}

	// A second call must reuse the cached pair, not regenerate it.
	before, _ := os.ReadFile(filepath.Join(dir, "doze.crt"))
	if _, err := BuildServerTLS("", "", dir); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, "doze.crt"))
	if string(before) != string(after) {
		t.Error("cached certificate was regenerated on the second call")
	}
}

func TestBuildServerTLSLoadsProvidedPair(t *testing.T) {
	// Generate a pair to disk, then load it via the cert/key path arguments.
	dir := t.TempDir()
	if _, err := generateSelfSigned(filepath.Join(dir, "c.crt"), filepath.Join(dir, "c.key")); err != nil {
		t.Fatal(err)
	}
	conf, err := BuildServerTLS(filepath.Join(dir, "c.crt"), filepath.Join(dir, "c.key"), dir)
	if err != nil {
		t.Fatal(err)
	}
	if conf.MinVersion != tls.VersionTLS12 {
		t.Errorf("MinVersion = %x, want TLS1.2", conf.MinVersion)
	}
}
