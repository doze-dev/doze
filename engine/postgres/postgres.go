// Package postgres implements the doze engine.Driver for PostgreSQL: resolving
// toolchains from the mirror, provisioning a data directory (initdb + tuned
// config), spawning the backend on a private unix socket, converging declared
// structure, and the wire-protocol proxy filter (startup/TLS/cancel).
package postgres

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nerdmenot/doze/internal/engine"
	"github.com/nerdmenot/doze/internal/supervisor"
)

func init() { engine.Register(Driver{}) }

// nominalPort names each backend's unix socket file (.s.PGSQL.<port>). No TCP
// port is bound on the backend, so every instance can share this number.
const nominalPort = 5432

const bootTimeout = 30 * time.Second

// envBinDir overrides toolchain resolution for every postgres instance.
const envBinDir = "DOZE_POSTGRES_BINDIR"

// Driver is the PostgreSQL engine driver.
type Driver struct{}

// Type implements engine.Driver.
func (Driver) Type() string { return "postgres" }

// NominalPort returns the socket-naming port the runtime should assign.
func (Driver) NominalPort() int { return nominalPort }

// Resolve implements engine.Driver.
func (Driver) Resolve(ctx context.Context, spec engine.VersionSpec, plat engine.Platform, lk engine.Locker, fetch engine.Fetcher) (engine.Toolchain, error) {
	if dir := os.Getenv(envBinDir); dir != "" {
		// Label the override with the declared spec so templating still has a
		// stable key (the bindir is assumed to match the declared version).
		full := spec.String()
		if spec.IsExact() {
			full = normalizeExact(full)
		}
		return engine.Toolchain{Engine: "postgres", BinDir: dir, Full: full}, nil
	}

	full, expectedSHA := "", ""
	if pin, ok := lk.Get("postgres", spec, plat); ok && pin.Resolved != "" {
		full = pin.Resolved
		expectedSHA = pin.Hashes[plat.Triple]
	} else if spec.IsExact() {
		full = normalizeExact(spec.String())
	} else {
		v, err := fetch.ResolveMajor("postgres", spec.String())
		if err != nil {
			return engine.Toolchain{}, err
		}
		full = v
	}

	binDir, digest, err := fetch.Ensure(ctx, "postgres", full, plat, expectedSHA)
	if err != nil {
		return engine.Toolchain{}, err
	}
	hashes := map[string]string{}
	if digest != "" {
		hashes[plat.Triple] = digest
	}
	lk.Record("postgres", spec, plat, engine.Pin{Resolved: full, Source: "mirror", Hashes: hashes})
	return engine.Toolchain{Engine: "postgres", Full: full, BinDir: binDir}, nil
}

// normalizeExact maps a real two-part Postgres version (16.14) to the three-part
// archive version (16.14.0); already three-part values pass through.
func normalizeExact(v string) string {
	if strings.Count(v, ".") == 1 {
		return v + ".0"
	}
	return v
}

// Provision implements engine.Driver.
func (Driver) Provision(ctx context.Context, inst engine.Instance, tc engine.Toolchain) error {
	cfg, err := pgConfig(inst)
	if err != nil {
		return err
	}
	return provision(ctx, inst, tc, cfg)
}

// Provisioned implements engine.Driver.
func (Driver) Provisioned(dataDir string) bool { return provisioned(dataDir) }

// Spawn implements engine.Driver.
func (Driver) Spawn(ctx context.Context, inst engine.Instance, tc engine.Toolchain) (engine.Process, error) {
	if err := os.MkdirAll(inst.SocketDir, 0o700); err != nil {
		return nil, fmt.Errorf("creating socket dir: %w", err)
	}
	if err := clearStaleLock(inst); err != nil {
		return nil, err
	}
	cmd := exec.Command(tc.Path("postgres"),
		"-D", inst.DataDir,
		"-k", inst.SocketDir,
		"-p", strconv.Itoa(inst.Port),
		"-c", "listen_addresses=", // unix socket only
	)
	return supervisor.Start(cmd)
}

// WaitReady implements engine.Driver: it polls pg_isready until the backend
// accepts connections, the process dies, or the boot timeout elapses.
func (Driver) WaitReady(ctx context.Context, inst engine.Instance, tc engine.Toolchain, p engine.Process) error {
	ctx, cancel := context.WithTimeout(ctx, bootTimeout)
	defer cancel()
	isready := tc.Path("pg_isready")
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if !p.Alive() {
			return fmt.Errorf("postgres for %q exited during startup:\n%s", inst.Name, strings.Join(p.Logs(), "\n"))
		}
		cmd := exec.CommandContext(ctx, isready, "-h", inst.SocketDir, "-p", strconv.Itoa(inst.Port), "-d", "postgres")
		if err := cmd.Run(); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("postgres for %q did not become ready within %s:\n%s", inst.Name, bootTimeout, strings.Join(p.Logs(), "\n"))
		case <-ticker.C:
		}
	}
}

// BackendSocket implements engine.Driver.
func (Driver) BackendSocket(socketDir string, port int) string {
	return filepath.Join(socketDir, fmt.Sprintf(".s.PGSQL.%d", port))
}

// BackendURL implements engine.BackendProvider: a libpq URL another local
// process (e.g. FerretDB) uses to connect directly to this instance's backend
// over its unix socket. The database is the instance name (created by converge).
func (Driver) BackendURL(inst engine.Instance) string {
	return fmt.Sprintf("postgres://postgres@/%s?host=%s", inst.Name, inst.SocketDir)
}

// ConnString implements engine.Driver.
func (Driver) ConnString(inst engine.Instance, ep engine.Endpoint) (string, string) {
	if ep.TCPAddr != "" {
		return "DATABASE_URL", fmt.Sprintf("postgres://postgres@%s/%s?sslmode=disable", ep.TCPAddr, inst.Name)
	}
	return "DATABASE_URL", fmt.Sprintf("postgres://postgres@/%s?host=%s", inst.Name, filepath.Dir(ep.UnixSocket))
}

// pgConfig extracts the Postgres config from an instance, defaulting if absent.
func pgConfig(inst engine.Instance) (*Config, error) {
	if inst.Spec == nil {
		return &Config{SharedBuffers: defaultSharedBuffers, MaxConnections: defaultMaxConnections}, nil
	}
	cfg, ok := inst.Spec.(*Config)
	if !ok {
		return nil, fmt.Errorf("instance %q: unexpected config type %T", inst.Name, inst.Spec)
	}
	return cfg, nil
}

// clearStaleLock refuses to double-start a running backend and clears a stale
// postmaster.pid (and orphaned socket) left by a crash.
func clearStaleLock(inst engine.Instance) error {
	lockPath := filepath.Join(inst.DataDir, "postmaster.pid")
	raw, err := os.ReadFile(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	lines := strings.SplitN(string(raw), "\n", 2)
	if pid, convErr := strconv.Atoi(strings.TrimSpace(lines[0])); convErr == nil && pid > 0 && supervisor.ProcessAlive(pid) {
		return fmt.Errorf("instance %q appears to already be running (pid %d); remove %s if you are sure it is not", inst.Name, pid, lockPath)
	}
	if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing stale lock: %w", err)
	}
	_ = os.Remove(filepath.Join(inst.SocketDir, fmt.Sprintf(".s.PGSQL.%d", inst.Port)))
	return nil
}
