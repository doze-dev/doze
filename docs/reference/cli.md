# CLI reference

Every command. The global flag `-c, --config <path>` selects the config (default
`doze.hcl`); it can also point at a directory to merge all its `*.hcl` files.

Most commands auto-start the background daemon if it isn't running, so you rarely
manage it directly.

## Everyday

### `doze init [--force]`
Scaffold a starter `doze.hcl` in the current directory. `--force` overwrites an
existing file.

### `doze run -- <command> [args…]`
Ensure the daemon is up, inject every instance's connection string into the
environment, run the command, and propagate its exit code. The core dev command.
```sh
doze run -- npm test
doze run -- go run ./cmd/api
doze run -- sh -c 'echo "$DATABASE_URL"'
```

### `doze env`
Print `export` lines for the connection strings, for `eval "$(doze env)"`. Also
writes `.doze/endpoints.yaml`.

### `doze up [instance]`
Converge declared structure — databases, roles, schemas, grants, extensions,
buckets, queues, topics. Idempotent. With no argument, converges everything.
```sh
doze up           # everything
doze up app       # just `app`
```

### `doze psql <instance> [-- psql args…]`
Open an interactive `psql` shell to a Postgres instance, booting it if cold.
Arguments after `--` pass through.
```sh
doze psql app
doze psql app -- -c 'select now()'
```

### `doze ephemeral <instance> [-- command…]`
Boot a disposable copy-on-write clone of an instance, inject its connection
string, run the command (or wait for Ctrl-C), then reap and delete it. Ideal for
isolated test runs.
```sh
doze ephemeral app -- pytest
doze ephemeral app                 # print the clone's URL and wait
```

## Inspect

### `doze status` (alias `doze ls`)
List instances and their live state — engine, colored state, connections, RAM,
uptime, endpoint, PID. Shows on-disk state when the daemon is stopped; an instance
that failed to boot shows state `error` with the reason. Output is plain when
piped (safe for scripts).

### `doze dash`
Launch the live, interactive TUI. Select a row with `↑/↓`, then:
`b` boot · `d` reap · `R` restart · `l` view logs · `r` refresh · `q` quit.

### `doze logs [instance] [-f]`
With no argument, tail the daemon's log. With an instance, show that backend's
logs. `-f`/`--follow` follows like `tail -f`.

### `doze doctor`
Diagnose the environment: config parses, platform, home/project dirs, per-instance
toolchain status, and daemon state.

### `doze versions [engine]`
List engine versions the mirror offers (like `nvm ls-remote`), marking which are
installed and pinned. With an engine name, also shows the platforms each version
is built for.

### `doze binaries list` · `doze binaries which <instance>`
`list` shows declared instances with their pinned/cached toolchains; `which`
resolves and prints an instance's bin directory.

## Lifecycle

### `doze start` / `doze stop` / `doze restart [instance]`
Manage the background daemon. `restart` with no argument restarts the daemon;
`restart <instance>` restarts a single instance (reap + re-boot).

### `doze serve`
Run the daemon in the **foreground** (instead of `start`), printing styled
boot/convergence progress to your terminal. Useful for watching what happens.

### `doze down [instance]`
Reap a running backend (or all of them with no argument). Data persists; the next
connection re-boots it.

### `doze version`
Print the doze version and Go runtime.

## Environment variables

| Variable | Effect |
|---|---|
| `DOZE_HOME` | Override the shared home (default `~/.doze`). |
| `DOZE_<ENGINE>_BINDIR` | Use an explicit engine bin dir instead of downloading (e.g. `DOZE_POSTGRES_BINDIR`). |
| `DOZE_<ENGINE>_MIRROR` / `DOZE_MIRROR` | Override the binaries mirror — see [BINARIES](../BINARIES.md). |
| `NO_COLOR` | Disable colored output. |
