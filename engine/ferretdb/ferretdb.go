// Package ferretdb implements the doze engine.Driver for FerretDB, a MongoDB
// wire-protocol server. FerretDB v2 is stateless — it stores everything in a
// PostgreSQL backend (with the documentdb extension) — so this driver is
// Dependent: it declares a dependency on a postgres instance, which the runtime
// boots and holds running, and FerretDB connects to that backend over its unix
// socket. The mongo wire protocol needs no preamble, so clients ride the generic
// splice path.
package ferretdb

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nerdmenot/doze/internal/engine"
	"github.com/nerdmenot/doze/internal/supervisor"
)

func init() { engine.Register(Driver{}) }

const (
	bootTimeout = 30 * time.Second
	envBinDir   = "DOZE_FERRETDB_BINDIR"
	socketName  = "ferretdb.sock"
)

// Driver is the FerretDB engine driver.
type Driver struct{}

// Type implements engine.Driver.
func (Driver) Type() string { return "ferretdb" }

// Resolve implements engine.Driver. FerretDB versions are three-part (2.7.0).
func (Driver) Resolve(ctx context.Context, spec engine.VersionSpec, plat engine.Platform, lk engine.Locker, fetch engine.Fetcher) (engine.Toolchain, error) {
	if dir := os.Getenv(envBinDir); dir != "" {
		return engine.Toolchain{Engine: "ferretdb", BinDir: dir, Full: spec.String()}, nil
	}
	full, expectedSHA := "", ""
	if pin, ok := lk.Get("ferretdb", spec, plat); ok && pin.Resolved != "" {
		full = pin.Resolved
		expectedSHA = pin.Hashes[plat.Triple]
	} else if spec.IsExact() {
		full = spec.String()
	} else {
		v, err := fetch.ResolveMajor("ferretdb", spec.String())
		if err != nil {
			return engine.Toolchain{}, err
		}
		full = v
	}
	binDir, digest, err := fetch.Ensure(ctx, "ferretdb", full, plat, expectedSHA)
	if err != nil {
		return engine.Toolchain{}, err
	}
	hashes := map[string]string{}
	if digest != "" {
		hashes[plat.Triple] = digest
	}
	lk.Record("ferretdb", spec, plat, engine.Pin{Resolved: full, Source: "mirror", Hashes: hashes})
	return engine.Toolchain{Engine: "ferretdb", Full: full, BinDir: binDir}, nil
}

// DependsOn implements engine.Dependent: FerretDB needs its postgres backend.
func (Driver) DependsOn(inst engine.Instance) []string {
	if cfg, ok := inst.Spec.(*Config); ok && cfg != nil && cfg.Backend != "" {
		return []string{cfg.Backend}
	}
	return nil
}

// Provision implements engine.Driver: FerretDB is stateless (data lives in the
// postgres backend); it just needs a state directory.
func (Driver) Provision(_ context.Context, inst engine.Instance, _ engine.Toolchain) error {
	return os.MkdirAll(inst.DataDir, 0o700)
}

// Provisioned implements engine.Driver.
func (Driver) Provisioned(dataDir string) bool {
	fi, err := os.Stat(dataDir)
	return err == nil && fi.IsDir()
}

// Spawn implements engine.Driver: start FerretDB on a private unix socket,
// pointed at its postgres backend (resolved into inst.Deps by the runtime).
func (Driver) Spawn(_ context.Context, inst engine.Instance, tc engine.Toolchain) (engine.Process, error) {
	cfg, _ := inst.Spec.(*Config)
	if cfg == nil || cfg.Backend == "" {
		return nil, fmt.Errorf("ferretdb %q: missing backend", inst.Name)
	}
	dep, ok := inst.Deps[cfg.Backend]
	if !ok || dep.URL == "" {
		return nil, fmt.Errorf("ferretdb %q: backend %q is not a ready postgres instance", inst.Name, cfg.Backend)
	}
	if err := os.MkdirAll(inst.SocketDir, 0o700); err != nil {
		return nil, fmt.Errorf("creating socket dir: %w", err)
	}
	socket := socketPath(inst.SocketDir)
	_ = os.Remove(socket)

	cmd := exec.Command(tc.Path("ferretdb"))
	cmd.Env = append(os.Environ(),
		"FERRETDB_POSTGRESQL_URL="+dep.URL,
		"FERRETDB_LISTEN_UNIX="+socket,
		"FERRETDB_LISTEN_ADDR=", // disable the TCP listener; doze fronts it
		"FERRETDB_STATE_DIR="+inst.DataDir,
		"FERRETDB_TELEMETRY=disable",
	)
	return supervisor.Start(cmd)
}

// WaitReady implements engine.Driver: FerretDB is ready once its unix socket
// accepts connections (the mongo client performs its own handshake after).
func (Driver) WaitReady(ctx context.Context, inst engine.Instance, _ engine.Toolchain, p engine.Process) error {
	ctx, cancel := context.WithTimeout(ctx, bootTimeout)
	defer cancel()
	socket := socketPath(inst.SocketDir)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if !p.Alive() {
			return fmt.Errorf("ferretdb for %q exited during startup:\n%s", inst.Name, strings.Join(p.Logs(), "\n"))
		}
		if conn, err := net.DialTimeout("unix", socket, 500*time.Millisecond); err == nil {
			_ = conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("ferretdb for %q did not become ready within %s:\n%s", inst.Name, bootTimeout, strings.Join(p.Logs(), "\n"))
		case <-ticker.C:
		}
	}
}

// BackendSocket implements engine.Driver.
func (Driver) BackendSocket(socketDir string, _ int) string { return socketPath(socketDir) }

func socketPath(socketDir string) string { return filepath.Join(socketDir, socketName) }

// ConnString implements engine.Driver.
func (Driver) ConnString(inst engine.Instance, ep engine.Endpoint) (string, string) {
	host := ep.TCPAddr
	if host == "" {
		host = "localhost"
	}
	return "MONGODB_URI", "mongodb://" + host + "/"
}
