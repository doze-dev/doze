# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project aims to
follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
