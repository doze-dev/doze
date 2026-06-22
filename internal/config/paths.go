package config

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
)

// EnvHome overrides the global doze home directory (like proto's PROTO_HOME).
const EnvHome = "DOZE_HOME"

// The doze home is laid out like moonrepo's proto: a single global directory
// holding a shared tool store plus per-project state, all under one root.
//
//	~/.doze/                       ($DOZE_HOME, else ~/.doze)
//	  <engine>/                    shared toolchains per engine (deduped)
//	  cache/                       transient downloads / metadata
//	  projects/<slug>/             per-project state
//	    clusters/<instance>/       data directories
//	    run/                       sockets, pidfile, log, admin socket

// CacheDir holds transient downloads and metadata.
func (c *Config) CacheDir() string { return filepath.Join(c.Home, "cache") }

// TLSDir holds the auto-generated self-signed certificate, shared across
// projects in this home.
func (c *Config) TLSDir() string { return filepath.Join(c.Home, "tls") }

// ResolvePath resolves a config-relative path against the config file's
// directory; absolute paths pass through unchanged.
func (c *Config) ResolvePath(p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	base := "."
	if c.path != "" {
		base = filepath.Dir(c.path)
	}
	return filepath.Join(base, p)
}

// ProjectDir is this project's state root.
func (c *Config) ProjectDir() string { return c.DataDir }

// ClustersDir holds this project's per-database data directories.
func (c *Config) ClustersDir() string { return filepath.Join(c.DataDir, "clusters") }

// ClusterDir is the data directory for a single database.
func (c *Config) ClusterDir(name string) string { return filepath.Join(c.ClustersDir(), name) }

// RunDir holds this project's runtime files (sockets, pidfile, log).
func (c *Config) RunDir() string { return filepath.Join(c.DataDir, "run") }

// SocketDir is the backend socket directory for a single database.
func (c *Config) SocketDir(name string) string { return filepath.Join(c.RunDir(), name) }

// projectSlug derives a stable, readable identifier for the project that owns
// configPath: the project directory's base name plus a short hash of its
// absolute path, so two projects with the same base name never collide.
func projectSlug(configPath string) string {
	if configPath == "" {
		return "default"
	}
	abs, err := filepath.Abs(configPath)
	if err != nil {
		abs = configPath
	}
	root := filepath.Dir(abs)
	sum := sha256.Sum256([]byte(root))
	return sanitizeSlug(filepath.Base(root)) + "-" + hex.EncodeToString(sum[:])[:8]
}

// sanitizeSlug lowercases and replaces any run of non-alphanumeric characters
// with a single dash.
func sanitizeSlug(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "project"
	}
	return out
}
