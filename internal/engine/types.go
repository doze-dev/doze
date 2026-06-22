// Package engine defines the driver contract every database engine implements,
// plus the value types the generic runtime and proxy exchange with drivers.
//
// The package contains NO engine-specific code — only the contract. Concrete
// engines live in their own packages (engine/postgres, engine/valkey, …) and
// self-register via Register in an init function.
package engine

import (
	"path/filepath"
	"strings"
)

// VersionSpec is the raw, un-normalized version from config: either a major
// version ("16") or an exact dotted full version ("16.14"). Each driver
// normalizes it to the form its mirror and toolchain expect.
type VersionSpec string

// IsExact reports whether the spec pins an exact version (contains a dot)
// rather than just a major.
func (v VersionSpec) IsExact() bool { return strings.Contains(string(v), ".") }

// String returns the raw spec text.
func (v VersionSpec) String() string { return string(v) }

// Platform identifies the host for toolchain artifact selection.
type Platform struct {
	OS     string // "linux", "darwin"
	Arch   string // "amd64", "arm64"
	Triple string // e.g. "x86_64-unknown-linux-gnu"
}

// Toolchain is a resolved set of executables for one engine version.
type Toolchain struct {
	Engine string            // "postgres"
	Full   string            // resolved full version, e.g. "16.14.0"
	BinDir string            // directory of executables
	Tools  map[string]string // optional logical-name -> absolute-path overrides
}

// Path returns the absolute path to a named executable in the toolchain,
// honoring any explicit override in Tools.
func (t Toolchain) Path(tool string) string {
	if p, ok := t.Tools[tool]; ok && p != "" {
		return p
	}
	return filepath.Join(t.BinDir, tool)
}

// Pin records the exact version a (engine, spec, platform) resolved to and the
// per-triple archive checksums it was verified against.
type Pin struct {
	Resolved string            // full version
	Source   string            // "mirror", "override", …
	Hashes   map[string]string // triple -> "sha256:<hex>"
}

// Locker records and enforces resolved version pins. The binaries lockfile
// implements it; drivers call Record after resolving.
type Locker interface {
	Get(engine string, spec VersionSpec, plat Platform) (Pin, bool)
	Record(engine string, spec VersionSpec, plat Platform, pin Pin)
}

// Endpoint is doze's client-facing listener(s) for one instance plus the
// backend address the proxy splices to.
type Endpoint struct {
	UnixSocket string // doze-owned client socket path, "" if none
	TCPAddr    string // doze-owned host:port, "" if none
	Backend    string // backend socket path the proxy dials (Driver.BackendSocket)
}

// EngineConfig is the opaque, engine-specific configuration payload decoded
// from a config block. Drivers type-assert it to their own concrete type.
type EngineConfig = any

// Instance is the runtime's view of one declared instance, handed to a driver.
type Instance struct {
	Name      string         // declared instance name (config block label)
	Type      string         // engine type ("postgres")
	Version   VersionSpec    // major or exact full
	DataDir   string         // per-instance data directory
	SocketDir string         // per-instance backend socket directory
	Port      int            // nominal port used for socket naming
	Endpoint  Endpoint       // doze-owned client-facing endpoint(s)
	Spec      EngineConfig   // engine-specific config (decoded by the driver)
	Deps      map[string]Dep // resolved dependencies, keyed by instance name
}

// Dep is a resolved dependency the runtime hands to a dependent instance's
// driver: another instance that has been booted and is held running.
type Dep struct {
	Name      string // dependency instance name
	Engine    string // dependency engine type
	SocketDir string // dependency's backend socket directory
	Backend   string // dependency's backend socket path
	URL       string // direct backend connection URL (from BackendProvider)
}
