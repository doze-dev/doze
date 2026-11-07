---
title: "doze.hcl"
description: Every block and field doze.hcl takes, with its type, default and what it does.
redirect_from: ["/reference/configuration/"]
---

This page lists every block and field in `doze.hcl`, so you can look up a field's type, default and meaning.

doze reads `doze.hcl` and any `*.doze.hcl` beside it. To learn a block, read
its page under doze.hcl in the sidebar; this page is for looking things up.

```hcl
stack = "larder"                          # root fields
defaults { names = true }

module postgres "db" { version = 18 }     # an engine from the doze registry
service "mailpit" { run = "mailpit" }     # anything else
task "migrate" { run = "bin/migrate up" } # runs to completion
app "api" { run = "go run ./cmd/api" }    # your code
```

A `module` is an engine from the doze registry. Anything else runs as your own
`service`. An `app` is your own code, and a `task` runs to completion.

## Root

| Field | Type | Default | What it does |
|---|---|---|---|
| `doze` | string | none | The oldest doze this stack needs: `">= 0.2"`. Read first, so an older doze says to update. `doze init` writes it. |
| `stack` | string | the folder's name | Names the stack. It scopes names (`<name>.<stack>.doze`) and must be unique among the stacks on the machine. |
| `home` | string | `$DOZE_HOME` or `~/.doze` | The shared toolchain store and cache, shared across projects. |
| `data_dir` | string | `<home>/projects/<slug>` | This project's state: data, sockets, logs. |

Blocks at the root: [`defaults`](#defaults), [`tls`](#tls), [`variable`](#variable),
[`locals`](#locals), [`output`](#output), [`module`](#module), [`app`](#app),
[`service`](#service), [`task`](#task).

## defaults

Settings for every block in the stack. A block's own field wins.

| Field | Type | Default | What it does |
|---|---|---|---|
| `sleep` | duration or `"never"` | `"2m"` | Sleep after this long at zero connections. `"never"` keeps everything awake unless a block says otherwise. |
| `names` | bool | `false` when unset; `doze init` writes `true` | Give every service a `<name>.<stack>.doze` name on an address of its own. `DOZE_ZONE=off` turns names off whatever this says. |
| `ready_timeout` | duration | `"60s"` | How long every app and service gets to become ready, unless its block says. |
| `env` | map | none | Variables every app, service and task gets. A block's own `env` wins key by key. |
| `log_runs` | number | `5` | How many runs of each service's log are kept. A run is one start. |
| `log_size` | size | `"10MB"` | The most log kept per service, across its runs. |
| `restart { … }` | block | `policy = "on_failure"` | The restart policy for every app and service without its own. Fields as in [restart](#restart). |

See [Sleep and wake](/hcl/sleep/) and [Names and addresses](/hcl/names/).

## tls

Client TLS for Postgres, ended by doze's proxy. The backend speaks plain text
over a local socket.

| Field | Type | Default | What it does |
|---|---|---|---|
| `cert` | string | made for you | A PEM certificate. With no `cert` and no `key`, doze makes a self-signed one. |
| `key` | string | made for you | The certificate's PEM private key. |
| `required` | bool | `false` | Refuse clients that do not use TLS (`sslmode=require`). |

## variable

`variable "<name>" { … }`, read as `var.<name>`.

| Field | Type | Default | What it does |
|---|---|---|---|
| `type` | type | any | A type constraint: `string`, `number`, `bool`, `list(string)`, `map(string)`, … |
| `default` | any | none | The value when nothing sets it. Without one, the variable is required. |
| `description` | string | none | What it is for. |
| `sensitive` | bool | `false` | The value is a secret. |

A value comes from, highest first: `--var name=value`, then `DOZE_VAR_<name>`,
then a sibling `*.auto.doze.vars` file, then `default`. A `--var` for a
variable the stack does not declare is an error.

## locals

`locals { … }`: named values, read as `local.<name>`. Every argument is one.
They may read variables, functions and other locals.

## output

`output "<name>" { … }`: a value worked out from the stack, recorded with its
state for scripts that read it.

| Field | Type | Default | What it does |
|---|---|---|---|
| `value` | any | none | The value, e.g. `module.db.url`. |
| `description` | string | none | What it is. |
| `sensitive` | bool | `false` | The value is a secret. |

## module

`module <type> "<name>" { … }`: an engine from the doze registry. These fields
work on every module; the rest belong to the module and are on its page.

| Field | Type | Default | What it does |
|---|---|---|---|
| `version` | number or string | none | The engine version: a major (`18`, the newest 18.x, then pinned) or exact (`"18.1"`, quoted). Required for every module except `aws`. |
| `port` | number | the engine's own | The port clients connect to, on the module's own address. |
| `listen` | string | none | A full address instead: `"unix:/path.sock"`, or `"0.0.0.0:5544"` to reach it from other machines. |
| `sleep` | duration or `"never"` | the stack's | How long it sits with nothing holding it before it sleeps. |
| `routes` | bool | `true` | `false` serves the module's HTTP listeners on their ports only, with none of its names without a port. |
| `enabled` | bool | `true` | `false` keeps it declared but never started. |
| `count` / `for_each` | number / map or set | none | Stamp several from one block. See [Stamping](#stamping). |
| `after` | list | none | Start only after these. See [after](#after). |

A version with a dot is quoted: as a number, `10.10` reads as `10.1`, so doze
refuses it.

### The modules

Each module's own fields, with examples, are on its page.

| Type | Versions | Port | Variable | Fields |
|---|---|---|---|---|
| `postgres` | 14 to 18 | 5432 | `DATABASE_URL` | [Postgres](/services/postgres/) |
| `valkey` | 8, 9 | 6379 | `REDIS_URL` | [Valkey](/services/valkey/) |
| `kvrocks` | 2 | 6380 | `REDIS_URL` | [Kvrocks](/services/kvrocks/) |
| `ferret` | 2 | 27017 | `MONGODB_URI` | [FerretDB](/services/ferretdb/) |
| `mariadb` | 11 | 3306 | `DATABASE_URL` | [MariaDB](/services/mariadb/) |
| `temporal` | 1 | 7233 | `TEMPORAL_ADDRESS` | [Temporal](/services/temporal/) |
| `kafka` | protocol 1 to 4 | 9092 | `KAFKA_BROKERS` | [Kafka](/services/kafka/) |
| `aws` | none | 4566 | `AWS_ENDPOINT_URL` | [AWS](/services/aws/) |

Versions and ports are from each module's newest release on the registry.
Anything not in this list runs as your own [`service`](#service).

## app

`app "<name>" { … }`: your own code, run on the host. See [Apps](/hcl/apps/).

| Field | Type | Default | What it does |
|---|---|---|---|
| `run` | string | none | The command line, run with `sh -c`. Required. |
| `dir` | string | the file's folder | The working directory, relative to the file that declares the block. |
| `port` | number | none | The port everyone else uses. doze holds it and starts the app with `$PORT`, a private port, to listen on. |
| `direct` | bool | `false` | The app binds `port` itself: no `$PORT`, and nothing wakes it. For what a TCP proxy cannot carry, such as UDP. |
| `sleep` | duration or `"never"` | the stack's with a `port`; `"never"` without | Sleep after this long with nothing holding it. A duration needs a `port` and not `direct`. |
| `watch` | list of globs | none | Restart when a matching file changes while it is awake. |
| `domains` | list of strings | none | Your own hostnames for the app, served over https. See [Domains and HTTPS](/app/domains/). |
| `cert` | string | made for you | A PEM certificate for `domains`. |
| `key` | string | made for you | The certificate's PEM private key. |
| `console` | string | none | A web console the app serves on a second port, e.g. `"http://127.0.0.1:8025"`. |
| `env` | map | none | Environment variables. Values may reference other blocks. |
| `env_file` | string | none | A `KEY=VALUE` file, below `env` in precedence. A missing file fails the start. |
| `ready` | probe | `tcp()` with a `port` | When it is ready: `tcp()`, `http("/path")`, `log("text")` or `exec("cmd")`. |
| `ready_timeout` | duration | `"60s"` | How long it has to become ready. |
| `setup` | string or list | none | Shell lines run once per data folder, before `run`. |
| `on_ready` | string or list | none | Shell lines run on every wake, once it is ready. |
| `stop` | string or list | none | Shell lines run before it is signalled to stop. |
| `restart { … }` | block | `policy = "on_failure"` | See [restart](#restart). |
| `enabled`, `count`, `for_each`, `after` | | | As on [module](#module). |

An `env` that sets `PORT` or `HOST` on an app with a `port` is a lint error.

### restart

| Field | Type | Default | What it does |
|---|---|---|---|
| `policy` | string | `"on_failure"` | `"no"`, `"on_failure"` or `"always"`. |
| `backoff` | duration | none | The first wait between restarts, doubled each time up to 30s, e.g. `"1s"`. |
| `max_retries` | number | `5` | How many restarts before it is marked failed. |

An HCL block on one line holds one argument only, so write `restart { … }`
across several lines.

## service

`service "<name>" { … }`: a long-running command from outside the registry. It
takes `run`, `dir`, `port`, `env`, `env_file`, `ready`, `ready_timeout`,
`setup`, `on_ready`, `stop`, `sleep`, `console`, `restart`, `enabled`,
`count`, `for_each` and `after`, as an app does. Plus:

| Field | Type | Default | What it does |
|---|---|---|---|
| `download` | string or map | none | An archive to fetch and run from: a URL, or a map of `mac`, `mac_intel`, `linux`, `linux_arm` to URLs. Checked against the digest pinned in `doze.lock`. |

A service with no `port` has nothing in front of it that could wake it, so it
runs until stopped, and a `sleep` duration on it is an error. See
[Your own services](/hcl/your-own-services/).

## task

`task "<name>" { … }`: a command that runs to completion, such as a migration.
It takes `run`, `dir`, `env`, `env_file`, `enabled`, `count`, `for_each` and
`after`. Plus:

| Field | Type | Default | What it does |
|---|---|---|---|
| `inputs` | list of globs | none | When the content of these files changes, the task runs again. |
| `manual` | bool | `false` | Run only by `doze task <name>`, never because something lists it in `after`. |

A task runs when something that lists it in `after` starts, if it never
succeeded, failed last time, or its `inputs` changed. See [Tasks](/hcl/tasks/).

## The moments of a command

`setup`, `on_ready` and `stop` are shell lines run around `run`.

| Step | Runs | If it fails |
|---|---|---|
| `setup` | Once per data folder: the first time, after `doze reset`, or when the list changes. | The start fails and `run` does not start. |
| `run` | Every wake. The one command that stays running. | Restarted with a backoff, then failed. |
| `ready` | Polled after `run` starts. | The start fails after `ready_timeout`. |
| `on_ready` | Every wake, after ready. | It keeps running but is not ready, so what depends on it does not start. |
| `stop` | Every sleep, before the signals. | Logged; the signals follow. |

`run` must stay in the foreground: `docker run -d` returns at once and looks
like an exit.

## References

A value can read another block as `<kind>.<name>.<attribute>`. A reference
also makes the reading block depend on the one it reads. The kind is the block
keyword, never the engine: `module postgres "db"` is `module.db`.

| Reference | Attributes |
|---|---|
| `module.<name>` | `url`, `address`, `host`, `port`, `socket`, `env_var`, `env`, `name`, `engine`, plus what the module adds |
| `app.<name>` | `address`, `host`, `port`, `url` (all need a `port`), `name` |
| `service.<name>` | `address`, `host`, `port`, `data`, `name` |
| `task.<name>` | none; a task is only named in `after` |

Names are unique across a stack, whatever the kind. Functions include the usual
string, collection and encoding helpers (`upper`, `join`, `format`, `merge`,
`jsonencode`, …), `env("NAME")` to read a host variable, and `env_of(…)`,
which gives every module's variable at once and refuses two that set the same
one. See [References and env](/hcl/references-and-env/).

## after

`after = [ … ]` on any block. Each entry is a reference to a whole block.

| Entry | Waits until |
|---|---|
| `module.db` | it is ready (its probe passes; a task has succeeded) |
| `started(module.db)` | its process has started (today doze still waits until it is ready) |
| `lazy(module.db)` | nothing: its address and env are filled in, it stays asleep, and the first connection wakes it |

See [Start order and readiness](/hcl/start-order/).

## Stamping

`count` and `for_each` stamp several blocks from one. A block can't set both.

| Field | Value | Inside the block | Names |
|---|---|---|---|
| `for_each` | a set of strings, or a map | `each.key`, `each.value` | `<label>_<key>` |
| `count` | a number, 0 or more | `count.index`, from 0 | `<label>_<index>` |

Keys are cleaned for names: `orders.fifo` becomes `orders_fifo`.

## What doze sets for a command

| Variable | Set for |
|---|---|
| `PORT` / `HOST` | an app or service with a `port` that is not `direct`: the private port and `127.0.0.1` |
| `DATA` | every app and service: a folder of its own that survives restarts |
| `DOZE_CA_FILE`, `NODE_EXTRA_CA_CERTS`, `SSL_CERT_FILE`, `REQUESTS_CA_BUNDLE` | every command, when the stack serves a domain from the doze certificate authority |

Connection strings are not added: pass each by reference
(`DATABASE_URL = module.db.url`). See [Environment](/reference/environment/).

## Files and the schema

Root fields live in `doze.hcl`. Blocks may be split across sibling
`*.doze.hcl` files; see [Splitting files](/hcl/splitting-files/).
[`/schema/doze-hcl.json`](/schema/doze-hcl.json) lists every block and field
on this page as data. It is generated from doze's own tables and checked
against them by a test. `doze fmt` formats the files; `doze lint` checks them.
