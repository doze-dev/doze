---
title: "CLI reference"
---


The global flag `-c, --config <path>` selects the config (default `doze.hcl`,
which auto-merges sibling `*.doze.hcl` files; a directory merges its `*.hcl`).
`--var name=value` (repeatable) overrides a config variable.

doze is deliberately **two-surface**: the [dash](/cli/dashboard/) is the primary
human surface — every per-service verb (wake, sleep, restart, reset, logs,
open-in-browser) lives there, reachable by key or `:` palette — and the CLI
below is the headless automation core for CI, scripts, and Makefiles, plus the
tools you need before a dash can run. doze starts its background daemon
automatically on first use, so you rarely manage it directly.

| | |
|---|---|
| **The dash** | `doze` (or `doze dash`) |
| **Automation core** | `up` · `down` · `sync` · `status` · `env` · `run` |
| **Before the dash** | `init` · `lint` · `doctor` · `dns-setup` |
| **Lockfile maintenance** | `modules upgrade` |
| | `version` |

## The dash

### `doze` / `doze dash`

Running bare `doze` opens the dash: the live fleet with per-service state,
charts, and logs; engine pages for the aws and kafka instances (service board,
live API wire, topics and consumer lag); an APPS view (`A`) for your own
processes; and a `:` command palette carrying every verb — `:wake`, `:sleep`,
`:restart`, `:reset`, `:logs`, `:filter`, `:apps`, `:theme`. `o` opens a
service's web surface (the AWS console, the Kafka console, your app) in the
browser.

## Automation core

### `doze up [service…]`

Bring the stack up in the background: converge declared structure (databases,
roles, buckets, queues, topics — everything your config declares), then wake
every service (or just the named ones and their dependencies), gated on health
probes. Returns when the stack is ready; the daemon keeps supervising.

### `doze down`

Bring the whole stack down: sleep every service and stop the daemon. Data
persists — the next `up` (or the first connection) brings everything back.

### `doze sync [service]`

Reconcile the running stack with the config — create new instances, update
changed ones, prune removed ones. `up` implies it; `sync` does it without
waking anything that's asleep.

### `doze status` (aliases `tree`, `ls`, `ps`)

Every service: state (active / idle / asleep), endpoint, connections, memory,
CPU, and dependencies. `--json` for scripts.

### `doze env`

Print the services' connection variables as eval-able exports —
`DATABASE_URL`, `REDIS_URL`, `AWS_ENDPOINT_URL_*`, `KAFKA_BROKERS`, … —
exactly what doze injects into your `process` blocks. `--dotenv` and `--json`
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

## Before the dash

### `doze init [--force]`

Scaffold a `doze.hcl` — an interactive wizard when on a terminal, a starter
config otherwise.

### `doze lint`

Validate the config without touching the daemon or any data: schema, references,
ports, engine-version gates. Exit 1 on problems (CI-friendly).

### `doze doctor`

Diagnose the environment: config, daemon, DNS setup, loopback aliases, module
signatures, toolchains. Says what's wrong and the command that fixes it.

### `doze dns-setup`

The one-time (sudo) setup that gives every service its own loopback IP so they
all share canonical ports by DNS name — three Postgres instances, each at
`<name>.<stack>.doze:5432`. `--check` verifies, `--uninstall` removes.

## Lockfile maintenance

### `doze modules upgrade [engine-type ...]` 

Every engine except `process` is a signed module fetched from the
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
| `DOZE_MODULES_MIRROR` | Override the module registry base (URL or `file://`). |
| `DOZE_MODULES` | `off` disables module fetching entirely (offline / `process`-only). |
| `DOZE_<TYPE>_PLUGIN` | Run a local plugin binary for an engine type, skipping the registry (module development). |
| `NO_COLOR` | Disable colored output. |
