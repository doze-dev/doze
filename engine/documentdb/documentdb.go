// Package documentdb implements the doze engine.Driver for DocumentDB: a
// MongoDB-wire database that a developer connects to with any Mongo client, but
// which is, under the hood, two cooperating processes doze starts and hides:
//
//   - a private PostgreSQL 18 with Microsoft's DocumentDB extension chain
//     compiled in (the `documentdb` mirror artifact), which stores the data, and
//   - a FerretDB v2 gateway (the `ferretdb` mirror artifact) that speaks the
//     MongoDB wire protocol and translates it to documentdb_api calls.
//
// The user declares only `documentdb "name" {}` and connects over MONGODB_URI;
// Postgres and FerretDB are an implementation detail they never name or see.
// Because one declared instance owns BOTH processes, this driver is NOT a
// Dependent (there is no second declared instance to wire up): it provisions the
// Postgres data dir, spawns Postgres, creates the extension, then spawns FerretDB
// against it, and exposes a single composite Process the runtime supervises and
// reaps as one unit. The Mongo wire needs no preamble, so clients ride the
// generic splice path straight to FerretDB's unix socket.
package documentdb

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nerdmenot/doze/internal/engine"
	"github.com/nerdmenot/doze/internal/supervisor"
)

func init() { engine.Register(Driver{}) }

const (
	bootTimeout = 60 * time.Second

	// Pinned components. DocumentDB is a curated bundle, not a version the user
	// selects: the extension chain and the gateway are validated together, so we
	// fix both here (Postgres 18 is pinned inside the `documentdb` artifact).
	ddbVersion    = "0.112-0" // microsoft/documentdb extension release (PG18 + chain)
	ferretVersion = "2.7.0"   // FerretDB v2 gateway

	// Bindir overrides for local development against freshly-built binaries.
	envDDBBinDir    = "DOZE_DOCUMENTDB_BINDIR" // the Postgres+extension bundle
	envFerretBinDir = "DOZE_FERRETDB_BINDIR"   // the FerretDB gateway

	// mongoSocket is FerretDB's client-facing unix socket inside the instance's
	// socket dir — the address the doze proxy splices Mongo connections to.
	mongoSocket = "documentdb.sock"
)

// Driver is the DocumentDB composite engine driver.
type Driver struct{}

// Type implements engine.Driver.
func (Driver) Type() string { return "documentdb" }

// Versionless implements engine.Versionless: DocumentDB is a pinned bundle, so a
// `documentdb` block needs no `version`.
func (Driver) Versionless() {}

// BootBudget implements engine.SlowBooter: the first cold boot downloads the
// bundle, runs initdb, and CREATE EXTENSION documentdb CASCADE (PostGIS, pg_cron,
// vector, …), which easily exceeds the proxy's default client-boot budget. Later
// boots (cluster provisioned, extension already present) finish in seconds.
func (Driver) BootBudget() time.Duration { return 3 * time.Minute }

// Resolve implements engine.Driver. It resolves TWO toolchains — the Postgres+
// extension bundle (the primary BinDir) and the FerretDB gateway (stashed under
// Tools["ferretdb"]) — so Spawn can launch both from one Toolchain.
func (Driver) Resolve(ctx context.Context, _ engine.VersionSpec, plat engine.Platform, lk engine.Locker, fetch engine.Fetcher) (engine.Toolchain, error) {
	// Postgres + DocumentDB extension bundle.
	pgBin := os.Getenv(envDDBBinDir)
	if pgBin == "" {
		var err error
		pgBin, err = ensure(ctx, lk, fetch, plat, "documentdb", ddbVersion)
		if err != nil {
			return engine.Toolchain{}, err
		}
	}
	// FerretDB gateway.
	ferretBin := os.Getenv(envFerretBinDir)
	if ferretBin == "" {
		var err error
		ferretBin, err = ensure(ctx, lk, fetch, plat, "ferretdb", ferretVersion)
		if err != nil {
			return engine.Toolchain{}, err
		}
	}
	return engine.Toolchain{
		Engine: "documentdb",
		Full:   ddbVersion,
		BinDir: pgBin,
		Tools:  map[string]string{"ferretdb": filepath.Join(ferretBin, "ferretdb")},
	}, nil
}

// ensure resolves+downloads one pinned component, recording its pin so the
// lockfile freezes the exact artifacts this DocumentDB bundle was built from.
func ensure(ctx context.Context, lk engine.Locker, fetch engine.Fetcher, plat engine.Platform, eng, full string) (string, error) {
	spec := engine.VersionSpec(full)
	expectedSHA := ""
	if pin, ok := lk.Get(eng, spec, plat); ok && pin.Resolved != "" {
		full = pin.Resolved
		expectedSHA = pin.Hashes[plat.Triple]
	}
	binDir, digest, err := fetch.Ensure(ctx, eng, full, plat, expectedSHA)
	if err != nil {
		return "", err
	}
	hashes := map[string]string{}
	if digest != "" {
		hashes[plat.Triple] = digest
	}
	lk.Record(eng, spec, plat, engine.Pin{Resolved: full, Source: "mirror", Hashes: hashes})
	return binDir, nil
}

// Provision implements engine.Driver: initialize the private Postgres cluster
// (with the DocumentDB-required settings) under inst.DataDir/pgdata. FerretDB is
// stateless, so it needs only a state directory, created at spawn. Idempotent.
func (Driver) Provision(ctx context.Context, inst engine.Instance, tc engine.Toolchain) error {
	return provision(ctx, inst, tc)
}

// Provisioned implements engine.Driver.
func (Driver) Provisioned(dataDir string) bool { return provisioned(dataDir) }

// Spawn implements engine.Driver: start Postgres on a private socket + loopback
// TCP (the extension self-connects over TCP), wait for it, create the DocumentDB
// extension, then start FerretDB pointed at it. Returns a composite Process that
// the runtime supervises and reaps as one unit.
func (Driver) Spawn(ctx context.Context, inst engine.Instance, tc engine.Toolchain) (engine.Process, error) {
	pgData := pgDataDir(inst.DataDir)
	pgSock := pgSocketDir(inst.SocketDir)
	if err := os.MkdirAll(pgSock, 0o700); err != nil {
		return nil, fmt.Errorf("creating postgres socket dir: %w", err)
	}
	if err := clearStaleLock(inst, pgData, pgSock); err != nil {
		return nil, err
	}
	// The DocumentDB extension opens libpq connections back to its own server over
	// loopback TCP, so the backend must bind a real (unique) localhost port; and
	// FerretDB always binds a debug/metrics handler. Allocate both in our high
	// window up front, distinct from each other.
	port, err := freePort()
	if err != nil {
		return nil, fmt.Errorf("allocating postgres port: %w", err)
	}
	debugPort, err := freePort(port)
	if err != nil {
		return nil, fmt.Errorf("allocating ferretdb debug port: %w", err)
	}

	pgCmd := exec.Command(tc.Path("postgres"),
		"-D", pgData,
		"-k", pgSock,
		"-p", strconv.Itoa(port),
	)
	pg, err := supervisor.Start(pgCmd)
	if err != nil {
		return nil, fmt.Errorf("starting postgres: %w", err)
	}
	// From here on, any failure must stop the Postgres we just started.
	if err := waitPostgres(ctx, tc, pgSock, port, pg); err != nil {
		_ = pg.Stop(context.Background())
		return nil, err
	}
	if err := createExtension(ctx, tc, pgSock, port); err != nil {
		_ = pg.Stop(context.Background())
		return nil, fmt.Errorf("documentdb %q: %w", inst.Name, err)
	}

	stateDir := filepath.Join(inst.DataDir, "ferretdb")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		_ = pg.Stop(context.Background())
		return nil, fmt.Errorf("creating ferretdb state dir: %w", err)
	}
	socket := BackendSocketPath(inst.SocketDir)
	_ = os.Remove(socket)

	ferretCmd := exec.Command(tc.Path("ferretdb"))
	ferretCmd.Env = append(os.Environ(),
		"FERRETDB_POSTGRESQL_URL="+backendURL(pgSock, port),
		"FERRETDB_LISTEN_UNIX="+socket,
		"FERRETDB_LISTEN_ADDR=", // no TCP listener; doze fronts the mongo wire
		// Pin the debug/metrics handler to a port in doze's high window. Left unset,
		// FerretDB hardwires it to 127.0.0.1:8088, so a second documentdb instance
		// would crash-loop on "address already in use".
		"FERRETDB_DEBUG_ADDR=127.0.0.1:"+strconv.Itoa(debugPort),
		"FERRETDB_STATE_DIR="+stateDir,
		"FERRETDB_TELEMETRY=disable",
		// Local-dev posture: no Mongo-client auth. FerretDB reaches the private
		// Postgres over its unix socket with local trust, and doze only ever binds
		// the mongo endpoint to the user's chosen (loopback) address — so a client
		// just connects with MONGODB_URI and no credentials, like every other doze
		// engine. Mongo wire auth would otherwise require provisioning SCRAM verifiers
		// as Postgres roles for each client.
		"FERRETDB_AUTH=false",
	)
	ferret, err := supervisor.Start(ferretCmd)
	if err != nil {
		_ = pg.Stop(context.Background())
		return nil, fmt.Errorf("starting ferretdb: %w", err)
	}
	return newComposite(ferret, pg), nil
}

// WaitReady implements engine.Driver: ready once FerretDB's mongo socket accepts
// connections. Postgres readiness was already established inside Spawn.
func (Driver) WaitReady(ctx context.Context, inst engine.Instance, _ engine.Toolchain, p engine.Process) error {
	ctx, cancel := context.WithTimeout(ctx, bootTimeout)
	defer cancel()
	socket := BackendSocketPath(inst.SocketDir)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if !p.Alive() {
			return fmt.Errorf("documentdb for %q exited during startup:\n%s", inst.Name, strings.Join(p.Logs(), "\n"))
		}
		if conn, err := net.DialTimeout("unix", socket, 500*time.Millisecond); err == nil {
			_ = conn.Close()
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("documentdb for %q did not become ready within %s:\n%s", inst.Name, bootTimeout, strings.Join(p.Logs(), "\n"))
		case <-ticker.C:
		}
	}
}

// BackendSocket implements engine.Driver: the proxy splices mongo clients to
// FerretDB's unix socket.
func (Driver) BackendSocket(socketDir string, _ int) string { return BackendSocketPath(socketDir) }

// ConnString implements engine.Driver.
func (Driver) ConnString(_ engine.Instance, ep engine.Endpoint) (string, string) {
	host := ep.TCPAddr
	if host == "" {
		host = "localhost"
	}
	return "MONGODB_URI", "mongodb://" + host + "/"
}

// BackendSocketPath is FerretDB's mongo socket inside socketDir.
func BackendSocketPath(socketDir string) string { return filepath.Join(socketDir, mongoSocket) }

func pgDataDir(dataDir string) string     { return filepath.Join(dataDir, "pgdata") }
func pgSocketDir(socketDir string) string { return filepath.Join(socketDir, "pg") }

// backendURL is the libpq URL FerretDB (and our convergence psql) use to reach
// the private Postgres over its unix socket.
func backendURL(pgSock string, port int) string {
	return fmt.Sprintf("postgres://postgres@/postgres?host=%s&port=%d", pgSock, port)
}

// doze keeps documentdb's internal loopback ports — the Postgres port the
// extension self-connects to, and FerretDB's debug/metrics handler — inside one
// high, fixed window. That sits well clear of the low-numbered defaults real
// services use (FerretDB otherwise hardwires its debug handler to :8088, which
// collides the moment a second documentdb instance boots), and is easy to spot
// in `lsof`/logs as "doze's private ports".
const (
	portLo = 30000
	portHi = 40000
)

// freePort returns an unused loopback TCP port in [portLo, portHi], skipping any
// port in exclude (so a caller allocating several at once keeps them distinct).
// It probes random ports in the window, so two instances booting concurrently are
// unlikely to choose the same one. There is a small TOCTOU window before the port
// is bound; acceptable for local dev and far simpler than a port registry — the
// server logs loudly if it loses the race.
func freePort(exclude ...int) (int, error) {
	excluded := func(p int) bool {
		for _, e := range exclude {
			if p == e {
				return true
			}
		}
		return false
	}
	const span = portHi - portLo + 1
	for i := 0; i < 128; i++ {
		p := portLo + rand.IntN(span)
		if excluded(p) {
			continue
		}
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err != nil {
			continue // in use — try another
		}
		_ = l.Close()
		return p, nil
	}
	return 0, fmt.Errorf("no free loopback port in %d-%d", portLo, portHi)
}

// waitPostgres polls pg_isready until the backend accepts connections, the
// process dies, or the boot timeout elapses.
func waitPostgres(ctx context.Context, tc engine.Toolchain, sockDir string, port int, p engine.Process) error {
	ctx, cancel := context.WithTimeout(ctx, bootTimeout)
	defer cancel()
	isready := tc.Path("pg_isready")
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if !p.Alive() {
			return fmt.Errorf("documentdb postgres exited during startup:\n%s", strings.Join(p.Logs(), "\n"))
		}
		cmd := exec.CommandContext(ctx, isready, "-h", sockDir, "-p", strconv.Itoa(port), "-d", "postgres")
		if err := cmd.Run(); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("documentdb postgres did not become ready within %s:\n%s", bootTimeout, strings.Join(p.Logs(), "\n"))
		case <-ticker.C:
		}
	}
}

// createExtension installs the DocumentDB extension chain (documentdb +, via
// CASCADE, documentdb_core, pg_cron, vector, postgis, …) into the backend's
// postgres database. Idempotent and run on every boot so FerretDB always finds
// documentdb_api present before it connects.
func createExtension(ctx context.Context, tc engine.Toolchain, sockDir string, port int) error {
	cmd := exec.CommandContext(ctx, tc.Path("psql"),
		"-h", sockDir, "-p", strconv.Itoa(port), "-U", "postgres", "-d", "postgres",
		"-v", "ON_ERROR_STOP=1", "-X", "-q",
		"-c", "CREATE EXTENSION IF NOT EXISTS documentdb CASCADE;",
	)
	var out strings.Builder
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(out.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("creating documentdb extension: %s", msg)
	}
	return nil
}

// clearStaleLock refuses to double-start a running backend and clears a stale
// postmaster.pid (and orphaned socket) left by a crash.
func clearStaleLock(inst engine.Instance, pgData, pgSock string) error {
	lockPath := filepath.Join(pgData, "postmaster.pid")
	raw, err := os.ReadFile(lockPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	lines := strings.SplitN(string(raw), "\n", 2)
	if pid, convErr := strconv.Atoi(strings.TrimSpace(lines[0])); convErr == nil && pid > 0 && supervisor.ProcessAlive(pid) {
		return fmt.Errorf("documentdb %q appears to already be running (pid %d); remove %s if you are sure it is not", inst.Name, pid, lockPath)
	}
	if err := os.Remove(lockPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing stale lock: %w", err)
	}
	// best-effort: drop any orphaned unix socket files
	if entries, err := os.ReadDir(pgSock); err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".s.PGSQL.") {
				_ = os.Remove(filepath.Join(pgSock, e.Name()))
			}
		}
	}
	return nil
}

// composite is the engine.Process for one DocumentDB instance: the FerretDB
// gateway (user-facing) plus its private Postgres backend. The runtime treats it
// as a single process — Alive only while BOTH are up, and if either exits on its
// own the other is torn down so a half-dead instance cleanly re-boots on the
// next connection.
type composite struct {
	gateway engine.Process // ferretdb — its PID is the instance's PID
	backend engine.Process // postgres
	exited  chan error
	stop    sync.Once
}

func newComposite(gateway, backend engine.Process) *composite {
	c := &composite{gateway: gateway, backend: backend, exited: make(chan error, 1)}
	// If either side exits unexpectedly, stop the other and surface the exit.
	watch := func(dead, other engine.Process) {
		err := dead.Wait()
		_ = other.Stop(context.Background())
		select {
		case c.exited <- err:
		default:
		}
	}
	go watch(gateway, backend)
	go watch(backend, gateway)
	return c
}

// PID reports the FerretDB gateway pid — the mongo-facing process.
func (c *composite) PID() int { return c.gateway.PID() }

// Alive is true only while both processes run.
func (c *composite) Alive() bool { return c.gateway.Alive() && c.backend.Alive() }

// Logs interleaves backend then gateway logs (backend boot precedes gateway).
func (c *composite) Logs() []string {
	return append(append([]string{}, c.backend.Logs()...), c.gateway.Logs()...)
}

// Stop tears down the gateway first, then the backend.
func (c *composite) Stop(ctx context.Context) error {
	gErr := c.gateway.Stop(ctx)
	bErr := c.backend.Stop(ctx)
	if gErr != nil {
		return gErr
	}
	return bErr
}

// Wait returns once either process has exited (the runtime then reaps the pair).
func (c *composite) Wait() error { return <-c.exited }
