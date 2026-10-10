---
title: "Configuration reference"
---


doze reads HCL from `doze.hcl` (and any sibling `*.doze.hcl`, merged
automatically — see [splitting config](#splitting-config-across-files)). The file
has a fixed **root** plus one **block per thing it runs**. There are four
kinds of block:

```hcl
stack = "shop"                            # root settings
defaults { idle_timeout = "5m" }

module postgres "app"   { version = 16 }  # an engine from the doze registry
module valkey   "cache" { version = 9 }

app "api" { run = "go run ./cmd/api" }    # your code
```

`module <type> "<name>"` is an engine from the registry, `app "<name>"` is your
own code, and [`service` and `task`](#service-and-task) are commands from
outside the registry and commands that run to completion.

Jump to an engine: [postgres](#postgres) · [valkey](#valkey) ·
[kvrocks](#kvrocks) · [ferret](#ferret) · [mariadb](#mariadb) ·
[temporal](#temporal) · [kafka](#kafka) · [aws](#aws).
Your own code: [app](#app). Project-level: [modules](#modules) · [TLS](#tls).

This is the field-by-field reference. For what each engine *is* and when to use
it, see **[The engines](/guides/engines/)**. Each engine's page on the
[registry](https://doze.nerdmenot.in/registry/) carries the same reference,
generated from the module itself, so it can't be stale.

## Root

| Field | Type | Default | Description |
|---|---|---|---|
| `stack` | string | the config folder's name | Names the stack. It scopes local domains (`<name>.<stack>.doze`) and must be unique among the stacks running on the machine. |
| `listen` | string | `127.0.0.1:6432` | Base client address. Each instance gets the next port; override per instance with `listen`, or use a `unix:/path`. |
| `home` | string | `$DOZE_HOME` or `~/.doze` | Shared toolchain store + cache (deduplicated across projects). |
| `data_dir` | string | `<home>/projects/<slug>` | This project's state (data dirs, sockets, logs). |
| `defaults { idle_timeout }` | duration | `5m` | Reap an instance after this long at **zero connections**. |
| `defaults { log_runs }` | number | `5` | How many runs of each service's log are kept. A run is one start. |
| `defaults { log_size }` | size | `"10MB"` | The most log kept per service, across its runs. |
| `tls { … }` | block | off | Terminate client TLS for Postgres — see [TLS](#tls). |

### TLS

| Field | Type | Default | Description |
|---|---|---|---|
| `cert` | string | auto | Path to a PEM certificate. Omit `cert` **and** `key` to auto-generate a self-signed cert. |
| `key` | string | auto | Path to the matching PEM private key. |
| `required` | bool | `false` | Reject plaintext TCP clients (require `sslmode=require`). |

```hcl
tls {}                          # auto self-signed cert; sslmode=require works
```

TLS is terminated at the proxy; the backend speaks plaintext over a local socket.

## Common instance fields

Every `module <type> "<name>" { … }` block accepts:

| Field | Type | Default | Description |
|---|---|---|---|
| `version` | string/number | — | The **engine** version: a major (`16` → newest 16.x, pinned) or exact (`"16.14"`). **Required** for every module except `aws`, which takes none. |
| `listen` | string | next port from root `listen` | Per-instance client address (`"127.0.0.1:5544"` or `"unix:/path.sock"`). |

`version` is the only version you declare. The *module* (the plugin that
provides the engine) is selected automatically — the newest release compatible
with your doze and the engine versions you declared — and pinned in `doze.lock`.
Some engine arguments are **version-gated**: using one below the engine version
that introduced it fails at `doze lint` with the argument and required major
named (docs mark these, e.g. *engine ≥ 18*). See [modules](#modules) for the
rare overrides.

---

## References & expressions

Values are HCL expressions, not just literals. You can call functions and—most
usefully—**reference other blocks** by `<kind>.<name>.<attribute>`:

```hcl
app "api" {
  run  = "go run ./cmd/api"
  port = 8080
  env = {
    DATABASE_URL = module.app.url   # reference → builds the dependency edge
  }
}
```

The kind is the block's keyword, never the engine: a `module postgres "app"`
block is `module.app`, not `postgres.app`.

| Reference | Attributes |
|---|---|
| `module.<name>` | `url`, `address`, `host`, `port`, `socket`, `env_var`, `name`, `engine`, plus whatever the module adds |
| `app.<name>` | `address`, `host`, `port`, `url` (all need the app to declare a `port`), `name` |
| `service.<name>` | `address`, `host`, `port`, `data`, `name` |
| `task.<name>` | none — a task is only named in `after` |

A reference does two things: it resolves to the attribute's value, and it makes
the referencing block **depend on** the referenced one (doze boots and holds
the dependency first — no hand-declared ordering). Referencing a block that
isn't declared is a lint error, and reference cycles are rejected. Names are
unique across a stack whatever the kind, so `module.db` and `app.db` cannot
both exist.

Every module exposes these baseline attributes:

| Attribute | Description |
|---|---|
| `name` / `engine` | The instance name and its engine type. |
| `address` | Client-facing `host:port` (or `unix:/path`). |
| `host` / `port` | Split address (empty/`0` for a unix socket). |
| `socket` | Unix socket path (empty for TCP). |
| `url` | The connection string (e.g. `postgres://…`). |
| `env_var` | The conventional variable name (`DATABASE_URL`, …). |

Functions include the common string/collection/number/encoding helpers (`upper`,
`join`, `format`, `coalesce`, `merge`, `jsonencode`, …) plus `env("NAME")` to read
a host environment variable (with an optional default).

### Variables & locals

```hcl
variable "pg_version" {
  type    = number
  default = 16
}

locals {
  app_db = "app_${var.pg_version}"
}

module postgres "app" {
  version = var.pg_version
  owner   = local.app_db
}
```

**`variable "<name>" { … }`** — a typed input.

| Field | Type | Default | Description |
|---|---|---|---|
| `type` | type | any | Optional constraint: `string`, `number`, `bool`, `list(string)`, `map(string)`, … |
| `default` | any | — | Value when no override is given. Omit to make the variable required. |
| `description` | string | none | Human description. |
| `sensitive` | bool | `false` | Hint that the value is secret. |

Values resolve by precedence (highest first): **`--var name=value`** › **`DOZE_VAR_<name>`**
env var › a sibling **`*.auto.doze.vars`** file (`name = value` assignments) › the
`default`. A required variable with no value is an error.

**`locals { … }`** — named intermediate values (`local.<name>`); may reference
variables, functions, and earlier locals.

### Stamping with `for_each` / `count`

Stamp several similar instances from one block. Each stamp becomes its own
instance with a flat name — `<label>_<key>` (for_each) or `<label>_<index>`
(count) — addressable like any other (`module.shard_0.url`, `module.tenant_eu.url`).

```hcl
module postgres "tenant" {
  for_each = toset(["eu", "us", "apac"])   # → tenant_eu, tenant_us, tenant_apac
  version  = 16
}

module valkey "shard" {
  count     = 3                       # → shard_0, shard_1, shard_2
  version   = 9
  maxmemory = "${(count.index + 1) * 64}mb"
}
```

| Meta-arg | Value | In-body | Names |
|---|---|---|---|
| `for_each` | a set of strings, or a map | `each.key`, `each.value` | `<label>_<key>` |
| `count` | a non-negative number | `count.index` (0-based) | `<label>_<index>` |

`count = 0` (or an empty `for_each`) produces no instances. A block can't set both.
Keys are sanitized for use in names/paths (`orders.fifo` → `orders_fifo`).

### Explicit ordering: `after`

References already create dependencies, so you rarely need this. For an ordering
that isn't expressed through a reference, `after` adds it:

```hcl
app "worker" {
  run   = "go run ./cmd/worker"
  after = [module.cloud]
}
```

Each entry is a reference to a whole block. The app waits until a module or
another app is ready, and until a task has succeeded.

`depends_on` is the longer form, for when the condition matters: a map of block
name to `healthy` (the default for every reference and every `after` entry) or
`started` (wait only until the dependency's process has spawned).

```hcl
app "worker" {
  run        = "go run ./cmd/worker"
  depends_on = { "cloud" = "started" }
}
```

---

## postgres

Real PostgreSQL (14–17). On boot, doze creates the database (named after the
instance) and converges the declared roles, schemas, grants, and extensions.

```hcl
module postgres "app" {
  version = 16
  owner   = "app"
  role "app" { password = "app" }
  grant {
    role       = "app"
    database   = "app"
    privileges = ["ALL"]
  }
  extensions = ["uuid-ossp"]
}
```

**Block fields**

| Field | Type | Default | Description |
|---|---|---|---|
| `owner` | string | `postgres` | Role that owns the database. |
| `encoding` | string | server default | Database encoding, e.g. `"UTF8"`. |
| `locale` | string | server default | Database locale; shorthand for both `lc_collate` and `lc_ctype`. |
| `lc_collate` | string | `locale` | Collation, overriding `locale`. |
| `lc_ctype` | string | `locale` | Character classification, overriding `locale`. |
| `template` | string | server default | Template database to clone from. |
| `connection_limit` | number | `-1` (unlimited) | Database `CONNECTION LIMIT`. |
| `is_template` | bool | `false` | Mark the database a template. |
| `allow_connections` | bool | `true` | Allow connections to the database. |
| `tablespace` | string | default | Tablespace for the database. |
| `comment` | string | none | `COMMENT ON DATABASE`. |
| `shared_buffers` | string | `16MB` | Postgres `shared_buffers`. |
| `max_connections` | number | `50` | Postgres `max_connections`. |
| `fsync` | bool | `false` | When off (default), also disables `synchronous_commit` and `full_page_writes` — fast, not crash-safe. Set `true` for durability. |
| `autovacuum` | bool | `false` | Enable autovacuum. |
| `settings` | map(string) | `{}` | Raw `postgresql.conf` passthrough for any parameter without a typed field (e.g. `{ work_mem = "8MB" }`). Applied after the typed tuning; doze-locked params (`listen_addresses`, …) always win. |
| `extensions` | list(string) | `[]` | Shorthand for `CREATE EXTENSION IF NOT EXISTS` per name. |

**`role "<name>" { … }`** — a login user (default) or, with `login = false`, a group role.

| Field | Type | Default | Description |
|---|---|---|---|
| `password` | string | none | Login password. |
| `login` | bool | `true` | `false` makes it a group role. |
| `superuser` | bool | `false` | Grant SUPERUSER. |
| `createdb` | bool | `false` | Grant CREATEDB. |
| `createrole` | bool | `false` | Grant CREATEROLE. |
| `replication` | bool | `false` | Grant REPLICATION. |
| `inherit` | bool | `true` | Inherit privileges of granted roles. |
| `bypassrls` | bool | `false` | Grant BYPASSRLS (skip row-level security). |
| `connection_limit` | number | `-1` (unlimited) | Max concurrent connections. |
| `valid_until` | string | none | Password expiry timestamp. |
| `member_of` | list(string) | `[]` | Roles this role is a member of. |
| `comment` | string | none | `COMMENT ON ROLE`. |
| `config` | map(string) | `{}` | Per-role parameters via `ALTER ROLE … SET` (e.g. `{ search_path = "app, public" }`). |

**`schema "<name>" { … }`**

| Field | Type | Default | Description |
|---|---|---|---|
| `owner` | string | database owner | Role that owns the schema. |

**`grant { … }`**

| Field | Type | Default | Description |
|---|---|---|---|
| `role` | string | — | Grantee role. **Required.** |
| `privileges` | list(string) | — | e.g. `["ALL"]`, `["SELECT","INSERT"]`. **Required.** |
| `database` | string | none | Grant at the database level. |
| `schema` | string | none | Grant within a schema. |
| `objects` | string | none | With `schema`: `tables` / `sequences` / `functions` (covers current + future objects). |
| `with_grant_option` | bool | `false` | Allow the grantee to re-grant. |

**`extension "<name>" { … }`** — for options beyond the `extensions` shorthand.

| Field | Type | Default | Description |
|---|---|---|---|
| `version` | string | latest available | Specific extension version. |
| `schema` | string | default | Schema to install into. |
| `source` | string | none | Path to a source/bundle for an extension the binary doesn't ship — see [Extensions](/reference/extensions/). |
| `cascade` | bool | `false` | Add `CASCADE` to also create dependency extensions. |
| `optional` | bool | `false` | When `true`, an unavailable or failed extension is a warning, not a hard error. By default a missing/failed extension **fails convergence and taints the instance**. |

---

## valkey

In-memory, Redis-protocol cache.

```hcl
module valkey "cache" {
  version   = 9
  maxmemory = "256mb"
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `version` | string/number | — | Major or exact. **Required.** |
| `password` | string | none | `requirepass`. |
| `maxmemory` | string | unlimited | Memory cap, e.g. `"256mb"`. |
| `maxmemory_policy` | string | server default | Eviction policy, e.g. `"allkeys-lru"`. |
| `appendonly` | bool | `false` | Enable the AOF persistence log. |
| `save` | string | off | RDB snapshot schedule, e.g. `"3600 1 300 100"`. |
| `settings` | map(string) | `{}` | Raw `valkey.conf` passthrough, e.g. `{ "lazyfree-lazy-eviction" = "yes" }`. Applied last so it overrides typed fields. |

---

## kvrocks

RocksDB-backed, Redis-protocol durable KV store.

```hcl
module kvrocks "store" {
  version  = 2
  password = "default-token"
  namespace "tenant_a" { token = "tok-a" }
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `version` | string/number | — | Major or exact. **Required.** |
| `password` | string | none | `requirepass` (also the default-namespace token). |
| `workers` | number | server default | Worker thread pool size. |
| `settings` | map(string) | `{}` | Raw `kvrocks.conf` passthrough, e.g. `{ "rocksdb.block_size" = "16384" }`. |

**`namespace "<name>" { … }`** — a kvrocks namespace with an access token. Requires `password`.

| Field | Type | Default | Description |
|---|---|---|---|
| `token` | string | — | The namespace access token. **Required.** |

---

## ferret

A self-contained, MongoDB-compatible engine: doze runs a private PostgreSQL with
Microsoft's DocumentDB extension behind a FerretDB v2 gateway, exposing only the
Mongo wire (`MONGODB_URI`). One declared instance is one composite backend — no
separate postgres to wire up. See the [DocumentDB recipe](/guides/recipes/documentdb/).

```hcl
module ferret "shop" {
  version = "2.7"
  port    = 27017

  database "catalog" {
    collection "products" { seed = "./seed/products.json" }
  }
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `version` | string/number | — | FerretDB v2.x gateway version. **Required.** |
| `database` | block | — | A Mongo database to ensure (repeatable; label = name), with nested `collection` blocks. |
| `settings` | map(string) | — | Extra `FERRETDB_*` gateway settings. |

Full argument/block reference: its
[registry page](https://doze.nerdmenot.in/registry/doze/ferret).

---

## mariadb

MySQL-compatible relational database (the MariaDB 11.4 LTS series). Runs on
macOS and Linux — doze repackages upstream's x86_64-Linux binary and builds
the other platforms from source.

```hcl
module mariadb "db" {
  version = 11.4

  user "app" { password = "app" }
  grant {
    user = "app"
    privileges = "ALL"
    database = "db"
  }
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `version` | string | — | MariaDB server version (e.g. `11.4`). **Required.** |
| `character_set` | string | — | Default charset for the instance database (e.g. `utf8mb4`). |
| `collation` | string | — | Default collation. |
| `settings` | map(string) | — | Extra `[mysqld]` my.cnf entries. |

**`user "<name>" { … }`** — a MariaDB account (`host`, `password`).
**`grant { … }`** — a privilege grant (`user`, `privileges`, `database`, `table`).

---

## temporal

A durable workflow engine: the Temporal dev server (single pure-Go binary —
services, SQLite store, Web UI). Workers long-poll it, which holds it awake.

```hcl
module temporal "dev" {
  version = 1.1
  port    = 7233
  ui_port = 8233

  namespace "orders" {}
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `version` | string | — | Temporal CLI version (e.g. `1.1`). **Required.** |
| `port` | number | `7233` | Frontend gRPC port. |
| `ui_port` | number | `8233` | Web UI port. |
| `headless` | bool | `false` | Disable the Web UI. |

**`namespace "<name>" { … }`** — a namespace to create (`retention`, `description`).
**`restart { … }`** — supervisor restart policy (`policy`, `backoff`, `max_retries`).

---

## kafka

A single-node Kafka-protocol broker, no JVM. `version` is the **advertised
protocol profile** (1–4), not a broker build. Its web console serves one port
above the broker.

```hcl
module kafka "events" {
  version = 4

  auto_create_topics = true
  retention          = "168h"

  topic "orders"   { partitions = 3 }
  topic "payments" { config = { "cleanup.policy" = "compact" } }
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `version` | number | — | Advertised Kafka protocol profile — 1, 2, 3, or 4. **Required.** |
| `auto_create_topics` | bool | `true` | Create unknown topics on first reference. |
| `default_partitions` | number | `1` | Partition count for auto-created topics. |
| `retention` | duration | unbounded | Delete segments older than this (`"168h"`). |
| `retention_bytes` | number | `0` (unbounded) | Delete old segments past this per-partition size. |

**`topic "<name>" { … }`**

| Field | Type | Default | Description |
|---|---|---|---|
| `partitions` | number | `1` | Partition count. |
| `config` | map(string) | — | Per-topic config (e.g. `"cleanup.policy" = "compact"`). |

References: `module.<name>.address` is the bootstrap `host:port`.

---

## aws

The whole local AWS as **one instance**: S3, SQS, SNS, DynamoDB (with
Streams), Lambda, EventBridge, KMS, SSM, and Secrets Manager behind a single
endpoint, with a web console at `/_console`. Takes no `version`. Resources are
nested blocks, converged on boot; their labels are the resource names your SDK
uses.

```hcl
module aws "local" {
  port = 4566

  bucket "uploads" { versioning = true }
  queue "emails" {
    dlq          = "auto"
    max_receives = 5
  }

  queue "orders.fifo" {
    fifo          = true
    content_dedup = true
  }

  topic "signups" {
    subscribe {
      queue = "emails"
      raw   = true
    }
  }

  table "sessions" {
    key = "session_id:S"
    ttl = "expires_at"
  }

  function "resize" {
    code    = "./functions/resize"    # dir with a provided.al2 bootstrap
    timeout = 10
  }

  rule "order_placed" {
    pattern = "{\"source\":[\"shop.checkout\"]}"
    targets = ["lambda:resize", "queue:emails"]
  }

  key       "app_key"      {}
  parameter "/demo/flag"   { value = "on" }
  secret    "db_password"  { value = "local-only" }
}
```

**Resource blocks** (each repeatable; the label is the name):

| Block | What it declares |
|---|---|
| `bucket` | An S3 bucket — `versioning`, `object_lock` (implies versioning). |
| `queue` | An SQS queue — `fifo`, `content_dedup`, `dlq` (`"auto"` creates + wires a dead-letter companion), `max_receives`, timing fields. FIFO names end in `.fifo`. |
| `topic` | An SNS topic with `subscribe` blocks (`queue`/`lambda`/`http`, `raw`, `filter`). |
| `table` | A DynamoDB table — `key = "pk:S"` shorthand (add `sort = "at:N"`), `ttl`, `gsi`/`lsi` blocks. Streams on. |
| `function` | A Lambda running as a real local process — `code` (local dir with a `provided.al2` bootstrap), `timeout`, `env`. |
| `rule` | An EventBridge rule — `pattern` (a **literal JSON string** — HCL functions aren't available here), `targets` as `"kind:name"` shorthand. |
| `key` / `parameter` / `secret` | KMS key (real local crypto) / SSM parameter (label = full `/path`) / Secrets Manager secret (never stomped without force). |

References: `module.<name>.url` is the endpoint for every `AWS_ENDPOINT_URL_*`
variable. Resource names inside the block are plain strings to your app.

---

## app

An **app** is a long-running command that is your code, run directly on the
host — no Docker, no virtualization. Unlike a module (which doze proxies and
idle-reaps), an app is a supervised *client* of those backends: it starts with
`doze up`, is not put to sleep when idle, gates readiness on a probe, and
restarts per a policy when it exits.

```hcl
app "api" {
  dir  = "../approvals-engine"            # working dir, relative to this file
  run  = "go run main.go -program api"    # run via `sh -c` (pipes, &&, expansion OK)
  port = 8080                             # doze listens here; the app listens on $PORT

  env = {
    DATABASE_URL = module.app.url         # reference → dependency edge + value
  }
  env_file = ".env.local"                 # optional; lower precedence than env

  after = [task.migrate]                  # ordering with no value to reference

  setup    = "go run main.go -program migrate -command up"   # before the app starts
  on_ready = "./scripts/notify-ready.sh"                     # once it is ready
  stop     = "./scripts/drain.sh"                            # before it is signalled

  ready         = http("/health/ready")   # probed on the port the app listens on
  ready_timeout = "90s"

  restart {
    policy      = "on_failure"            # no | on_failure | always
    backoff     = "1s"                    # exponential, capped at 30s
    max_retries = 5
  }
}
```

| Field | Type | Default | Notes |
|---|---|---|---|
| `run` | string | — | Command line, run via `sh -c`. **Required.** |
| `dir` | string | the file's dir | Working directory, resolved relative to the declaring file. |
| `port` | number | none | The app's public port. **doze listens on it, not the app**: the app is started with `$PORT`, a private port doze picks at each start, and doze forwards between the two (see [Ports](#ports-for-an-app)). Exposed as `app.<name>.{url,address,host,port}`. Omit for a worker with no endpoint. |
| `direct` | bool | `false` | The app binds `port` itself, and doze only starts it: no `$PORT`, no waking on a connection, no connection count. For anything a TCP proxy cannot carry, such as UDP. |
| `sleep` | duration or `"never"` | `"never"` | Put the app to sleep after this long with nothing holding it; its next connection wakes it. Needs a `port` and not `direct`, since only a connection through doze can wake it. |
| `console` | URL | none | A web console the app serves on a second port, such as `http://127.0.0.1:8025`. doze fronts that port too, on the app's own address, and opening it wakes the app. |
| `env` | map | none | Environment for the app and its steps. Highest precedence; values may reference other blocks. |
| `env_file` | string | none | Path to a `KEY=VALUE` file (lower precedence than `env`). |
| `after` | list of references | none | Blocks to wait for, written as references to the whole block: `[task.migrate, module.db]`. |
| `ready` | probe | `tcp()` when `port` is set | `tcp()` (the port accepts a connection), `http("/path")` (a 2xx from the app's own port), `log("text")` (the app printed it) or `exec("cmd")` (exits 0). With no `port` and no `ready`, readiness = "the process stayed alive briefly". |
| `ready_timeout` | duration | `"60s"` | How long the app gets to become ready. A first `go run` that has to compile needs more. |
| `setup` | string or list | none | Commands run before the app starts, each via `sh -c` in `dir`. A failure aborts the start. |
| `on_ready` | string or list | none | Commands run once the app is ready. |
| `stop` | string or list | none | Commands run before the app is signalled to stop. |
| `restart` | block | `policy = "on_failure"` | `policy` (`no` / `on_failure` / `always`), `backoff` (base, grown exponentially and capped), `max_retries` (default 5). |

An app's environment is layered as: the daemon's own environment → `env_file`
→ `env`, plus three variables doze sets for every command:

| Variable | Value |
|---|---|
| `PORT` | The port to listen on. Set only when the app declares a `port` and is not `direct`. |
| `HOST` | The address to listen on, `127.0.0.1`. Set with `PORT`. |
| `DATA` | A folder of the app's own that survives restarts. |

Nothing else is added: connection strings reach an app by reference
(`DATABASE_URL = module.app.url`), not by injection. An `env` that sets `PORT`
or `HOST` on an app with a `port` is a lint error, because the app would
listen somewhere doze is not forwarding to.

### Ports for an app

```hcl
app "api" {
  run  = "go run ./cmd/api"
  port = 8080
}
```

`port = 8080` is where everyone else finds the app: `localhost:8080`, or
`api.<stack>.doze:8080` with names on. doze holds that port from the moment
the stack is up, whether the app is awake or asleep. The app itself is started
with `PORT` set to a private port and must listen there:

```go
addr := net.JoinHostPort(os.Getenv("HOST"), os.Getenv("PORT"))
```

What that buys:

- **A sleeping app wakes on its first request**, the way a database does.
- **`doze status` knows who is connected**: connections to an app are counted,
  and `held by` works for apps as it does for modules.
- **Two stacks can both use 8080.** With names on, each app has an address of
  its own. doze also gives the app `127.0.0.1:8080` when nothing else has it,
  because people type `localhost` from habit; when something does, status says
  the app is reachable by name only.

It costs one hop on the same machine: about 0.2 ms per new connection. An app
that hard-codes its port, or sets `PORT` itself, will fail to start because
doze already holds the port; either read `$PORT` or set `direct = true`.
doze runs `go`/`bun`/`node` from `PATH`; a `.go-version`/`.prototools` in `dir`
only triggers a warning on mismatch. The command and all its children are
reaped as a process group on stop.

> Note: HCL single-line blocks hold one argument only — write `restart { … }`
> across multiple lines (one argument per line), as above.

---

## service and task

A **service** is a long-running command that comes from outside the doze
registry — anything you can start from a shell. It sleeps when nothing is using
it and wakes when something connects, or when something that depends on it
starts. A **task** is a command that runs to completion, such as a migration.

```hcl
service "mailpit" {
  run  = "mailpit --smtp 127.0.0.1:$PORT"
  port = 1025
}

task "migrate" {
  run    = "go run ./cmd/migrate up"
  inputs = ["migrations/*.sql"]
  env    = { DATABASE_URL = module.db.url }
}

app "api" {
  run   = "go run ./cmd/api"
  port  = 8080
  after = [task.migrate]
  env   = { SMTP = "smtp://${service.mailpit.address}" }
}
```

A service takes `run`, `dir`, `port`, `env`, `env_file`, `after`, `ready`,
`ready_timeout`, `setup`, `on_ready`, `stop`, `restart` and `sleep` as an app
does. It listens on `$PORT` (a private port; doze holds `port` itself), and
`$DATA` is a folder of its own that survives restarts.

A `service` with a `download` (a URL, or a map of `mac` / `mac_intel` /
`linux` / `linux_arm` to a URL) is checked but **cannot be run yet**: `doze up`
refuses a stack that declares one.

### The moments of a command

`setup`, `on_ready` and `stop` are lists of shell lines run around `run`:

| | Runs | If it fails |
|---|---|---|
| `setup` | Once per data folder: the first time, after `doze reset`, or when the list changes. | The start fails and `run` does not start. |
| `run` | Every wake; the one command that stays running. | Restarted with a backoff, then failed. |
| `ready` | Polled after `run` starts. Default: the private port accepts a connection. | The start fails after `ready_timeout`. |
| `on_ready` | Every wake, after ready. Connections that woke it wait for it. | The command keeps running but is not ready, so what depends on it does not start. Fix it and `doze up <name>` runs this step again, and only it. |
| `stop` | Every sleep, instead of the signals. | Logged; the signals follow. |

With no `stop`, a command is asked to exit with SIGTERM, then killed after a
grace period. `run` must stay in the foreground: `docker run -d` returns at
once and looks like an exit. Each step is a line in `doze logs` and an event in
the journal, with how long it took.

### Tasks

A task takes `run`, `dir`, `env`, `env_file`, `after`, `inputs` and `manual`. It
runs, in order:

- when something that lists it in `after` (or references it) starts, if it has
  never succeeded, failed last time, or its `inputs` changed — the content of
  the files the globs match, not their timestamps;
- when you run `doze task <name>`, whatever its state.

A `manual = true` task runs only the second way. A task that fails stops what
is after it, and runs again the next time. `doze task` with no name lists every
task and how it last ran; the result is kept in `.doze/state.json`.

---

## modules

Every `module` block is backed by a **module** — a signed plugin doze fetches
from the doze registry on first use. `module postgres "db"` resolves to the
module `doze/postgres`; doze selects the newest release compatible with your
doze and your declared engine versions, and `doze.lock` freezes the choice.
There is one registry and nothing to configure: no mirror, no per-module
source, no `modules {}` block. `DOZE_MODULES=off` disables fetching (offline,
apps only), and `DOZE_<TYPE>_PLUGIN` runs a module you built yourself.

Module workflow in one breath: discovery and docs live on the
[registry](https://doze.nerdmenot.in/registry/); updates are explicit —
`doze modules upgrade --check` reports, `doze modules upgrade` moves the pins
(commit the updated `doze.lock`). A declared engine version the pinned module
doesn't support fails with exactly that upgrade command. See the
[CLI reference](/reference/cli/).

## Splitting config across files

Root settings live in `doze.hcl`; instance blocks may be split across sibling
`*.doze.hcl` files (merged automatically), or pass `--config <dir>` to merge
every `*.hcl` in a directory. See [Files & storage](/guides/files-and-storage/#breaking-config-into-files).

## Versions & the lockfile

A bare major (`version = 16`) resolves to the newest minor and is pinned in
`doze.lock`; a dotted string (`version = "16.14"`) pins exactly. The lock also
pins each engine's **module** (release, plugin protocol, supported engine
versions, per-platform checksums).
**Commit `doze.lock`** — every machine then runs byte-identical modules *and*
engine binaries. Run `doze binaries available <engine>` to see engine versions;
`doze modules upgrade --check` to see waiting module updates. See
[Managing binaries](/reference/binaries/) and [Files & storage](/guides/files-and-storage/#dozelock--commit-it).

## Connection-string environment variables

`doze env` prints these, `doze run --env` runs a command with them set, and
the full set is written to `.doze/endpoints.yaml` for external tooling. An
`app` block does **not** receive them automatically: it gets the environment
it declares, so pass each one by reference (`DATABASE_URL = module.app.url`).
See [Workflows](/guides/workflows/#getting-connection-strings-into-your-code).

| Variable | For |
|---|---|
| `DOZE_<NAME>_URL` | every instance, always |
| `DATABASE_URL` | the single postgres instance (if unambiguous) |
| `REDIS_URL` | the single valkey/kvrocks instance |
| `MONGODB_URI` | the single ferret instance |
| `AWS_ENDPOINT_URL_S3` / `_SQS` / `_SNS` | the AWS services, plus `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION` (dummy values) |
