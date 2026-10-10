---
title: "CLI reference"
---


The global flag `-c, --config <path>` selects the config (default `doze.hcl`,
which auto-merges sibling `*.doze.hcl` files; a directory merges its `*.hcl`).
`--var name=value` (repeatable) overrides a config variable.

doze has two surfaces that do the same things. The [dash](/cli/dashboard/) is
the one to live in: every verb is a key or a `:` palette row there. The CLI
below has a command for each of them, so a script, a Makefile, CI, or a
terminal with no room for a dash can do everything the dash can. doze starts
its background daemon automatically on first use, so you rarely manage it
directly.

| | |
|---|---|
| **The dash** | `doze` (or `doze dash`) |
| **The stack** | `up` · `down` · `sync` · `status` · `env` · `run` |
| **One service** | `wake` · `sleep` · `restart` · `reset` · `pin` · `logs` · `url` |
| **Before the dash** | `init` · `lint` · `doctor` · `dns-setup` |
| **Lockfile maintenance** | `modules upgrade` |
| | `version` |

## The dash

### `doze` / `doze dash`

Running bare `doze` on a terminal opens the dash (piped or redirected, it prints
the [status table](#doze-status-aliases-tree-ls-ps) instead): the live fleet with per-service state,
charts, and logs; engine pages for the aws and kafka instances (service board,
live API wire, topics and consumer lag); an APPS view (`A`) for your own
processes; and a `:` command palette carrying every verb — `:wake`, `:sleep`,
`:restart`, `:reset`, `:logs`, `:filter`, `:apps`, `:theme`. `o` opens a
service's web surface (the AWS console, the Kafka console, your app) in the
browser.

## The stack

### `doze up [service…]`

Bring the stack up in the background: converge declared structure (databases,
roles, buckets, queues, topics — everything your config declares), then wake
every service (or just the named ones and their dependencies), gated on health
probes. Each line says how long that service took to become ready, as the
daemon measured it. Returns when the stack is ready; the daemon keeps
supervising.

### `doze down`

Bring the whole stack down: sleep every service and stop the daemon. Data
persists — the next `up` (or the first connection) brings everything back.

### `doze sync [service]`

Reconcile the running stack with the config — create new instances, update
changed ones, prune removed ones. `up` implies it; `sync` does it without
waking anything that's asleep.

It also says which **blocks** differ from the ones the running daemon started
with, because the daemon reads `doze.hcl` once, when it starts:

```
doze.hcl differs from what the running daemon started with:

  ~ api       app      changed  restarts
  + mail      service  new      starts when something uses it
  - old       app      removed  stops; its data is kept

  1 block unchanged

  the daemon reads doze.hcl when it starts: doze down && doze up applies this
```

A comment or a reordering is not a change; only a block's arguments are.

### `doze status` (aliases `tree`, `ls`, `ps`)

Every module and app: state, endpoint, connections, memory, CPU, and a note.

| State | Meaning |
|---|---|
| `awake` | Running, and something is holding it: a connection, a dependant, a pin. |
| `dozing` | Running with nothing holding it; the note counts down to sleep. |
| `waking` | Starting, or waiting for something it depends on. |
| `asleep` | Not running. It wakes on its next connection. |
| `crashing 2/5` | It exited by itself and is being restarted: attempt 2 of 5. |
| `failed` | It is not running and something went wrong; the note says what. |

The note is the one thing worth knowing beyond the state: `held by api`,
`pinned`, `sleeps in 4m12s`, or the error. `-w` keeps the table on screen and
redraws it as things change. `--graph` draws the dependency tree. `--json` is
for scripts, and keeps the daemon's own state words (`active`, `idle`,
`booting`, `reaped`).

### `doze env`

Print the services' connection variables as eval-able exports —
`DATABASE_URL`, `REDIS_URL`, `AWS_ENDPOINT_URL_*`, `KAFKA_BROKERS`, … —
the same set `doze run --env` injects. `--dotenv` and `--json`
for other consumers, dummy AWS credentials included when an AWS endpoint is
present.

```sh
eval "$(doze env)"
psql "$DATABASE_URL"
```

### `doze run -- <command> [args…]`

Ensure the daemon is up, then run a command with the stack's environment
injected — the one-shot version of `eval "$(doze env)"`:

```sh
doze run -- go test ./...
```

## One service

What the dash's keys do, as commands.

### `doze wake [name…]`

Start the named services now, with whatever they depend on, and wait until each
is ready. With no name, wake everything. Unlike `up` it does not reconcile
declared structure first.

### `doze sleep [name…]`

Stop the named services, keeping their data; each wakes again on its next
connection. With no name, put everything that is awake to sleep and leave the
daemon running (`doze down` stops that too).

### `doze restart <name…>`

Stop a service and start it again.

### `doze reset <name…> [--yes]`

Delete a service's data. The next wake provisions it fresh from the config.
It cannot be undone, so it asks first; `--yes` skips the question and is
required when there is no terminal to ask on.

### `doze task [name]`

Run a task now, whatever its state, after waking what it references. It returns
when the task is over: exit 0 if it succeeded, 1 with the task's last lines of
output if it failed. This is the only way to run a `manual = true` task.

With no name it lists every task, when it last ran, how that went and why it
ran. A task also runs by itself when something that lists it in `after` starts
and it has never succeeded or its `inputs` changed.

### `doze pin <name…>`

Keep a service awake past its idle time. It is a toggle: pin again to unpin.
A pin lasts until the daemon stops.

### `doze logs [name…] [-f] [--run N] [-n N] [--since T]`

What a service printed. Logs are kept on disk, one run per start, so this
works for a service that is asleep or has crashed, and with the daemon stopped.

```sh
doze logs api              # the newest run
doze logs api --run -1     # the run before it: what it said before it died
doze logs api --since 10m  # only the last ten minutes
doze logs -f api worker    # follow both, merged, as they print
```

Lines from stderr are marked `err`. With no name, every service is shown,
merged by time. The last five runs and 10 MB per service are kept; change that
with `defaults { log_runs = 5  log_size = "10MB" }` (written on separate
lines).

### `doze url <name>`

Print the one line a client needs to reach a service — `doze url db | pbcopy`,
or `psql "$(doze url db)"`. It reads the config, so the service need not be
running.

## Before the dash

### `doze init [--force] [--wizard] [--yes]`

Write a `doze.hcl` for this project from what it says. `init` reads `go.mod`
and the imports and environment variables of your Go main packages,
`package.json`, a compose file, a `Makefile` and `.env`, and writes the apps to
run, the modules and services they talk to, and the tasks that prepare them —
each block with a comment saying why it is there, and each app's `env` wired to
the module that answers what it reads (`DATABASE_URL = module.db.url`). An app
that reads a database waits for its `migrate` task.

It shows what it found and asks before writing (`--yes` skips the question). A
project it finds nothing in, or `--wizard`, gets the questions instead.

### `doze add <module|service> [name]`

Add a block to `doze.hcl`, beside what is there — your comments and ordering are
left alone. `doze add postgres` writes a registry module with the newest engine
version and its usual port, and prints the line that wires it into an app:

```
$ doze add postgres
→ module postgres "db"
  module postgres "db" {
    version = 18
    port    = 5432
  }

  use it from an app:  DATABASE_URL = module.db.url
```

A name that is not a module — `doze add mongo` — is added as a **service**, and
there are three ways to run one: `--as installed` (a binary on your `PATH`),
`--as container` (a `docker run` command; a container is just a command) and
`--as download` (a URL, pinned in `doze.lock`). With no `--as` it takes the first
that works on this machine and says the others. `--dry-run` shows the block and
writes nothing; `--version`, `--port` and a second argument set the rest. A
name that is taken is refused, and what would be written has to parse.

### `doze lint`

Validate the config without touching the daemon or any data: schema, references,
ports, engine-version gates. Exit 1 on problems (CI-friendly).

### `doze doctor [--fix]`

Diagnose the environment: config, daemon, module pins, and each part of the
machine setup for `.doze` names — including which doze program is currently
answering for the zone. Says what's wrong and the command that fixes it.

When a port the stack declares is held by another program, doctor says which —
its name, pid and address — and the options in order of how little they ask:
stop it (`kill <pid>`), give every service an address of its own (`doze doctor
--fix`, so ports never clash), or use another port in the block. It is silent
about ports once each service has an address of its own.

`doze doctor --fix` runs the one-time setup that makes
`<service>.<stack>.doze` names work: it routes the `.doze` zone to doze's
resolver and, on macOS, installs a small network daemon that gives every
service an address of its own, so several can share a port (three Postgres
instances, each at `<name>.<stack>.doze:5432`) with no limit on how many. It
needs sudo once. `--print-fix` prints the
script instead of running it. The setup is shared with doze-aws and
doze-kafka: whichever of them you run first installs it for all three.

### `doze dns-setup`

Another name for `doze doctor --fix`. `--check` verifies without sudo,
`--print` shows the script, `--uninstall` removes the setup.

## Lockfile maintenance

### `doze modules upgrade [engine-type ...]` 

Every `module` block is backed by a signed module fetched from the
[registry](https://doze.nerdmenot.in/registry/), selected automatically and
pinned in `doze.lock` — pins never move on their own. `upgrade` re-selects
against the registry, downloads + verifies, and moves the pins (**commit the
updated lock**). With `--check`, report available upgrades without changing
anything and exit 1 if any (CI-friendly). No arguments = every declared engine.

Discovery — what modules exist, their engine versions, platforms, config —
lives on the [registry](https://doze.nerdmenot.in/registry/), generated from
the modules themselves.

### `doze version`

Print the doze version.

## Environment variables

| Variable | Effect |
|---|---|
| `DOZE_HOME` | Override the shared home (default `~/.doze`). |
| `DOZE_VAR_<name>` | Set a config variable (lower precedence than `--var`). |
| `DOZE_<ENGINE>_BINDIR` | Use an explicit engine bin dir instead of downloading (e.g. `DOZE_POSTGRES_BINDIR`). |
| `DOZE_<ENGINE>_MIRROR` / `DOZE_MIRROR` | Override the engine-binaries mirror — see [BINARIES](/reference/binaries/). |
| `DOZE_MODULES` | `off` disables module fetching entirely (offline / apps only). |
| `DOZE_ZONE` | `off` switches `.doze` names off for every doze program, whatever a config asks for (CI, containers). |
| `DOZE_<TYPE>_PLUGIN` | Run a local plugin binary for an engine type, skipping the registry (module development). |
| `NO_COLOR` | Disable colored output. |
