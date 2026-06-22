# Contributing to doze

Thanks for helping out. doze is a single Go binary; the dev loop is small.

## Prerequisites

- Go 1.26+ (the module pins the toolchain, so `go` will fetch it automatically).
- A local database install is **not** required to build or run — doze downloads
  its own binaries. A local engine is handy for integration smoke tests via
  `DOZE_<ENGINE>_BINDIR` (e.g. `DOZE_POSTGRES_BINDIR`).

## Dev loop

```sh
make build      # version-stamped binary at ./doze
make test       # unit tests
make race       # tests with the race detector
make check      # fmt-check + vet + test (what CI runs)
```

Before sending a change, run `make check`. Keep the tree `gofmt`-clean.

## Layout

```
cmd/doze/            CLI commands (cobra); blank-imports engine/* to register drivers
internal/
  engine/            the driver contract: Driver + Process + capability interfaces + registry
  runtime/           engine-agnostic orchestration: singleflight boot, deps, reaper, state
  proxy/             per-instance listeners, byte splice, cancel registry, ProxyFilter dispatch
  supervisor/        generic process lifecycle (spawn, stop, parent-death); engine.Process
  binaries/          multi-engine mirror manifest, content-addressed cache, doze.lock
  config/            HCL -> engine-agnostic root + per-engine block dispatch
  endpoints/         per-instance addresses, connection strings, .doze/endpoints.json
  control/ daemon/   admin IPC + `doze serve` wiring
  tui/               Charm Bubble Tea dashboard
engine/
  postgres/          first driver: cluster, converge, extensions, proxyfilter, template
  valkey/ kvrocks/   Redis-protocol drivers (required methods only)
  ferretdb/          MongoDB-wire driver; depends on a Postgres backend
docs/                design docs (architecture, binaries, extensions)
```

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for how the pieces fit. New
engines are a driver package under `engine/` plus a registration line; the core
does not change.

## Conventions

- Match the style of the surrounding code; comment the *why*, not the *what*.
- Pure logic (protocol parsing, registry, config, manifest, dependency wiring)
  is unit-tested; anything that needs a real backend is verified with a manual
  smoke test against `DOZE_<ENGINE>_BINDIR` and noted in the PR.
- Commits are imperative and scoped (`proxy: …`, `config: …`, `docs: …`).

## Architecture decisions that are settled

doze runs **real, unmodified engine binaries**, one instance per declared
database, started on demand and reaped when idle (by connection count, never
query inactivity). A generic core drives per-engine drivers behind a small
interface. It does not embed engines, reimplement wire protocols beyond what a
transparent proxy needs, or seed/migrate application data. Please open an issue
before proposing changes to these.
