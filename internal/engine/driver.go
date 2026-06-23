package engine

import (
	"bufio"
	"context"
	"crypto/tls"
	"io"
	"net"
	"time"

	"github.com/hashicorp/hcl/v2"
	"github.com/zclconf/go-cty/cty"
)

// SlowBooter is implemented by engines whose cold boot legitimately takes longer
// than the proxy's default client-boot budget — e.g. documentdb builds a Postgres
// cluster and runs CREATE EXTENSION … CASCADE on first boot. The proxy waits up to
// BootBudget (instead of its default) before giving up on a client-triggered boot.
// Once provisioned, later boots are quick and finish well within either bound.
type SlowBooter interface {
	BootBudget() time.Duration
}

// Driver is the minimal contract every database engine implements. The generic
// runtime depends only on these methods; richer behavior (convergence,
// protocol-aware proxying, copy-on-write templates) is discovered via the
// optional capability interfaces below using type assertions.
type Driver interface {
	// Type is the config block keyword and registry key, e.g. "postgres".
	Type() string

	// Resolve locates (or downloads) the toolchain for spec on plat, using fetch
	// to read the mirror and download archives, and recording the resolved pin
	// in lk. spec is normalized per engine.
	Resolve(ctx context.Context, spec VersionSpec, plat Platform, lk Locker, fetch Fetcher) (Toolchain, error)

	// Provision makes inst.DataDir ready to boot, running the engine's init step
	// if needed. It is idempotent.
	Provision(ctx context.Context, inst Instance, tc Toolchain) error

	// Provisioned reports whether dataDir already holds an initialized store.
	Provisioned(dataDir string) bool

	// Spawn starts the server bound to the instance's backend socket and returns
	// a running handle. It does not block on readiness.
	Spawn(ctx context.Context, inst Instance, tc Toolchain) (Process, error)

	// WaitReady blocks until the backend accepts connections, the process dies,
	// or ctx expires.
	WaitReady(ctx context.Context, inst Instance, tc Toolchain, p Process) error

	// BackendSocket returns the absolute path the proxy dials to reach a running
	// backend, given its socket directory and nominal port.
	BackendSocket(socketDir string, port int) string

	// ConnString builds the connection URL doze injects for a child process,
	// pointed at the doze-owned endpoint. envVar is the variable name family
	// (DATABASE_URL, REDIS_URL, MONGODB_URI).
	ConnString(inst Instance, ep Endpoint) (envVar, url string)
}

// Fetcher resolves and downloads engine toolchains from the mirror. The
// binaries package implements it; the runtime passes one to Driver.Resolve.
// Defined here (not imported from binaries) to keep the dependency one-way.
type Fetcher interface {
	// ResolveMajor returns the full version the mirror maps a major to.
	ResolveMajor(engineType, major string) (full string, err error)
	// Ensure makes the toolchain for (engineType, full) present and returns its
	// bin dir and verified "sha256:<hex>" digest. expectedSHA, when non-empty
	// (from the lockfile), must match.
	Ensure(ctx context.Context, engineType, full string, plat Platform, expectedSHA string) (binDir, digest string, err error)
}

// Process is a running backend process. The generic supervisor implements it.
type Process interface {
	PID() int
	Alive() bool
	Logs() []string
	Stop(ctx context.Context) error
	Wait() error
}

// Converger is implemented by engines that converge to a declared structural
// spec (roles, databases, schemas, grants, extensions). The runtime calls it
// only on a freshly provisioned instance (and on explicit `doze up`). Engines
// without structure (Valkey, Kvrocks) do not implement it.
type Converger interface {
	Converge(ctx context.Context, inst Instance, tc Toolchain, ep Endpoint) error
}

// Inventory is implemented by engines whose instances manage discrete structural
// objects, so `doze plan`/`apply`/`destroy` can track and diff them. Objects
// returns the objects the instance currently declares (derived from its config,
// no live query). Engines that implement Converger should usually implement this
// too; engines without structure (Valkey, Kvrocks) implement neither.
type Inventory interface {
	Objects(inst Instance) []Object
}

// Pruner is implemented by engines that can delete previously-applied objects no
// longer declared. The runtime calls Prune during apply (for objects removed from
// config) and destroy (for every applied object), passing the objects to drop.
type Pruner interface {
	Prune(ctx context.Context, inst Instance, tc Toolchain, ep Endpoint, removed []Object) error
}

// Attributer is implemented by engines that expose attributes beyond the generic
// baseline (name, engine, host, port, address, socket, url) under their
// <type>.<name> reference. The runtime/config merge these over the baseline when
// building the evaluation context, so config can reference e.g. postgres.x.owner
// or sqs.jobs.queues. Optional — engines without it expose only the baseline.
type Attributer interface {
	Attributes(inst Instance, ep Endpoint) map[string]cty.Value
}

// BackendProvider is implemented by engines that can serve as another instance's
// backend. BackendURL returns a URL a local process uses to connect directly to
// this instance's backend over its unix socket (not via the doze proxy).
type BackendProvider interface {
	BackendURL(inst Instance) string
}

// EnvProvider is implemented by engines that inject more than the single
// ConnString pair into a child's environment — e.g. the AWS services need an
// endpoint URL plus dummy credentials and a region. The returned variables are
// merged into the environment doze exports for `doze run`/`doze env`, in
// addition to whatever ConnString contributes.
type EnvProvider interface {
	Env(inst Instance, ep Endpoint) map[string]string
}

// Versionless is implemented by engines that ship inside the doze binary and
// therefore have no selectable version (the local-AWS services). config does not
// require a `version` for their instances.
type Versionless interface {
	Versionless()
}

// ConfigDecoder is implemented by drivers that decode their own config block
// body into an EngineConfig. config calls it for each block whose keyword
// matches a registered driver. baseDir is the config file's directory, for
// resolving relative paths (e.g. extension source bundles).
type ConfigDecoder interface {
	DecodeConfig(body hcl.Body, ctx *hcl.EvalContext, baseDir string) (EngineConfig, error)
}

// Templater is implemented by engines that support copy-on-write data-dir
// templates: provision once into a shared template, then clone per instance for
// instant cold boots and disposable databases. The runtime owns templateDir
// (keyed by engine + resolved version); the driver owns how it is built and
// cloned. Optional — engines without it are provisioned directly.
type Templater interface {
	// EnsureTemplate provisions templateDir if it does not already exist.
	EnsureTemplate(ctx context.Context, tc Toolchain, templateDir string) error
	// CloneTemplate materializes destDir as a (copy-on-write where possible)
	// clone of templateDir.
	CloneTemplate(ctx context.Context, templateDir, destDir string) error
}

// ProxyFilter is implemented by engines whose wire protocol needs handling on
// the splice path: reading a startup preamble, terminating TLS, and routing
// out-of-band control messages (e.g. the Postgres CancelRequest dance). Engines
// without it get the pure accept -> boot -> count -> splice path.
type ProxyFilter interface {
	// Preamble processes the initial client bytes before routing. It may upgrade
	// the connection to TLS (per opts) and buffers any startup bytes to replay
	// to the backend. If the connection is a terminal out-of-band control
	// request (e.g. a cancel), the filter handles it fully using reg and returns
	// Handled=true, after which the proxy neither boots nor splices.
	Preamble(ctx context.Context, client net.Conn, reg CancelRegistry, opts ProxyOpts) (PreambleResult, error)

	// Handshake observes the backend->client startup exchange on the spliced
	// pair, optionally rewriting it (e.g. swapping the backend cancel key for a
	// synthetic one registered in reg). It returns whether the stream is ready
	// to splice and a cleanup func the proxy defers (e.g. to unregister the key).
	Handshake(client net.Conn, backend *bufio.Reader, backendSocket string, reg CancelRegistry) (ready bool, cleanup func(), err error)
}

// ErrorWriter is implemented by engines that can encode a protocol-level error
// message, so the proxy can report a boot/dial failure cleanly instead of just
// dropping the connection. Optional.
type ErrorWriter interface {
	WriteError(w io.Writer, code, message string)
}

// ProxyOpts carries the proxy's TLS policy to a ProxyFilter.
type ProxyOpts struct {
	TLS        *tls.Config
	RequireTLS bool
	LocalUnix  bool // client connected over a unix socket (TLS-exempt)
}

// PreambleResult is what a ProxyFilter.Preamble returns to the proxy.
type PreambleResult struct {
	Client  net.Conn // possibly TLS-upgraded; the proxy splices on this
	Replay  []byte   // bytes to write to the backend before splicing
	Handled bool     // terminal (e.g. cancel handled) -> do not boot/splice
}

// CancelTarget identifies a backend for out-of-band cancellation.
type CancelTarget struct {
	BackendSocket string
	Key           []byte // opaque engine-specific real key (PG: pid+secret)
}

// CancelRegistry maps generated synthetic keys to real backend targets. The
// proxy owns one and passes it to ProxyFilter methods; only the filter uses it.
type CancelRegistry interface {
	// Register stores target under a freshly generated synthetic key (returned).
	Register(target CancelTarget) (synthetic []byte)
	Unregister(synthetic []byte)
	Lookup(synthetic []byte) (CancelTarget, bool)
}
