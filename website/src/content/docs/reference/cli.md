---
title: "CLI"
description: Every doze command and flag, with the JSON envelope and exit codes for scripts.
---

This page lists every doze command, so you can look up what a command does and which flags it takes.

Everything the app does is also a command, so a script, a Makefile, CI or a
machine with no screen can do the same. doze starts its background daemon on
first use; you rarely manage it yourself. `doze <command> --help` prints the
same facts as this page.

| Group | Commands |
|---|---|
| [The app](#the-app) | `doze` · `open` |
| [The stack](#the-stack) | `up` · `down` · `sync` · `status` · `env` · `run` |
| [One service](#one-service) | `wake` · `sleep` · `restart` · `reset` · `migrate` · `task` · `pin` · `on` · `off` · `logs` · `url` |
| [Set up and edit](#set-up-and-edit) | `init` · `add` · `lint` · `fmt` · `doctor` · `dns-setup` · `trust` |
| [Lockfile](#lockfile) | `modules upgrade` |
| [Other](#other) | `version` · `completion` · `help` |

## Global flags

Every command takes these.

| Flag | What it does |
|---|---|
| `-c, --config <path>` | The config to read (default `doze.hcl`). doze also reads every sibling `*.doze.hcl`. A directory merges its `*.hcl`. doze looks upward from the current folder when the file is not here. |
| `--var name=value` | Set a config variable. Repeat it for more. |
| `--json` | Print the result as one JSON envelope (`DOZE_JSON=1`). See [Scripting doze](#scripting-doze). |
| `-q, --quiet` | Print nothing on stdout; the exit code is the answer. |
| `-y, --yes` | Answer yes to every question (`DOZE_YES=1`). |
| `--timeout <duration>` | How long to wait for one service to become ready (default `5m`). A block's own `ready_timeout` still applies. |
| `--no-color` | No colour (`NO_COLOR=1`). |

## The app

### `doze` and `doze open [dir]`

Bare `doze`, or `doze open`, opens [the app](/app/library/) at this project and
gives the prompt back. The app is one window for every stack on this machine,
plus a menu bar icon that stays after the window closes. Quitting the app
leaves stacks running.

| Flag | What it does |
|---|---|
| `--show <service>` | Open the window at this service of the project. |

With no screen (SSH, CI) `doze open` prints the status table instead. Bare
`doze` over SSH is the terminal dash; piped or redirected, it prints the
[status table](#doze-status).

`doze open` also takes a `doze://` link: `doze://shop` is the shop stack and
`doze://shop/db` its db service. Put one in a README or a runbook.

```sh
doze open doze://shop/db
```

## The stack

### `doze up [service…]`

Bring the stack up in the background. doze converges declared structure
(databases, roles, buckets, queues, topics), then wakes every enabled service
in dependency order and waits for each health probe. It returns when the stack
is ready; the daemon keeps supervising. Each line says how long that service
took to become ready. Name services to start only those and what they depend on.

| Flag | What it does |
|---|---|
| `-f, --follow` | Stream logs after waking. Ctrl-C detaches; the stack keeps running. |
| `--last` | Start what was started last time, without asking. |
| `--all` | Start everything, without asking. |

doze remembers what was started. When last time's set was smaller than
everything, a bare `doze up` on a terminal asks whether to start the same
again (`-y` says yes). With no terminal it starts everything, so a script gets
the same stack whatever someone ran before. Blocks with `enabled = false`, and
services turned [off](#doze-off-name) in this run, are skipped.

### `doze down`

Sleep every service and stop the daemon, so nothing is left running or
listening. Data stays. A connection does not wake a stack that is down; the
next `doze up` brings it back. To sleep services and keep the daemon up, use
[`doze sleep`](#doze-sleep-name).

### `doze sync [service]`

Bring the environment in line with `doze.hcl`: create or update databases,
roles, schemas, extensions, buckets, queues and topics, and drop structure that
was applied before but is no longer declared. It wakes nothing that is asleep.
A block with `enabled = false` is left alone, so its data survives.

| Flag | What it does |
|---|---|
| `--dry-run` | Show the changes without making them. |

The daemon reads `doze.hcl` once, when it starts. `sync` also lists the blocks
that differ from what the running daemon started with:

```text
doze.hcl differs from what the running daemon started with:

  ~ api       app      changed  restarts
  + mail      service  new      starts when something uses it
  - old       app      removed  stops; its data is kept

  1 block unchanged

  the daemon reads doze.hcl when it starts: doze down && doze up applies this
```

A comment or a reordering is not a change; only a block's arguments are.

### `doze status`

Aliases: `tree`, `ls`, `ps`.

Every service in a table grouped by kind: state, endpoint, open connections,
memory, CPU and a note. With the daemon down it shows the declared structure.

| Flag | What it does |
|---|---|
| `-w, --watch` | Keep the table on screen and redraw it as things change. Ctrl-C stops. |
| `--graph` | Draw the dependency tree instead of the table. |

| State | Meaning |
|---|---|
| `awake` | Running, and something holds it: a connection, a dependant, a pin. |
| `dozing` | Running with nothing holding it. The note counts down to sleep. |
| `waking` | Starting, or waiting for something it depends on. |
| `asleep` | Not running. It wakes on its next connection. |
| `crashing 2/5` | It exited by itself and doze is restarting it: attempt 2 of 5. |
| `failed` | Not running, and something went wrong. The note says what. |
| `disabled` | The block says `enabled = false`. |

The note is the one thing worth knowing beyond the state: `held by api`,
`pinned`, `sleeps in 1m12s`, or the error. `--json` keeps the daemon's own
words for the states (`active`, `idle`, `booting`, `reaped`).

### `doze env [service…]`

Print one `export` line per declared service, pointing at doze's endpoints, so
an unmodified app or the `aws` CLI talks to your local stack. Name services to
limit the output. The daemon does not need to be running.

| Flag | What it does |
|---|---|
| `--dotenv` | Print `KEY=value` lines for a `.env` file. |

```sh
eval "$(doze env)"
psql "$DATABASE_URL"
```

The variables are listed on [Environment](/reference/environment/#variables-doze-env-prints).

### `doze run -- <command> [args…]`

Make sure the daemon is up, then run a command. By default doze adds nothing to
the command's environment: every service has a fixed port, so connection
strings are stable and can live in your app's config.

| Flag | What it does |
|---|---|
| `--env` | Add the variables `doze env` prints to the command's environment. |

```sh
doze run --env -- go test ./...
```

`doze run` exits with its command's own code (128 plus the signal when a
signal killed it).

## One service

### `doze wake [name…]`

Aliases: `boot`.

Start the named services and what they depend on, in order, and wait until each
is ready. With no name, wake every enabled service. Unlike `up`, it does not
converge declared structure first.

### `doze sleep [name…]`

Aliases: `reap`.

Stop the named services. Their data stays, and each wakes on its next
connection or the next `doze wake`. With no name, sleep everything that is
awake. The daemon keeps running.

### `doze restart <name…>`

Stop a service and start it again.

### `doze reset <name…>`

Stop the named services and delete their data. The next wake provisions each
one fresh from the config. This cannot be undone, so it asks first. `--yes`
skips the question and is required when there is no terminal.

### `doze migrate <name>`

Move a postgres instance's data to the major version `doze.hcl` now declares.
Change `version` first (16 to 17), bring the stack down, then run
`doze migrate db`. doze dumps the data with Postgres 16, sets the old data
folder aside (kept, named for its version), makes a fresh Postgres 17 cluster
and restores the dump into it. Then bring the stack up. See
[Postgres major upgrades](/guides/postgres-upgrades/).

### `doze task [name]`

Run a task now, whatever its state, after waking what it references. It returns
when the task is over: exit 0 if it succeeded, 1 with its output if it failed.
This is the only way to run a `manual = true` task.

With no name it lists every task and how it last ran. A task also runs by
itself when something that lists it in `after` starts, if it never succeeded
or its `inputs` changed. See [Tasks](/hcl/tasks/).

### `doze pin <name…>`

Aliases: `keepawake`.

Keep services awake with nobody connected. Running it twice changes nothing the
second time, so a script can say what it wants. A pin lasts until the daemon
stops.

| Flag | What it does |
|---|---|
| `--off` | Unpin: let them sleep again when idle. |
| `--toggle` | Flip each one. |

### `doze off <name…>`

Take services out of this run: stop them and keep them stopped. A connection
does not wake one, and neither does a dependant. It is the switch on a service
in the app. Data stays. Off lasts until `doze on` or until the daemon stops.

### `doze on <name…>`

Undo `doze off`. The services are part of the run again and wake on their next
connection or the next `doze wake`. Nothing starts now.

### `doze logs [name…]`

Print the last lines of each named service's newest run, oldest first. With no
name, print every service, merged by time. Logs are kept on disk per run, so
this works for a service that is asleep or has crashed, and with the daemon
stopped. Lines from stderr are marked.

| Flag | What it does |
|---|---|
| `-f, --follow` | Keep printing as lines are written. Needs the daemon running. |
| `--run <n>` | Which run: `0` the newest, `-1` the one before, `N` a run by number. |
| `--since <duration>` | Only lines newer than this, e.g. `10m`. |
| `-n, --tail <n>` | How many lines from the end of the run (default 200). |

```sh
doze logs api --run -1     # what it said before it died
doze logs -f api worker    # follow both as they print
```

doze keeps the last 5 runs and 10MB per service. Change that with `log_runs`
and `log_size` in [`defaults`](/reference/doze-hcl/#defaults).

### `doze url <name>`

Aliases: `copy`.

Print the one line a client needs to reach a service: its connection string,
or its address when it has none. It reads the config, so the service need not
be running.

```sh
doze url db | pbcopy
psql "$(doze url db)"
```

## Set up and edit

### `doze init`

Read the project and write a `doze.hcl`. doze reads `go.mod` and the imports and
environment variables of your Go main packages, `package.json`, a compose file,
a `Makefile` and `.env`. It writes the apps to run, the modules and services
they talk to, the tasks that prepare them, and each app's `env` wired to what
it reads. Every block has a comment saying why it is there. It shows what it
found and asks before writing; `-y` skips the question. A project where it
finds nothing gets questions instead.

| Flag | What it does |
|---|---|
| `--wizard` | Ask questions instead of reading the project. |
| `--force` | Overwrite an existing config. |

### `doze add <module|service> [name]`

Add a block to `doze.hcl`, beside what is there. It does not reformat the file.
It prints the block and the line that wires it into an app.

| Flag | What it does |
|---|---|
| `--version <v>` | The engine version (default: the newest the module offers). |
| `--port <n>` | The port to declare (default: the module's usual one). |
| `--as <how>` | For a service: `installed`, `container` or `download`. |
| `--dry-run` | Show the block and write nothing. |

```text
$ doze add postgres
→ module postgres "db"
  module postgres "db" {
    version = 18
    port    = 5432
  }

  use it from an app:  DATABASE_URL = module.db.url
```

A name that is not a registry module, such as `doze add mailpit`, is added as a
[service](/hcl/your-own-services/). `--as installed` runs a binary on your
`PATH`, `--as container` runs a `docker run` command, and `--as download`
fetches a URL and pins it in `doze.lock`. With no `--as`, doze picks the first
way that works on this machine and names the others.

### `doze lint`

Check `doze.hcl` without touching the daemon or any data. It runs nothing and
changes nothing, so it is safe in CI and pre-commit hooks.

The first pass needs only the files: block kinds and names, the arguments of
`app`, `service` and `task`, variables, references, and the dependency graph
(no cycles, and no enabled block depending on a disabled one). The second pass
hands each module its own block to check, from the cache or fetched. If a
module is not cached and the registry cannot be reached, lint says which blocks
went unchecked and why, gives no tick, and exits 5: a block it could not check
is not a pass. `--offline` is the way to ask for the first pass alone.

| Flag | What it does |
|---|---|
| `--offline` | Run only the first pass: no cache, no network. It exits 0 on that pass and names the module blocks it left. |

It exits 5 on problems and 6 with no config. It also warns, without failing,
about a variable nothing reads and a task nothing lists in `after`.

### `doze fmt`

Rewrite `doze.hcl` and every sibling `*.doze.hcl` in HCL's canonical layout:
indentation, aligned `=`, spacing. Layout only, never meaning. A file that does
not parse is left untouched.

| Flag | What it does |
|---|---|
| `--check` | Write nothing; list the files that would change and exit 1 if any would. |

### `doze doctor`

Check the machine and the config: the config, the module registry (it asks
for one module's index, waiting at most 3 seconds), the daemon, module pins,
and each part of the setup for `.doze` names. It says what is wrong and the command that
fixes it. When a port the stack declares is held by another program, doctor
names it, with its pid and address.

| Flag | What it does |
|---|---|
| `--fix` | Run the one-time machine setup for `.doze` names (one sudo). |
| `--print-fix` | Print the setup script instead of running it. |

### `doze dns-setup`

The one-time machine setup for `<service>.<stack>.doze` names, the same as
`doze doctor --fix`. It routes the `.doze` zone to doze's resolver and installs
a small network daemon. On macOS it also gives services loopback addresses of
their own, so several can share a port such as 5432. It needs sudo once. The
setup is shared with doze AWS and doze Kafka: any of them installs it for all.

| Flag | What it does |
|---|---|
| `--check` | Report what is set up (no sudo). |
| `--print` | Print the script instead of running it. |
| `--uninstall` | Remove the setup (needs sudo). |

### `doze trust`

Make an app's `domains` reachable on this machine over https. It routes each
domain to doze's resolver and trusts the doze certificate authority. doze mints
only names a stack declares, but like any root the authority could sign any
name, so its key stays in `~/.doze/ca`, readable by you alone. It needs sudo
once. A domain no stack declares any more loses its route in the same step.
See [Domains and HTTPS](/app/domains/).

| Flag | What it does |
|---|---|
| `--check` | Report what is set up (no sudo). |
| `--print` | Print the script instead of running it. |
| `--remove` | Remove this stack's routes, and the authority's trust with the last stack (needs sudo). |
| `--rotate` | Make a new certificate authority, trust it, and stop trusting the old one. |

## Lockfile

### `doze modules upgrade [engine-type…]`

Aliases for `modules`: `mod`.

Every `module` block runs a signed module from the
[registry](/contribute/registry/), chosen automatically and pinned in
[`doze.lock`](/reference/lockfile/). Pins never move on their own. `upgrade`
picks the newest release that works with this doze and the engine versions you
declared, downloads and verifies it, and rewrites the pin. With no arguments it
upgrades every module the config uses. Commit the new `doze.lock`.

| Flag | What it does |
|---|---|
| `--check` | Report available upgrades without changing `doze.lock`; exit 1 if any. |

## Other

| Command | What it does |
|---|---|
| `doze version` | Print the doze version. |
| `doze completion <shell>` | Print a completion script for `bash`, `zsh`, `fish` or `powershell`. |
| `doze help [command]` | Help for any command. |

<!-- ground:skip -->
`doze dash` is hidden and deprecated: the app replaced it, and it goes away in the next release.

## Scripting doze

stdout carries what a command produced; stderr carries progress, warnings and
errors. So `doze up > log` keeps the results, and `doze url db | pbcopy` copies
one line.

With `--json` (or `DOZE_JSON=1`) a command prints one JSON envelope on stdout,
on success and on failure. The envelope has the same shape for every command:

```json
{
  "schema": "doze/v1",
  "command": "doze wake",
  "ok": false,
  "data": { "results": [
    { "name": "db", "state": "awake", "took_ms": 412 },
    { "name": "events", "error": "events not ready: did not become ready within 1m0s" }
  ] },
  "error": { "code": "timeout", "message": "events not ready: did not become ready within 1m0s", "exit": 8 }
}
```

`data` is the command's result: `results` for `up`, `wake`, `sleep`,
`restart`, `reset`, `pin` and `task <name>`; the services for `status`; the
variables for `env`; `{name, url}` for `url`; `lines` for `logs`. A command may
add `warnings`, a list of strings. `error.code` is the kind of failure, the
same word as in [Exit codes](#exit-codes).

These commands have a JSON result: `up`, `down`, `status`, `env`, `wake`,
`sleep`, `restart`, `reset`, `task`, `pin`, `on`, `off`, `logs`, `url`,
`lint`, `doctor` and `version`. The others (`run`, `sync`, `migrate`, `init`,
`add`, `fmt`, `dns-setup`, `trust`, `open`) refuse `--json` and exit 2;
`DOZE_JSON=1` leaves them as they are. `--json` and `-q` together are refused.

A command that follows something as it happens (`logs -f`, `status -w`) prints
NDJSON instead: one object per line, each with a `type`, and an `end` line
last. `status -w` sends a `status` line when anything changes. A watch ends
`ok` on Ctrl-C and when the stack stops.

```json
{"type":"log","instance":"api","line":"listening on :3000","stream":"o","run":2,"time":"…"}
{"type":"end","ok":true}
```

`-y` (or `DOZE_YES=1`) answers yes to every question, `reset`'s included. With
neither and no terminal, a command that must ask exits 9.

## Exit codes

Each code is one kind of failure. The same kind is the `code` in the JSON
envelope. Codes are added, never renumbered.

| Code | Kind | Meaning |
|---|---|---|
| 0 | | It worked. |
| 1 | `failed` | It failed, for a reason with no code of its own. |
| 2 | `usage` | The command line is wrong: an unknown command or flag, a missing argument. |
| 4 | `not_found` | The stack declares no service or task by that name. |
| 5 | `config_invalid` | The config doesn't load: an error in it, or a module it names won't fetch or start. |
| 6 | `no_config` | There is no `doze.hcl` here or in any parent folder. |
| 7 | `daemon_down` | The command needs a running stack and doze isn't running. |
| 8 | `timeout` | It took longer than allowed. |
| 9 | `needs_confirmation` | It needs a yes and there is no terminal to ask: pass the flag the message names. |
| 130 | `interrupted` | It was interrupted (Ctrl-C or SIGTERM). |

`doze run` exits with its command's own code instead.

The variables doze reads are on [Environment](/reference/environment/).
