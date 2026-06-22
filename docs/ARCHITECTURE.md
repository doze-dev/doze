# Architecture

doze is a single Go binary that is both a CLI and a long-running daemon. The
daemon is a thin proxy in front of many real backing-service instances — one per
declared block — that it boots on demand and reaps when idle. Most are unmodified
database binaries (Postgres, Valkey, Kvrocks, FerretDB); the local **AWS
services (S3, SQS, SNS)** are built into the doze binary and run as self-exec'd
child processes behind the same proxy.

Its shape is two layers: a **generic, engine-agnostic core** and a **driver per
engine** behind a small interface. Adding an engine is a driver package plus one
registration line; the core never changes.

```
        ┌───────────────────────── doze serve (daemon) ─────────────────────────┐
clients │  proxy (one listener per instance)                                      │
  │     │     │ accept ─▶ runtime.Boot ─▶ driver ──┬─ Resolve  (toolchain)        │
  └─────▶     │  [optional ProxyFilter: TLS/        ├─ Provision (init / clone)    │
              │   startup/cancel for Postgres]      ├─ Spawn + WaitReady           │
              │     ▼                               └─ Converge  (Postgres only)   │
              │  splice ──▶ backend @instance (unix socket)                        │
        │     │  reaper (idle, by connection count)   control IPC   pidfile/log    │
        └───────────────────────────────────────────────────────────────────────┘
```

## The driver contract (`internal/engine`)

Every engine implements a small required interface; richer behavior is opt-in
via capability interfaces the core discovers with a type assertion. So Valkey
implements ~8 methods, while Postgres adds convergence, a protocol filter, and
templating — without the core knowing which is which.

```go
type Driver interface {
    Type() string
    Resolve(ctx, spec, plat, lock, fetch) (Toolchain, error) // fetch/cache binaries
    Provision(ctx, inst, tc) error                            // init the data dir (idempotent)
    Provisioned(dataDir) bool
    Spawn(ctx, inst, tc) (Process, error)                     // start the backend
    WaitReady(ctx, inst, tc, p) error                         // engine-specific health probe
    BackendSocket(socketDir, port) string                     // path the proxy dials
    ConnString(inst, ep) (envVar, url string)                 // for doze run/env
}

// Optional, discovered by type assertion:
type Converger       interface { Converge(...) error }            // roles/db/schema/grants (Postgres)
type ProxyFilter     interface { Preamble(...); Handshake(...) }  // startup/TLS/cancel (Postgres)
type Templater       interface { EnsureTemplate(...); CloneTemplate(...) } // CoW (Postgres)
type Dependent       interface { DependsOn(inst) []string }       // FerretDB → Postgres; SNS → SQS
type BackendProvider interface { BackendURL(inst) string }        // Postgres as a backend
type ConfigDecoder   interface { DecodeConfig(body, ctx, baseDir) (EngineConfig, error) }
type ErrorWriter     interface { WriteError(w, code, message) }   // protocol-clean errors
type EnvProvider     interface { Env(inst, ep) map[string]string } // extra env (AWS creds/region)
type Versionless     interface { Versionless() }                  // built-in services need no `version`
```

### Built-in services (the local-AWS engines)

S3, SQS, and SNS reuse this contract via a shared `awslocal.BaseDriver`. They have
no toolchain to download, so `Resolve` returns a synthetic toolchain and `Spawn`
re-executes the doze binary as a hidden `doze __serve <service>` worker that runs
the service (a `net/http` handler) on a private unix socket — full process
isolation, reusing the supervisor/proxy/reaper unchanged. They are `Versionless`,
`EnvProvider`s (inject `AWS_ENDPOINT_URL_<svc>` + dummy creds), and `Converger`s
(create declared buckets/queues/topics). SNS is also `Dependent` on its SQS
instance, and the `BaseDriver.ChildEnv` hook passes that instance's backend socket
to the SNS worker for fanout.

## Request flow (the core loop)

1. **proxy** accepts a connection on an instance's listener — so the target is
   known without parsing anything. If the driver is a `ProxyFilter` (Postgres),
   it first handles the SSL/TLS preamble, buffers the `StartupMessage`, and
   routes out-of-band `CancelRequest`s; otherwise the bytes pass straight through.
2. It calls **runtime**`.Boot(name)`. Concurrent cold boots coalesce via
   `singleflight`. If the instance is `Dependent`, its dependencies are booted
   and **held** first.
3. The cold path runs the driver: `Resolve` a toolchain (**binaries**),
   `Provision` the data dir (Postgres clones a version-keyed `initdb` **template**
   copy-on-write; others just `mkdir`), `Spawn` the backend on a private unix
   socket, and `WaitReady`. On first provision a `Converger` (Postgres) applies
   the declared structure.
4. The proxy dials the backend socket and **splices** the two connections
   byte-for-byte. **registry** counts the live connection.
5. The **reaper** reaps an instance only after `idle_timeout` at **zero
   connections** — never on query inactivity, so pools holding idle connections
   are never severed.

## Packages

| Package | Responsibility |
|---|---|
| `cmd/doze` | CLI (cobra); blank-imports `engine/*` to register the drivers. |
| `internal/engine` | The driver contract only: `Driver` + `Process` + capability interfaces + the driver registry. No engine code. |
| `internal/runtime` | Engine-agnostic orchestration: `singleflight` boot, dependency boot+hold, idle reaper, the instance state machine (`internal/registry`). |
| `internal/proxy` | One listener per instance, byte splice, the cancel registry, and optional `ProxyFilter` dispatch. |
| `internal/supervisor` | Generic process lifecycle: spawn, clean stop (SIGINT→SIGQUIT→SIGKILL), parent-death cleanup. Implements `engine.Process`. |
| `internal/binaries` | The multi-engine mirror manifest, content-addressed cache, and the `doze.lock` lockfile. |
| `internal/config` | HCL → an engine-agnostic root plus per-engine block dispatch to each driver's `ConfigDecoder`. Merges `doze.hcl` + `doze.d/*.hcl` (or a `--config` directory); reports validation errors with file/line/snippet and "did you mean" hints. |
| `internal/endpoints` | Per-instance client addresses, connection strings, and `.doze/endpoints.yaml`. |
| `internal/ui` | Shared, color-gated CLI/TUI vocabulary: palette, state coloring, ANSI-aware table, cross-platform RAM, uptime. Plain when piped or `NO_COLOR`. |
| `internal/control` | Newline-delimited JSON admin IPC over a unix socket. |
| `internal/daemon` | Wires runtime + per-instance proxy listeners + reaper + control into `doze serve`. |
| `internal/tui` | Charm Bubble Tea dashboard. |
| `engine/postgres` | The Postgres driver: cluster (`initdb`/conf/hba), convergence, extensions, the startup/TLS/cancel `ProxyFilter`, CoW `Templater`, `BackendProvider`. |
| `engine/valkey`, `engine/kvrocks` | Redis-protocol drivers (required methods only). |
| `engine/ferretdb` | MongoDB-wire driver; `Dependent` on a Postgres backend. |
| `engine/s3`, `engine/sqs`, `engine/sns` | Local-AWS drivers: `ConfigDecoder` + `Converger` over `awslocal.BaseDriver` (self-exec). `sns` is `Dependent` on its `sqs` instance. |
| `internal/awslocal` | The self-exec harness: `BaseDriver`, the unix-socket serve loop + health endpoint + service-factory registry (`doze __serve`), and shared AWS identity/ARN helpers. |
| `internal/s3srv` | S3 server: gofakes3 over a bbolt store. |
| `internal/sqssrv` | Ground-up SQS server: both wire protocols, bbolt store, visibility/long-poll/FIFO/DLQ, notifier-based receive. |
| `internal/snssrv` | Ground-up SNS server: Query/XML, topics/subscriptions, filter policies, SQS + http(s) delivery. |

## Key design decisions

- **Real binaries, one instance per database.** True concurrency and full
  fidelity for free; near-zero protocol code.
- **Generic core, drivers behind a tiny interface.** Optional capabilities via
  type assertion keep simple engines simple and the core engine-blind.
- **Per-instance listeners.** The listener identifies the instance, so routing
  needs no protocol parsing; protocol awareness is a per-engine opt-in.
- **Reap on connection count, never on query inactivity.** Connection pools hold
  idle connections open and must not be severed.
- **Instance dependencies are first-class.** A dependent boots and holds its
  dependencies (e.g. FerretDB → Postgres, SNS → SQS), releasing them on stop.
- **Built-in services, no new runtime.** S3/SQS/SNS ship inside the binary and
  self-exec as `doze __serve` children, so "local AWS" needs no Docker, no JVM,
  and no LocalStack — and reuses the same boot/splice/reap path as everything else.
- **Copy-on-write templates.** `initdb` runs once per version; instances clone it.
- **Shared tool store, per-project state.** Binaries live once under
  `~/.doze/<engine>`; each project's clusters/runtime are namespaced.
- **Self-healing for local dev.** A backend that exits unexpectedly is detected
  (a per-backend watcher) and marked reaped so the next connect re-boots cleanly;
  failures are recorded (`LastError`) and surfaced in `status`/`doctor`; daemon
  shutdown is bounded so it can't hang; and because macOS lacks `PDEATHSIG`, each
  backend's pid is recorded so a restart can reclaim orphans from a prior crash.
