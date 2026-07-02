# Architecture

doze is a single Go binary that is both a CLI and a long-running daemon. The
daemon is a thin proxy in front of many real backing-service instances — one per
declared block — that it boots on demand and reaps when idle.

Its shape is two layers: a **generic, engine-blind core** (this repo) and a
**driver per engine** behind a small interface. The core compiles in exactly one
driver — `process`, the supervision primitive for host applications. Every other
engine (postgres, valkey, kvrocks, ferret, mariadb, temporal, s3, sqs, sns) is
an **out-of-process plugin module**: a separate binary implementing the
[doze-sdk](https://github.com/doze-dev/doze-sdk) driver contract over gRPC
(HashiCorp go-plugin), fetched from the signed registry, pinned in `doze.lock`,
and kept warm for the daemon's lifetime. Adding an engine is publishing a
module; the core never changes.

```
        ┌───────────────────────── doze daemon process ──────────────────────────┐
clients │  proxy (one listener per instance)                                      │
  │     │     │ accept ─▶ runtime.Boot ─▶ driver ──┬─ Resolve  (engine binary)    │
  └─────▶     │  [optional wire filter: TLS/        ├─ Provision (init / clone)    │
              │   startup/cancel, e.g. Postgres —   ├─ Plan → supervised spawn     │
              │   runs IN the plugin via fd handoff]└─ Converge (structural)      │
              │     ▼                                                             │
              │  splice ──▶ backend @instance (unix socket)                       │
        │     │  reaper (idle, by connection count)   control IPC   pidfile/log   │
        └────────│────────────────────────────────────────────────────────────────┘
                 │ gRPC (go-plugin)
        ┌────────▼─────────┐   fetched from the signed registry, one process per
        │  engine modules   │   engine type: postgres-plugin, valkey-plugin, …
        └───────────────────┘
```

## The five repos

| Repo | Role |
|---|---|
| **doze** (this one) | The core: CLI, daemon, config evaluator, proxy, supervisor, runtime, module fetcher. Engine-blind. |
| **doze-sdk** | The public contract: `engine.Driver` + capability interfaces, the go-plugin gRPC transport, the `modindex` signed-index schema + selection policy, the `binaries` fetch/verify/cache library, the `modtool` release toolchain, and the `enginetest` harness. |
| **doze-modules** | The official engine modules, one Go package + tiny `plugin/main.go` each. Built/released by `dzm` (a thin loop over `modtool`). |
| **doze-registry** | The signed discovery layer (static files on Cloudflare Pages): per-namespace ed25519 keys, per-module signed indexes, the catalog, and the browsable docs site. |
| **doze-binaries** | The engine-binaries mirror: real upstream Postgres/Valkey/… built from source, append-only rolling releases. |

## The driver contract (doze-sdk/engine)

Every engine implements a small required interface — `Type`, `Resolve`
(fetch/pin the engine binary), `Provision`, `Provisioned`, `BackendSocket`,
`ConnString`, plus a run path (`Spawner.Plan`, a declarative spawn plan the core
executes and supervises). Richer behavior is opt-in via capability interfaces
discovered by type assertion — so valkey implements the minimum, while postgres
adds convergence (`Converger`/`Inventory`/`Pruner`), a wire-protocol filter
(TLS/startup/cancel), and copy-on-write templates (`Templater`) without the core
knowing which is which.

For plugins, each capability is advertised over a `Capabilities()` RPC and
adapted back so the runtime's type assertions keep working; config crosses the
boundary as gob, decoded *by the plugin* (`RemoteDecoder`) so the core never
learns engine schemas. The declared engine version travels with the decode so
modules reject version-gated arguments at lint time (`engine.RequireVersion`).
Wire-protocol engines receive the client connection's file descriptor via
SCM_RIGHTS and run the preamble/handshake/splice in their own process.

## Two version axes

The **engine version** is user-declared (`version = 18`) and resolved by the
module against the doze-binaries mirror. The **module version** is the plugin
binary's own release, chosen by the core: the newest release in the module's
signed index that speaks this doze's plugin protocol and supports every declared
engine major, preferring the `stable` channel head. Both pin into `doze.lock`
(engines / modules / publisher-key layers); pinned resolution is offline, and
pins move only via `doze modules upgrade`. Selection lives in
`doze-sdk/modindex.Select`; trust is the index-level ed25519 signature + per-
artifact signatures + TOFU key pinning.

## Request flow (the core loop)

1. **proxy** accepts a connection on an instance's listener — the listener
   identifies the instance, so routing parses nothing. A wire-filter engine
   (Postgres) first handles TLS/startup/cancel — inside its plugin, via fd
   handoff; other engines' bytes pass straight through.
2. It calls **runtime**`.Boot(name)`. Concurrent cold boots coalesce via
   `singleflight`. Dependencies (e.g. SNS → SQS) boot and are **held** first.
3. The cold path runs the driver over gRPC: `Resolve` the engine binary
   (mirror → content-addressed cache, lock-pinned), `Provision` the data dir
   (Postgres clones a version-keyed `initdb` template copy-on-write), run the
   `Plan` under the core's supervisor gated on its readiness probe, and on first
   provision `Converge` the declared structure (roles/schemas/buckets/queues).
4. The proxy dials the backend socket and **splices** byte-for-byte;
   **registry** counts the live connection.
5. The **reaper** reaps only after `idle_timeout` at **zero connections** —
   never on query inactivity, so pools holding idle connections survive.

## Packages (this repo)

| Package | Responsibility |
|---|---|
| `cmd/doze` | CLI (cobra); wires the module fetcher + plugin manager into `engine.SetPluginResolver` and the config hooks. |
| `engine/process` | The one compiled-in driver: supervised host applications (hooks, health, restart policy, port binding). |
| `internal/config` | HCL → engine-agnostic root + per-block plugin decode. Two-phase evaluator: variables/locals/outputs, `for_each`/`count`, typed cross-instance references (the dependency graph), multi-file merge, positioned diagnostics with did-you-mean (catalog-backed for engine types). Feeds declared engine versions into module selection and validates every block against the pinned module's engine support. |
| `internal/modules` | The module fetcher: signed-index fetch + verification, release selection, TOFU key pinning, lock pins, `upgrade`, the registry catalog (`search`/`docs`/`info`). |
| `internal/runtime` | Engine-agnostic orchestration: `singleflight` boot, dependency boot+hold, idle reaper, the instance state machine (`internal/registry`), plan/apply/destroy. |
| `internal/proxy` | One listener per instance, byte splice, cancel registry, wire-filter dispatch (fd handoff to plugins). |
| `internal/supervisor` | Generic process lifecycle: spawn, clean stop (SIGINT→SIGQUIT→SIGKILL), parent-death cleanup, ring log buffer. |
| `internal/daemon` | Wires runtime + listeners + reaper + control IPC into the hidden `doze __daemon` self-exec. |
| `internal/control` / `internal/endpoints` / `internal/state` / `internal/health` | Admin IPC · connection strings + `.doze/endpoints.yaml` · project state · health probes. |
| `internal/ui` / `internal/tui` | Color-gated CLI vocabulary · the Bubble Tea dashboard. |

## Key design decisions

- **Real binaries, one instance per database.** Full fidelity for free;
  near-zero protocol code.
- **Engine-blind core, engines as signed plugins.** Third-party engines are
  exactly as capable as official ones; a driver bug ships as a module release,
  not a doze release. No privileged path.
- **Two version axes, one lockfile.** Users declare only engine versions;
  module selection is automatic, deterministic once locked, explicit to move —
  and every compatibility failure names its fix (`doze modules upgrade …`).
- **Per-instance listeners.** Routing needs no protocol parsing; protocol
  awareness is a per-engine opt-in that runs in the engine's own process.
- **Reap on connection count, never query inactivity.** Pools hold idle
  connections open and must not be severed.
- **Instance dependencies are first-class**, derived from the config reference
  graph (SNS boots and holds its SQS).
- **Copy-on-write templates.** `initdb` runs once per version; instances clone.
- **Shared tool store, per-project state.** Engine binaries and modules live
  once under `~/.doze`; each project's clusters/runtime are namespaced.
- **Self-healing for local dev.** Backend exit detection re-arms lazy boot;
  failures surface in `status`/`doctor`; daemon shutdown is bounded; orphans
  from a prior crash are reclaimed by pidfile.
