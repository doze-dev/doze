# CLI reference

The global flag `-c, --config <path>` selects the config (default `doze.hcl`,
which auto-merges sibling `*.doze.hcl` files; a directory merges its `*.hcl`).
`--var name=value` (repeatable) overrides a config variable.

Most commands auto-start the background daemon if it isn't running, so you rarely
manage it directly. The command set has two parallel quartets — **structure**
(`plan`/`apply`/`destroy`/`output`) and **lifecycle** (`start`/`stop`/`restart`) —
where every lifecycle verb takes an optional instance (no argument = the daemon).

## Structure (declarative)

### `doze plan [instance]`
Show the structural changes `apply` would make — the diff between the declared
structure and the last applied state, as `+` create / `~` change / `-` destroy.
Read-only; makes no changes and boots nothing.

### `doze apply [instance]`
Converge declared structure — databases, roles, schemas, extensions, buckets,
queues, topics — **and prune** objects that were applied before but are no longer
declared. Shows the plan and asks for confirmation first (skip with
`--auto-approve`). Records the result in `.doze/state.json`. With no argument,
applies every instance that has structure.
```sh
doze apply                  # plan, confirm, apply everything
doze apply app              # just `app`
doze apply --auto-approve   # no prompt (for scripts/CI)
```

### `doze destroy [instance]`
Drop the structural objects doze has applied (tracked in state) and clear them
from state. Shows a plan and confirms first (`--auto-approve` to skip). This is
**not** `doze reset` — it removes logical structure (roles/databases/buckets/…),
not the engine's on-disk data directory.

### `doze output [name]`
Print the values declared in `output` blocks (connection strings, facts). With a
name, prints just that value, raw — for `$(doze output db_url)` in scripts;
sensitive outputs are masked in the full listing.

## Run & connect

### `doze run -- <command> [args…]`
Ensure the daemon is up, inject every instance's connection string into the
environment, run the command, and propagate its exit code. The core dev command.
```sh
doze run -- npm test
doze run -- sh -c 'echo "$DATABASE_URL"'
```

### `doze env`
Print `export` lines for the connection strings, for `eval "$(doze env)"`. Also
writes `.doze/endpoints.yaml`.

### `doze shell <instance> [-- client args…]` (alias `doze psql`)
Open the right interactive client for an instance's engine — `psql` for postgres,
`redis-cli` for valkey/kvrocks, `mongosh` for documentdb — connected through
doze's endpoint, booting the backend on connect. Arguments after the name pass
through to the client.
```sh
doze shell app
doze shell app -- -c 'select now()'
doze shell cache               # opens redis-cli
```

### `doze ephemeral <instance> [-- command…]`
Boot a disposable copy-on-write clone of an instance, inject its connection
string, run the command (or wait for Ctrl-C), then reap and delete it. Ideal for
isolated test runs.

## Lifecycle

`start` and `stop` always act on **instances** — name one, or pass `--all`. The
background **daemon starts automatically** on first use, so you don't start it by
hand; the only daemon action you take is shutting it down (`stop --all`).

### `doze start <instance | --all>`
Boot a backend now — warming it up instead of waiting for a connection. Name an
instance, or `--all` to boot every declared one. `-f`/`--foreground` runs the
daemon in this terminal (for debugging) instead of detaching.
```sh
doze start app        # boot the app backend now
doze start --all      # boot every declared instance
doze start -f         # run the daemon in the foreground
```
With no instance and no `--all`, it's an error — `start` never silently acts on
"the daemon." (Need the daemon up but nothing booted? `doze run -- true`.)

### `doze stop <instance | --all>`
Reap a backend. Name an instance to reap just that one (the daemon keeps running;
the next connection re-boots it), or `--all` to **stop everything** — every backend
and the daemon itself. Data persists either way.
```sh
doze stop app         # reap just app
doze stop --all       # full shutdown (daemon + all backends)
```

### `doze restart [instance]`
No argument: restart the daemon. With an instance: restart that backend (reap +
re-boot) — e.g. to pick up changed engine tuning.

## Inspect

### `doze status` (alias `doze ls`)
List instances and their live state — engine, colored state, connections, RAM,
uptime, endpoint, PID. Shows on-disk state when the daemon is stopped; an instance
whose last apply failed shows `tainted`, and one that failed to boot shows
`error` with the reason. Output is plain when piped (safe for scripts).

### `doze dash`
Launch the live, interactive TUI — a split "mission control": an instance sidebar
on the left, and on the right the selected instance's telemetry (state,
RAM/connection sparklines, a reap countdown) above its **streaming logs**. Select
a row with `↑/↓`, then: `b` boot · `d` reap · `R` restart · `f` toggle log-follow ·
`/` filter · `r` refresh · `q` quit. Mouse: click to select, scroll the sidebar or
the logs pane; drag across log lines (or `c` for keyboard copy mode) to copy to
the clipboard. For piping, `doze logs <instance>` prints to stdout instead.

### `doze logs [instance] [-f]`
With no argument, tail the daemon's log. With an instance, show that backend's
logs. `-f`/`--follow` follows like `tail -f`.

### `doze doctor`
Diagnose the environment: config parses, platform, home/project dirs, per-instance
toolchain status, and daemon state.

## Setup & toolchain

### `doze init [--force]`
Scaffold a starter `doze.hcl` in the current directory. `--force` overwrites an
existing file.

### `doze reset [instance]`
Wipe an instance's data directory and start fresh — the clean-slate counterpart to
`stop` (which only reaps the process). The next connection re-provisions and
re-converges. Downloaded toolchains are kept; `--hard` also drops shared templates.

### `doze binaries` (alias `bin`)
Inspect engine toolchains:
- `binaries list` — declared instances with their pinned/cached toolchains.
- `binaries which <instance>` — resolve and print an instance's bin directory.
- `binaries available [engine]` — versions the mirror offers (like `nvm ls-remote`),
  marking which are installed and pinned; with an engine, the platforms each builds for.

### `doze version`
Print the doze version and Go runtime.

## Environment variables

| Variable | Effect |
|---|---|
| `DOZE_HOME` | Override the shared home (default `~/.doze`). |
| `DOZE_VAR_<name>` | Set a config variable (lower precedence than `--var`). |
| `DOZE_<ENGINE>_BINDIR` | Use an explicit engine bin dir instead of downloading (e.g. `DOZE_POSTGRES_BINDIR`). |
| `DOZE_<ENGINE>_MIRROR` / `DOZE_MIRROR` | Override the binaries mirror — see [BINARIES](../BINARIES.md). |
| `NO_COLOR` | Disable colored output. |
