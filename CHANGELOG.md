# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project aims to
follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

doze is a multi-engine, no-Docker local database runtime — `proto` for
databases. A generic, engine-agnostic core drives per-engine drivers behind a
small interface; each declared instance boots on first connect and reaps when
idle.

### Added
- **Engines**: PostgreSQL, Valkey, Kvrocks (Redis protocol), and FerretDB
  (MongoDB wire), each a declarative config block (`postgres "n" {}`,
  `valkey "n" {}`, …).
- **Engine-agnostic core**: a `Driver` contract plus optional capability
  interfaces (convergence, protocol filter, copy-on-write templates, config
  decode, backend provider, dependencies) discovered by type assertion. Adding
  an engine is a driver package plus one registration line.
- **Per-instance proxy**: one listener per instance; a connection lazily boots
  the instance (coalesced via `singleflight`) and splices byte-for-byte. Reaping
  keys on connection count, never query inactivity.
- **`doze run` / `doze env`**: ensure instances up and inject connection strings
  (`DATABASE_URL`, `REDIS_URL`, `MONGODB_URI`, plus `DOZE_<NAME>_URL`);
  `.doze/endpoints.json` manifest.
- **Instance dependencies**: a dependent boots and holds its dependencies
  (FerretDB → its Postgres backend), releasing them on stop.
- **Copy-on-write templates & `doze ephemeral`**: `initdb` once per version,
  clone per instance (CoW); throwaway, isolated databases per test run.
- **Binaries**: multi-engine mirror manifest, content-addressed per-engine cache,
  a committed `doze.lock`, `DOZE_<ENGINE>_BINDIR` / `DOZE_<ENGINE>_MIRROR`
  (and `file://` mirrors), and `doze versions` to list what's available. The
  binaries are built and published by the companion `doze-binaries` repo.
- **Postgres specifics**: declarative roles/users, schemas, grants, and
  extensions (contrib + prebuilt bundles); query cancellation via the pgbouncer
  cancel dance; client-facing TLS termination via a `tls {}` block.
- `~/.doze` home laid out like moonrepo's proto: shared per-engine tool stores
  plus per-project, namespaced state. Overridable via `DOZE_HOME`.
- Daemon lifecycle (`start`/`stop`/`restart`/`serve`/`logs`), plus `init`,
  `doctor`, `psql`, and a live `dash` TUI.
- Licensed under Apache 2.0.

[Unreleased]: https://github.com/nerdmenot/doze/commits/main
