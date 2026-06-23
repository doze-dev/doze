// Package kvrocks implements the doze engine.Driver for Apache Kvrocks, a
// Redis-protocol store backed by RocksDB. Like Valkey it implements only the
// required Driver methods — no Converger, no ProxyFilter, and no Templater
// (RocksDB initializes lazily, so there is no init step worth templating) — so
// it rides the generic boot -> splice -> count -> reap path unchanged.
package kvrocks

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nerdmenot/doze/internal/engine"
	"github.com/nerdmenot/doze/internal/supervisor"
)

func init() { engine.Register(Driver{}) }

const (
	bootTimeout = 20 * time.Second
	envBinDir   = "DOZE_KVROCKS_BINDIR"
	socketName  = "kvrocks.sock"
)

// Driver is the Kvrocks engine driver.
type Driver struct{}

// Type implements engine.Driver.
func (Driver) Type() string { return "kvrocks" }

// Resolve implements engine.Driver. Kvrocks versions are three-part (2.15.0),
// so an exact spec is used verbatim.
func (Driver) Resolve(ctx context.Context, spec engine.VersionSpec, plat engine.Platform, lk engine.Locker, fetch engine.Fetcher) (engine.Toolchain, error) {
	if dir := os.Getenv(envBinDir); dir != "" {
		return engine.Toolchain{Engine: "kvrocks", BinDir: dir, Full: spec.String()}, nil
	}
	full, expectedSHA := "", ""
	if pin, ok := lk.Get("kvrocks", spec, plat); ok && pin.Resolved != "" {
		full = pin.Resolved
		expectedSHA = pin.Hashes[plat.Triple]
	} else if spec.IsExact() {
		full = spec.String()
	} else {
		v, err := fetch.ResolveMajor("kvrocks", spec.String())
		if err != nil {
			return engine.Toolchain{}, err
		}
		full = v
	}
	binDir, digest, err := fetch.Ensure(ctx, "kvrocks", full, plat, expectedSHA)
	if err != nil {
		return engine.Toolchain{}, err
	}
	hashes := map[string]string{}
	if digest != "" {
		hashes[plat.Triple] = digest
	}
	lk.Record("kvrocks", spec, plat, engine.Pin{Resolved: full, Source: "mirror", Hashes: hashes})
	return engine.Toolchain{Engine: "kvrocks", Full: full, BinDir: binDir}, nil
}

// Provision implements engine.Driver: Kvrocks just needs a data directory;
// RocksDB initializes its files on first start.
func (Driver) Provision(_ context.Context, inst engine.Instance, _ engine.Toolchain) error {
	return os.MkdirAll(inst.DataDir, 0o700)
}

// Provisioned implements engine.Driver.
func (Driver) Provisioned(dataDir string) bool {
	fi, err := os.Stat(dataDir)
	return err == nil && fi.IsDir()
}

// Spawn implements engine.Driver: start kvrocks with a generated config that
// binds a private unix socket (TCP disabled); doze splices clients to it.
func (Driver) Spawn(_ context.Context, inst engine.Instance, tc engine.Toolchain) (engine.Process, error) {
	if err := os.MkdirAll(inst.SocketDir, 0o700); err != nil {
		return nil, fmt.Errorf("creating socket dir: %w", err)
	}
	socket := socketPath(inst.SocketDir)
	_ = os.Remove(socket)

	confPath := filepath.Join(inst.DataDir, "kvrocks.conf")
	if err := writeConf(confPath, inst, socket); err != nil {
		return nil, err
	}
	return supervisor.Start(exec.Command(tc.Path("kvrocks"), "-c", confPath))
}

func writeConf(path string, inst engine.Instance, socket string) error {
	var b strings.Builder
	b.WriteString("# Managed by doze — regenerated on every boot.\n")
	fmt.Fprintf(&b, "dir %s\n", inst.DataDir)
	// Serve only over the unix socket. Kvrocks (unlike Redis/Valkey) rejects
	// `port 0` as out-of-range, so disable the TCP listener by binding no
	// address; `port` must still be a valid number but goes unused.
	b.WriteString("bind\n")
	b.WriteString("port 6666\n")
	fmt.Fprintf(&b, "unixsocket %s\n", socket)
	if cfg, ok := inst.Spec.(*Config); ok && cfg != nil {
		if cfg.Password != "" {
			fmt.Fprintf(&b, "requirepass %s\n", cfg.Password)
		}
		if cfg.Workers > 0 {
			fmt.Fprintf(&b, "workers %d\n", cfg.Workers)
		}
		// Raw kvrocks.conf passthrough, before namespaces so it can't clobber them.
		for _, k := range sortedKeys(cfg.Settings) {
			fmt.Fprintf(&b, "%s %s\n", k, cfg.Settings[k])
		}
		for _, ns := range cfg.Namespaces {
			fmt.Fprintf(&b, "namespace.%s %s\n", ns.Name, ns.Token)
		}
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

// sortedKeys returns the keys of m in deterministic order.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// WaitReady implements engine.Driver: poll the unix socket with a RESP PING.
func (Driver) WaitReady(ctx context.Context, inst engine.Instance, _ engine.Toolchain, p engine.Process) error {
	ctx, cancel := context.WithTimeout(ctx, bootTimeout)
	defer cancel()
	socket := socketPath(inst.SocketDir)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if !p.Alive() {
			return fmt.Errorf("kvrocks for %q exited during startup:\n%s", inst.Name, strings.Join(p.Logs(), "\n"))
		}
		if ping(socket) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("kvrocks for %q did not become ready within %s:\n%s", inst.Name, bootTimeout, strings.Join(p.Logs(), "\n"))
		case <-ticker.C:
		}
	}
}

// ping reports whether a kvrocks backend answers on its socket. Any RESP reply
// ('+PONG', or '-NOAUTH' when a password is set) means it is accepting.
func ping(socket string) bool {
	conn, err := net.DialTimeout("unix", socket, 500*time.Millisecond)
	if err != nil {
		return false
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(500 * time.Millisecond))
	if _, err := conn.Write([]byte("PING\r\n")); err != nil {
		return false
	}
	buf := make([]byte, 1)
	if _, err := conn.Read(buf); err != nil {
		return false
	}
	return buf[0] == '+' || buf[0] == '-'
}

// BackendSocket implements engine.Driver.
func (Driver) BackendSocket(socketDir string, _ int) string { return socketPath(socketDir) }

func socketPath(socketDir string) string { return filepath.Join(socketDir, socketName) }

// ConnString implements engine.Driver.
func (Driver) ConnString(inst engine.Instance, ep engine.Endpoint) (string, string) {
	auth := ""
	if cfg, ok := inst.Spec.(*Config); ok && cfg != nil && cfg.Password != "" {
		auth = ":" + cfg.Password + "@"
	}
	host := ep.TCPAddr
	if host == "" {
		host = "localhost"
	}
	return "REDIS_URL", "redis://" + auth + host + "/0"
}
