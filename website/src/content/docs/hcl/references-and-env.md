---
title: References and env
description: Pass one block's address to another by reference, set environment variables, and parameterise a stack with variables.
---

Hand each app the addresses it needs by reference, set its environment, and
make parts of a stack configurable.

## References

A value in doze.hcl can point at another block: `<kind>.<name>.<attribute>`.

```hcl
app "orders" {
  run  = "go run ./services/orders"
  port = 8003
  env = {
    DATABASE_URL  = module.postgres.url
    KAFKA_BROKERS = module.kafka.address
    CATALOG_URL   = app.catalog.url
    SMTP_ADDR     = service.mailpit.address
  }
}
```

The kind is the block's keyword, never the module: `module postgres "db"` is
`module.db`, not `postgres.db`. Names are unique in a stack whatever the
kind, so `module.db` and `app.db` cannot both exist.

A reference does two things. It fills in the value, and it makes the block
wait for the one it names: before `orders` starts, doze wakes `postgres`,
`kafka`, `catalog` and `mailpit` and waits until each is ready
([Start order](/hcl/start-order/)). A reference to something not declared is
an error that suggests the nearest name; a loop of references is an error
too.

### What you can reference

| Reference | Attributes |
|---|---|
| `module.<name>` | `url`, `address`, `host`, `port`, `env_var`, `env`, `socket`, `name`, `engine`, and what the module adds |
| `app.<name>` | `url`, `address`, `host`, `port`, `name` |
| `service.<name>` | `address`, `host`, `port`, `data`, `name` |
| `task.<name>` | `name`; a task is usually named in `after` |

With [names](/hcl/names/) on, in a stack called `larder` (the made-up shop
the docs use as an example):

| Reference | Value |
|---|---|
| `module.postgres.url` | `postgres://postgres@postgres.larder.doze:5432/postgres?sslmode=disable` |
| `module.postgres.address` | `postgres.larder.doze:5432` |
| `module.postgres.host` | `postgres.larder.doze` |
| `module.postgres.port` | `5432` |
| `module.postgres.env_var` | `DATABASE_URL` |
| `module.postgres.env` | `{ DATABASE_URL = "postgres://…" }` |
| `app.catalog.url` | `http://catalog.larder.doze:8002` |
| `service.mailpit.address` | `mailpit.larder.doze:2525` |

With names off, each host is `127.0.0.1`. An app or service with no port has
no address and no url: referencing one is an error that says so. A service
has no `url` at all; build one around its `address`.

Each module's URL has the form its clients expect (`redis://`, `mysql://`,
`mongodb://`); its page under Services shows it. Some add attributes of their
own; Temporal adds `namespace` and `ui_url`.

### All of a module's variables at once

`module.<name>.env` is the module's usual variable, already set. `env_of`
gives an app several at once:

```hcl
app "api" {
  run = "go run ./cmd/api"
  env = merge(env_of(module.db, module.cache), { LOG = "debug" })
}
```

Unlike `merge`, `env_of` refuses two modules that set the same variable (two
Postgres servers both offer `DATABASE_URL`) and says so.

## Environment

```hcl
defaults {
  env = { LOG_LEVEL = "debug" }
}

app "api" {
  run      = "go run ./cmd/api"
  port     = 8080
  env_file = ".env.local"
  env      = { DATABASE_URL = module.db.url }
}
```

An app, service or task gets, from lowest to highest precedence:

1. the environment doze itself runs with;
2. `env_file`, a file of `KEY=VALUE` lines, relative to the file that declares
   the block;
3. `defaults { env }`, for every app, service and task;
4. the block's own `env`, which wins key by key.

doze also sets:

| Variable | Is |
|---|---|
| `PORT` | the private port to listen on, when the block has a `port` (not with `direct = true`) |
| `HOST` | `127.0.0.1`, with `PORT` |
| `DATA` | a folder of the block's own, kept across restarts and sleep |

Setting `PORT` or `HOST` in `env` is an error: doze owns them.

### Env for tools outside the stack

```sh
eval "$(doze env)"            # export every module's variable in this shell
doze env --dotenv > .env      # KEY=value lines
doze run --env -- npm test    # one command, with the variables set
```

`doze env` reads the config, so the stack need not be running. `doze run`
sets nothing unless you pass `--env`. When two modules want the same
variable, the second gets a suffix: `REDIS_URL_STORE`.

### Values from your shell

`env("NAME")` reads a variable from the environment doze runs in, with an
optional default:

```hcl
app "api" {
  run = "go run ./cmd/api"
  env = { STRIPE_KEY = env("STRIPE_KEY", "sk_test_local") }
}
```

## Variables

```hcl
variable "pg_version" {
  type    = number
  default = 18
}

locals {
  db_owner = "app_${var.pg_version}"
}

module postgres "db" {
  version = var.pg_version
  owner   = local.db_owner
}
```

A variable's value comes from, highest first:

1. `--var name=value` on the command line;
2. the `DOZE_VAR_<name>` environment variable;
3. a `*.auto.doze.vars` file beside doze.hcl (`name = value` lines);
4. its `default`.

A variable with no default and no value is an error, and so is a `--var` for
a variable the stack does not declare. doze warns about a variable nothing
reads.

`locals` are named values worked out from variables and functions, in
whatever order they depend on each other; they cannot reference modules,
apps, services or tasks.

`output "<name>" { value = module.db.url }` records a value with the stack's
state, in `.doze/state.json`, for scripts that read it. No command prints
outputs yet.

## Many of one block

`count` and `for_each` stamp several copies of one block:

```hcl
module postgres "tenant" {
  for_each = toset(["eu", "us"])
  version  = 18
}

service "worker" {
  count = 2
  run   = "bin/worker --shard ${count.index}"
}
```

Each copy is its own block, named `<label>_<key>` or `<label>_<index>`:
`module.tenant_eu`, `module.tenant_us`, `service.worker_0`,
`service.worker_1`. Inside the block, `each.key` and `each.value` (for
`for_each`) or `count.index` (from 0) tell the copies apart. A block cannot
set both.
