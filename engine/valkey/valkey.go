// Package valkey implements the doze engine.Driver for Valkey (a Redis-protocol,
// in-memory store). It implements only the required Driver methods — Valkey has
// no declared structure to converge and its RESP protocol needs no preamble, so
// it rides the generic accept -> boot -> splice -> count path with no
// ProxyFilter or Converger. It is the proof that the engine abstraction holds.
package valkey

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
	bootTimeout = 15 * time.Second
	envBinDir   = "DOZE_VALKEY_BINDIR"
	socketName  = "valkey.sock"
)

// Driver is the Valkey engine driver.
type Driver struct{}

// Type implements engine.Driver.
func (Driver) Type() string { return "valkey" }

// Resolve implements engine.Driver. Valkey versions are already three-part
// (e.g. 9.1.0), so an exact spec is used verbatim.
func (Driver) Resolve(ctx context.Context, spec engine.VersionSpec, plat engine.Platform, lk engine.Locker, fetch engine.Fetcher) (engine.Toolchain, error) {
	if dir := os.Getenv(envBinDir); dir != "" {
		return engine.Toolchain{Engine: "valkey", BinDir: dir, Full: spec.String()}, nil
	}
	full, expectedSHA := "", ""
	if pin, ok := lk.Get("valkey", spec, plat); ok && pin.Resolved != "" {
		full = pin.Resolved
		expectedSHA = pin.Hashes[plat.Triple]
	} else if spec.IsExact() {
		full = spec.String()
	} else {
		v, err := fetch.ResolveMajor("valkey", spec.String())
		if err != nil {
			return engine.Toolchain{}, err
		}
		full = v
	}
	binDir, digest, err := fetch.Ensure(ctx, "valkey", full, plat, expectedSHA)
	if err != nil {
		return engine.Toolchain{}, err
	}
	hashes := map[string]string{}
	if digest != "" {
		hashes[plat.Triple] = digest
	}
	lk.Record("valkey", spec, plat, engine.Pin{Resolved: full, Source: "mirror", Hashes: hashes})
	return engine.Toolchain{Engine: "valkey", Full: full, BinDir: binDir}, nil
}

// Provision implements engine.Driver: Valkey just needs a data directory.
func (Driver) Provision(_ context.Context, inst engine.Instance, _ engine.Toolchain) error {
	return os.MkdirAll(inst.DataDir, 0o700)
}

// Provisioned implements engine.Driver.
func (Driver) Provisioned(dataDir string) bool {
	fi, err := os.Stat(dataDir)
	return err == nil && fi.IsDir()
}

// Spawn implements engine.Driver: start valkey-server on a private unix socket
// with TCP disabled (port 0); doze splices clients to it.
func (Driver) Spawn(_ context.Context, inst engine.Instance, tc engine.Toolchain) (engine.Process, error) {
	if err := os.MkdirAll(inst.SocketDir, 0o700); err != nil {
		return nil, fmt.Errorf("creating socket dir: %w", err)
	}
	socket := socketPath(inst.SocketDir)
	_ = os.Remove(socket) // clear any stale socket from a crash

	args := []string{
		"--port", "0",
		"--unixsocket", socket,
		"--dir", inst.DataDir,
		"--save", "",
		"--appendonly", "no",
		"--daemonize", "no",
	}
	if cfg, ok := inst.Spec.(*Config); ok && cfg != nil {
		if cfg.Password != "" {
			args = append(args, "--requirepass", cfg.Password)
		}
		if cfg.Maxmemory != "" {
			args = append(args, "--maxmemory", cfg.Maxmemory)
		}
	}
	return supervisor.Start(exec.Command(tc.Path("valkey-server"), args...))
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
			return fmt.Errorf("valkey for %q exited during startup:\n%s", inst.Name, strings.Join(p.Logs(), "\n"))
		}
		if ping(socket) {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("valkey for %q did not become ready within %s:\n%s", inst.Name, bootTimeout, strings.Join(p.Logs(), "\n"))
		case <-ticker.C:
		}
	}
}

// ping reports whether a valkey backend answers on its socket. Any RESP reply
// ('+PONG', or '-NOAUTH' when a password is set) means the server is accepting.
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
