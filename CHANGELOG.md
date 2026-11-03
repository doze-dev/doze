# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project aims to
follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed

- **One JSON envelope for every command that has a result**: `--json` (or
  `DOZE_JSON=1`) prints `{schema, command, ok, data, warnings, error}` on
  stdout, on failure too, with nothing else there. `up`, `wake`, `sleep`,
  `restart`, `reset`, `pin`, `task`, `down`, `url`, `lint`, `version` gain
  JSON, and `logs` gives its lines (`logs -f` streams NDJSON ending with an
  `end` line). `status`, `env` and `doctor` move theirs into `data` (status's
  `error` field is now the envelope's), and `status` carries each running
  instance's full live view as `live`: holders, pid, restarts, last error,
  idle policy. A command with no result refuses `--json` (exit 2).
- `doze pin` pins; `--off` unpins; `--toggle` flips, as the dash does. It was
  a toggle, so running it twice undid itself. Each result says whether it
  changed.
- Global `-q`, `-y/--yes` (`DOZE_YES=1`), `--timeout` and `--no-color`.
  `-y` replaces `sync --auto-approve`, `reset --yes` and `init --yes`;
  `--timeout` replaces the fixed five-minute boot budget.
- **Exit codes mean something.** 2 for a usage mistake, 4 for a name the stack
  doesn't declare, 5 for a config that doesn't load, 6 for no config, 7 when
  the command needs a running stack, 8 for a timeout, 9 when a yes is needed
  and there is no terminal, 130 when interrupted. They were all 1. The daemon
  sends the kind with each error (`code` in the control protocol), so the CLI
  never reads message text. Listed in the CLI reference.
- **An app with a `port` now sleeps when idle**, after the stack's
  `idle_timeout` or its own `sleep`: a request wakes it. An app with no port,
  or with `direct = true`, still runs until it is stopped. `sleep = "never"`
  keeps any app on.
- Module blocks take `sleep` too: `"never"` keeps the module on, a duration
  replaces `idle_timeout` for it. A module doze doesn't sit in front of, such
  as `temporal`, refuses a duration, since nothing would wake it.
- `wakes_on` is refused as not in this release, instead of being accepted and
  ignored.
- `doze status --json` marks an instance that never sleeps with `always_on`,
  and the dashboard no longer counts one down to sleep.
- `front` and `logs` on apps and services are gone; they were parsed and never
  acted on. A config that sets either is told to delete it.

### Fixed

- `after` on a module block works: every published module rejected it, because
  the module decoded doze's own arguments. doze now keeps `after` and
  `sleep` to itself.
- A clean clone builds again: doze pins tagged releases of doze-sdk (v0.8.0)
  and doze-names (v0.2.0) instead of an unpushed commit.
- Two data races: live add or remove of an instance against the stats sampler
  and handlers, and an in-process `Serve` returning before the daemon had
  finished starting.
- A daemon started automatically now gets the caller's `--var` overrides,
  through its environment (`DOZE_VAR_<name>`), not its command line, where
  `ps` would show them.
- `doze sync` with no terminal on stdin asks for `--auto-approve` instead of
  printing "sync cancelled" and exiting 0.
- `doze status --json` reports `crashing`, `failed` and `done` instead of
  "asleep". With a broken config it still prints JSON: the blocks it could
  read, any live state, and an `error` field, and exits 1.
- The getting-started example is a config doze accepts.
- Reachable vulnerabilities in golang.org/x/net, grpc, golang.org/x/text and
  the standard library (Go 1.27.3).

### Changed

- `Session.Close` (the Go API) returns the in-process daemon's `Run` error
  as well as the host's.
- CI on every PR: golangci-lint (including the integration and bench suites),
  a tidy check, a check that doze-* dependencies are tags, a build from the
  public module proxy, govulncheck, a coverage floor, and the memory and wake
  budgets, which moved from the nightly run to every PR and now hold on Linux
  too (calibrated from fourteen runs).

## Unreleased before the redesign

doze is a lazy, no-Docker local runtime for backing services — `proto` for
databases and AWS services. A generic, engine-agnostic core drives per-engine
drivers behind a small interface; each declared instance boots on first connect
and reaps when idle.

### Added

- **`bind` field on status**: `doze status --json` (and the control API's
  `InstanceView`) now carry `bind` — the address the backend actually occupies
  (the per-instance loopback bind, a process's self-bound address, or the
  internal backend behind an AWS built-in's shared host). The dash shows it as
  the raw line under each instance's `connect` address. Additive; `endpoint`
  is unchanged.
- **Database engines**: PostgreSQL, Valkey, Kvrocks (Redis protocol), and
  DocumentDB (MongoDB wire), each a declarative config block (`postgres "n" {}`,
  `valkey "n" {}`, …).
- **Built-in AWS services**: local **S3, SQS, and SNS** implemented in pure Go
  and shipped inside the binary — no Docker, no JVM, no LocalStack. SQS speaks
  both wire protocols (AWS JSON 1.0 + legacy Query/XML) with FIFO and dead-letter
  redrive; SNS does filter policies, raw delivery, SNS→SQS fanout, and HTTP(S)
  webhooks; S3 embeds gofakes3 (buckets, multipart, presigned URLs). All are
  SDK-verified.
- **Engine-agnostic core**: a `Driver` contract plus optional capability
  interfaces (convergence, protocol filter, copy-on-write templates, config
  decode, backend provider, dependencies, env injection, versionless) discovered
  by type assertion. Adding an engine is a driver package plus one registration.
- **Per-instance proxy**: one listener per instance; a connection lazily boots
  the instance (coalesced via `singleflight`) and splices byte-for-byte. Reaping
  keys on connection count, never query inactivity.
- **`doze run`**: ensure the daemon is up, then run a command (backends boot on
  connect, reap when idle). Connection strings come from the explicit per-instance
  ports (stable URLs), from supervised `process` blocks (which get their
  dependencies' `env_var` → URL injected), or from the `.doze/endpoints.yaml`
  manifest the daemon publishes.
- **Instance dependencies**: derived from the config reference graph, a dependent
  boots and holds its dependencies (e.g. SNS → SQS instance), releasing them on stop.
- **Copy-on-write templates**: `initdb` once per version, clone per instance (CoW)
  for fast first boot; `doze reset` re-clones for a clean slate per test run.
- **Multi-file config**: `doze.hcl` + merged sibling `*.doze.hcl` files (or
  `--config <dir>`), with positioned, file/line config diagnostics and
  "did you mean?" hints.
- **Interactive TUI** (`doze dash`): select an instance and boot/reap/restart it
  or tail its logs, with a live-updating table.
- **Resilience**: backend-crash detection (mark reaped → clean reboot), bounded
  daemon shutdown, macOS orphan reclamation, and boot/convergence errors surfaced
  in `doze status`/`doctor`.
- **Binaries**: per-engine append-only release mirror, content-addressed cache, a
  committed `doze.lock` (versions + checksums), `DOZE_<ENGINE>_BINDIR` /
  `DOZE_<ENGINE>_MIRROR` (and `file://` mirrors), and `doze versions`. Built and
  published by the companion `doze-binaries` repo.
- **Postgres specifics**: declarative roles/users, schemas, grants, and
  extensions (contrib + prebuilt + from-source bundles); query cancellation via
  the pgbouncer cancel dance; client-facing TLS termination via a `tls {}` block.
- `~/.doze` home laid out like moonrepo's proto: shared per-engine tool stores
  plus per-project, namespaced state. Overridable via `DOZE_HOME` / `data_dir`.
- Daemon lifecycle (`start`/`stop`/`restart [instance]`/`serve`/`logs`), plus
  `init`, `up`, `down`, `status`/`ls`, `psql`, `doctor`, and styled CLI output.
- Licensed under Apache 2.0.

[Unreleased]: https://github.com/doze-dev/doze/commits/main
