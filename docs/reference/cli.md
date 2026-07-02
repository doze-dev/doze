# CLI reference

The global flag `-c, --config <path>` selects the config (default `doze.hcl`,
which auto-merges sibling `*.doze.hcl` files; a directory merges its `*.hcl`).
`--var name=value` (repeatable) overrides a config variable.

doze starts its background daemon automatically on first use, so you rarely manage
it directly. The command set has a small, deliberate vocabulary:

| | |
|---|---|
| **Stack lifecycle** | `up` · `down` |
| **Per-service** | `wake` · `sleep` |
| **Structure** | `sync` |
| **Inspect** | `status` · `logs` · `dash` |
| **Wipe data** | `reset` |
| **Validate / scaffold / diagnose** | `lint` · `init` · `doctor` |
| **Connect / run** | `shell` · `run` |
| **Toolchains / registry** | `binaries` · `modules` · `version` |

## Stack lifecycle

### `doze up [service…]`
Converge declared structure and boot every enabled service in dependency order,
gating on each health probe — then **return**. The daemon keeps supervising
everything in the background; nothing stays attached to your terminal. Disabled
services are skipped. Name one or more services to bring up just those (and their
dependencies).
```sh
doze up                 # converge + boot the whole stack, then detach
doze up api worker      # just these two (and their deps)
doze up -f              # boot, then stream logs (Ctrl-C detaches — nothing stops)
```
Watch logs afterwards with `doze logs -f`. `doze down` stops the stack.

### `doze down`
The counterpart to `up`: sleep every service **and stop the background daemon**, so
nothing is left running or listening. To sleep services while keeping the daemon up
(so they can wake on the next connection), use `doze sleep` instead.

## Per-service

### `doze wake [service]`
Boot a service now instead of waiting for the first connection, bringing up its
dependencies first. With no argument it wakes every enabled service. Disabled
(`enabled = false`) services are skipped.
```sh
doze wake app      # boot app (and its deps) now
doze wake          # warm every enabled service
```

### `doze sleep [service]`
Reap a running service. Named, it first sleeps every service that **depends on** it
(so dependents drain before their dependency), then the service itself. With no
argument it sleeps all awake services. The daemon keeps running, so a later
connection can wake a service again — use `doze down` to stop the daemon too.

## Structure (declarative)

### `doze sync [service]`
Bring the local environment in line with `doze.hcl`: create or update databases,
roles, schemas, extensions, buckets, queues and topics, **and prune** structure
that was applied before but is no longer declared. The result is recorded in the
project state so the next `sync` diffs against it. A disabled (`enabled = false`)
service is left untouched — neither converged nor pruned, so its data survives.
```sh
doze sync                  # plan, confirm, then converge everything
doze sync app              # just `app`
doze sync --dry-run        # show the changes without making them
doze sync --auto-approve   # skip the confirmation prompt (for scripts/CI)
```

## Inspect

### `doze status` (aliases `tree`, `ls`, `ps`)
List the stack as a grouped table — services by category (Modules / Processes),
each with its live state (`active` / `idle` / `asleep` / `disabled`), endpoint,
open connections, memory and CPU, and what it depends on. With the daemon down it
shows the declared structure. `--graph` draws the dependency tree instead. Output
is plain when piped (safe for scripts).
```sh
doze status            # the grouped table
doze status --graph    # the dependency tree
```

### `doze dash`
Launch the live, interactive TUI — a split "mission control": an instance sidebar
on the left, and on the right the selected instance's telemetry (state, CPU, a
RAM/connection trace, a reap countdown) above its **streaming logs**. Select a row
with `↑/↓`, then: `b` boot · `d` reap · `R` restart · `f` toggle log-follow · `/`
filter · `r` refresh · `q` quit. Mouse: click to select, scroll the sidebar or the
logs pane; drag across log lines (or `c` for keyboard copy mode) to copy to the
clipboard.

For a running built-in (`s3` / `sqs` / `sns`), the inspector lists its resources
with live status — queue depth/in-flight, bucket object count/size, topic
subscriptions — and the data actions its engine offers (SQS `peek`/`send`/`purge`/
`redrive`, S3 `browse`/`empty`, SNS `publish`/`subscriptions`), driven from a small
command console with Tab completion.

### `doze logs [service] [-f]`
Show the output of your running services — the engine backends and your processes,
never doze's own supervisor chatter. With no service named it aggregates them all,
each line prefixed with its instance; name one to see just that service's raw
output. `-f`/`--follow` streams live. `--daemon` shows doze's own operational log
instead (booting/reaping/listeners) — for debugging doze.

## Run & connect

### `doze run -- <command> [args…]`
Ensure the daemon is up (so instances boot on first connect and reap when idle),
then run the command and propagate its exit code — a wrapper that guarantees your
backends are awake before a test or dev-server command connects.
```sh
doze run -- npm test
doze run -- ./dev-server
```
`run` injects **nothing** into the environment. Because every instance has an
explicit `port`, your connection strings are stable — see
[Getting connection strings](#getting-connection-strings) below.

### `doze shell <instance> [-- client args…]` (alias `doze psql`)
Open the right interactive client for an instance's engine — `psql` for postgres,
`redis-cli` for valkey/kvrocks, `mongosh` for documentdb — connected through doze's
endpoint, booting the backend on connect. Arguments after `--` pass through to the
client.
```sh
doze shell app
doze shell app -- -c 'select now()'
doze shell cache               # opens redis-cli
```

### Getting connection strings
doze does not inject environment variables into arbitrary commands. There are three
honest ways to get a connection string into your code:

1. **Declare the app as a `process` block.** doze supervises it and injects each
   dependency's connection string (its `env_var` → URL) into the process
   environment automatically. This is the blessed path for your own apps and
   workers.
2. **Write the stable URL yourself.** Every proxied instance has an explicit
   `port`, so its URL is deterministic — e.g.
   `postgresql://app:app@127.0.0.1:5432/app`. Put it in your app config or `.env`.
3. **Read the manifest.** The daemon writes `.doze/endpoints.yaml` with every
   instance's address and connection string — machine-readable, for tooling.

For ad-hoc interactive access, `doze shell <instance>` connects for you with no URL
needed.

## Wipe data

### `doze reset [instance]`
Stop the backend(s) and delete their data directories. The next connection
re-provisions a fresh store and re-converges the declared structure (roles,
databases, schemas, extensions) — so you get your schema back with no rows. The
clean-slate counterpart to `sleep` (which only reaps the process). With no instance
named, resets all. Downloaded toolchains are kept by default; `--binaries` also
drops the cached toolchain so the next boot re-downloads and re-verifies it against
`doze.lock`; `--hard` also drops the shared data-dir template. `-y`/`--force` skips
the confirmation prompt.

## Validate, scaffold, diagnose

### `doze lint`
Statically check `doze.hcl`: syntax, per-engine schema, variable and reference
resolution, and the dependency graph (acyclic, and no enabled service depending on
a disabled one). It runs nothing and changes nothing — safe for CI and pre-commit
hooks.

### `doze init [--force]`
Scaffold a `doze.hcl` — an interactive wizard on a TTY (pick services, optionally
wire an app command), or a starter file when non-interactive. `--force` overwrites
an existing config.

### `doze doctor`
Diagnose the environment: config parses, platform, home/project dirs, per-instance
toolchain status, and daemon state — a checklist of `✓`/`✗` items.

## Toolchains & registry

### `doze binaries` (alias `bin`)
Inspect the engine toolchains doze resolves from the mirror (versions and checksums
are pinned in `doze.lock`):
- `binaries list` — declared instances with their pinned/cached toolchains.
- `binaries which <instance>` — resolve and print an instance's bin directory.
- `binaries available [engine]` — versions the mirror offers (like `nvm ls-remote`),
  marking which are installed and pinned; with an engine, the platforms each builds for.

### `doze modules` (alias `mod`)
Every engine except `process` is a **module**: a signed plugin fetched from the
registry, selected automatically (newest release compatible with this doze and
your declared engine versions), pinned in `doze.lock`, and cached under
`~/.doze/modules`. The subcommands cover the whole lifecycle:

- `modules search [query]` (alias `available`) — discover what the registry
  publishes (source, engine versions, tagline).
- `modules docs <engine-type|source>` — the module's full config reference
  (arguments, nested blocks, defaults, engine-version badges) in the terminal —
  generated from the module itself, so it can't be stale.
- `modules list` — each declared engine type and how it's provided: the module
  release, its supported engine versions, and the cached binary (or an
  `override` when `DOZE_<TYPE>_PLUGIN` is set).
- `modules info <source>` (alias `verify`) — a source's release metadata
  (stable release, plugin protocol, engine support) and signature status for
  the index and every platform artifact — the same checks doze enforces before
  running a module.
- `modules upgrade [engine-type ...]` — re-select each module against the
  registry, download + verify, and move the `doze.lock` pins. No arguments =
  every declared engine. **Commit the updated lock.** With `--check`, report
  available upgrades without changing anything and exit 1 if any (CI-friendly).
- `modules which <engine-type>` — fetch (if needed) and print the plugin binary.

Pins never move on their own: a moving registry can't drift a locked project,
and errors that need a newer module say `run 'doze modules upgrade <type>'`
verbatim.

### `doze version`
Print the doze version and Go runtime.

## Environment variables

| Variable | Effect |
|---|---|
| `DOZE_HOME` | Override the shared home (default `~/.doze`). |
| `DOZE_VAR_<name>` | Set a config variable (lower precedence than `--var`). |
| `DOZE_<ENGINE>_BINDIR` | Use an explicit engine bin dir instead of downloading (e.g. `DOZE_POSTGRES_BINDIR`). |
| `DOZE_<ENGINE>_MIRROR` / `DOZE_MIRROR` | Override the engine-binaries mirror — see [BINARIES](../BINARIES.md). |
| `DOZE_MODULES_MIRROR` | Override the module registry base (URL or `file://`). |
| `DOZE_MODULES` | `off` disables module fetching entirely (offline / `process`-only). |
| `DOZE_<TYPE>_PLUGIN` | Run a local plugin binary for an engine type, skipping the registry (module development). |
| `NO_COLOR` | Disable colored output. |
