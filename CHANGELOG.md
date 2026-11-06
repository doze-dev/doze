# Changelog

All notable changes to this project are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project aims to
follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **The config language, tidied.** One word for ordering: `after = [module.db,
  started(app.cloud), lazy(module.aws)]`. Services and tasks take `count`,
  `for_each` and `enabled`. `defaults` takes `ready_timeout`, `restart` and a
  shared `env`. A module publishes `env` (`{ DATABASE_URL = … }`), and
  `env_of(module.db, module.cache)` joins them, refusing two that set the same
  variable. A gitignored `*.override.doze.hcl` changes `port`, `enabled`,
  `sleep` or `version` of a block for one developer. `doze fmt` (and `--check`
  for CI), and an editor schema at `/schema/doze-hcl.json`.
- **Mistakes say what to do.** A typo in a reference names the nearest
  attribute; an app's and a module's errors show the file, the line and the
  snippet like every other; an app that ignores `$PORT` is named on its ready
  timeout with the port it listens on; `doze lint` warns about a variable
  nothing reads and a task nothing waits for. In the app, a failed start says
  what went wrong and how to fix it, with Edit doze.hcl.
- **Compatibility.** `doze = ">= 0.2"` at the top of doze.hcl says the oldest
  doze a stack needs; doze skips module releases it cannot run and says to
  update; `doze migrate db` moves Postgres data to a new major; Check for
  Updates… in the app.

- **The app installs and explains itself.** A disk image and a Homebrew cask
  for the Mac (`brew install --cask doze-dev/tap/doze`, once a signed release is
  out); deb and rpm packages for Linux are built and wait for their by-eye pass.
  Repair in Settings, and Set up on the Library, run the machine setup behind
  the system's password prompt. Open at login puts the bat in the menu bar.
  A stack's page says when `doze.hcl` was saved broken (with the line) or
  changed after it came up, and names the process holding a port an app
  wanted. Activity per stack: the last hour, day or week from its journal, with how long each service slept, the memory that left free, what woke it, and what crashed. `doze://shop/db` links open the app there.
- **`doze` opens the app.** Where there is a screen, bare `doze` and `doze open`
  start the app and give the prompt back; over SSH `doze` is still the terminal
  dash, and piped it prints the status table. `doze dash` is deprecated and goes
  away in the next release. See "The app" page.
- **The app, redrawn from the ground up.** The Library is the first tab:
  every stack with a switch (up as last time, everything, a chosen set,
  down), its addresses, when it was last up, and the machine's setup on
  one line. A stack's overview comes in three views, Tiles, List with the
  details pane, and Map, remembered per stack; every row has a switch that
  takes a service out of this run and back (`doze off`, `doze on`). Inside
  a service: the connect card with the same URL in Node, Python, Go, a
  shell, .env and psql; why it is awake; the charts; a task's last run with
  the runs kept; a module's console in one frame. ⌘K reaches across every
  stack: what needs you, what you opened lately, a few things to do, a verb
  then a name, and the closest name when nothing matches. Settings holds
  the six themes (the terminal's five and a light one) and the machine part
  by part with the command that fixes each. The window opens at the
  screen's size the first time. A folder with no `doze.hcl` shows what
  doze found and the three ways to start.

- **An app can have its own domain, over https.** `domains = ["dev.acme.com",
  "*.dev.acme.com"]` on an app; `doze trust` routes the domain here and
  trusts a machine-local certificate authority in one prompt, or the app
  brings its own `cert` and `key`. The front door serves https on 443; the
  stack's processes get the authority through `SSL_CERT_FILE` and friends;
  `doze url`, the Connect card and ⌘K show the https address. A route no
  stack declares any more is removed at the next `doze trust`, and `doze
  doctor` reports routes, the authority, who holds the ports, whether the
  domain resolves to the stack, and a domain sign-in providers would refuse.
  A rotated authority is served by a running stack at once. The network
  daemon answers the zone whenever no stack does, on macOS and on Linux
  with systemd, so a routed domain means the real host again the moment
  doze is down; a machine set up before this needs `doze doctor --fix`
  once more, and doctor says so. See the "Your own domain" page.

### Changed

- **Renamed, with the old spelling an error that prints the new line:**
  `depends_on` is `after`; in `defaults`, `idle_timeout` is `sleep` (`"0s"`
  is `"never"`) and `domains = true` is `names = true`. A root `listen` is gone.
  A `--var` for an undeclared variable, and an `env_file` that is not there,
  are errors now. A version written as a number must be quoted when it has a
  trailing zero (`"10.10"`).
- A service with no port no longer sleeps by default: nothing could wake it.

- **Services doze after 2 minutes, not 5.** The default `idle_timeout` is now
  `2m`, and `doze init` writes `2m`. A stack that sets its own keeps it.
- **Modules download engine binaries through doze** (doze-sdk v0.10.0): the
  mirror, the cache and their checks live in doze alone, so changing them
  rebuilds no module. Modules built with an older SDK keep downloading for
  themselves and still work.
- **`doze up` remembers.** What was started, and what was awake at `doze
  down`, is kept in the project's state folder (`project.json`). When it was
  less than everything, a bare `up` asks whether to start the same again;
  `--last` and `--all` skip the question.
- **One JSON envelope for every command that has a result**: `--json` (or
  `DOZE_JSON=1`) prints `{schema, command, ok, data, warnings, error}` on
  stdout, on failure too, with nothing else there. `up`, `wake`, `sleep`,
  `restart`, `reset`, `pin`, `task`, `down`, `url`, `lint`, `version` gain
  JSON, and `logs` gives its lines (`logs -f` streams NDJSON ending with an
  `end` line, and so does `status -w`). `status`, `env` and `doctor` move theirs into `data` (status's
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

- `Session.Close` (the Go API) returns the in-process daemon's `Run` error
  as well as the host's.
- CI on every PR: golangci-lint (including the integration and bench suites),
  a tidy check, a check that doze-* dependencies are tags, a build from the
  public module proxy, govulncheck, a coverage floor, and the memory and wake
  budgets, which moved from the nightly run to every PR and now hold on Linux
  too (calibrated from fourteen runs).

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
