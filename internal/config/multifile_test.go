package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMergesSiblingDozeHCL(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "doze.hcl")
	write(t, main, `
defaults { idle_timeout = "1m" }
fake "a" {
  version = "1"
  port    = 7001
}
`)
	write(t, filepath.Join(dir, "extra.doze.hcl"), `
fake "b" {
  version = "1"
  port    = 7002
}
`)

	cfg, err := Load(main, nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Lookup("a") == nil || cfg.Lookup("b") == nil {
		t.Fatalf("expected instances a (anchor) and b (sibling), got %d", len(cfg.Instances))
	}
	if cfg.Defaults.IdleTimeout.String() != "1m0s" {
		t.Fatalf("root settings from anchor not applied: %v", cfg.Defaults.IdleTimeout)
	}
}

func TestLoadDirectory(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "00-root.hcl"), `fake "a" {
  version = "1"
  port    = 7001
}`)
	write(t, filepath.Join(dir, "10-more.hcl"), `fake "b" {
  version = "1"
  port    = 7002
}`)

	cfg, err := Load(dir, nil)
	if err != nil {
		t.Fatalf("Load(dir): %v", err)
	}
	if len(cfg.Instances) != 2 {
		t.Fatalf("expected 2 instances from dir, got %d", len(cfg.Instances))
	}
}

func TestDuplicateAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "doze.hcl")
	write(t, main, `fake "dup" { version = "1" }`)
	write(t, filepath.Join(dir, "x.doze.hcl"), `fake "dup" { version = "1" }`)

	_, err := Load(main, nil)
	if err == nil || !strings.Contains(err.Error(), "already declared") {
		t.Fatalf("expected cross-file duplicate error, got %v", err)
	}
	// The positioned error should name the first declaration's file.
	if !strings.Contains(err.Error(), "doze.hcl") {
		t.Fatalf("duplicate error should reference the first declaration: %v", err)
	}
}

func TestUnknownBlockSuggests(t *testing.T) {
	_, err := Parse([]byte(`fak "a" { version = "1" }`), "doze.hcl", nil)
	if err == nil || !strings.Contains(err.Error(), `did you mean "fake"`) {
		t.Fatalf("expected did-you-mean suggestion, got %v", err)
	}
}

func TestPositionedMissingVersion(t *testing.T) {
	_, err := Parse([]byte("fake \"a\" {\n}\n"), "doze.hcl", nil)
	if err == nil || !strings.Contains(err.Error(), "missing required") {
		t.Fatalf("expected missing-version error, got %v", err)
	}
	// Positioned: should include the file name and a line marker.
	if !strings.Contains(err.Error(), "doze.hcl line") {
		t.Fatalf("missing-version error should be positioned (file:line): %v", err)
	}
}
