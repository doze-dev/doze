# Configuration reference

doze reads HCL from `doze.hcl` (and any sibling `*.doze.hcl`, merged
automatically — see [splitting config](#splitting-config-across-files)). The file
has a fixed **root** plus one **block per instance**, keyed by engine:

```hcl
defaults { idle_timeout = "5m" }          # root settings

postgres "app"   { version = 16 }         # instances
valkey   "cache" { version = 9 }
```

Jump to an engine: [postgres](#postgres) · [valkey](#valkey) ·
[kvrocks](#kvrocks) · [ferret](#ferret) · [s3](#s3) · [sqs](#sqs) ·
[sns](#sns). Project-level blocks: [modules](#modules) · [TLS](#tls).

This is the field-by-field reference. For what each engine *is* and when to use
it, see **[The engines](../guide/engines.md)**. Every engine's full argument
reference is also one command away — `doze modules docs <engine>` — generated
from the module itself, so it can't be stale.

## Root

| Field | Type | Default | Description |
|---|---|---|---|
| `listen` | string | `127.0.0.1:6432` | Base client address. Each instance gets the next port; override per instance with `listen`, or use a `unix:/path`. |
| `home` | string | `$DOZE_HOME` or `~/.doze` | Shared toolchain store + cache (deduplicated across projects). |
| `data_dir` | string | `<home>/projects/<slug>` | This project's state (data dirs, sockets, logs). |
| `defaults { idle_timeout }` | duration | `5m` | Reap an instance after this long at **zero connections**. |
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

Every `<engine> "<name>" { … }` block accepts:

| Field | Type | Default | Description |
|---|---|---|---|
| `version` | string/number | — | The **engine** version: a major (`16` → newest 16.x, pinned) or exact (`"16.14"`). **Required** for database engines; the AWS engines (`s3`/`sqs`/`sns`) take no version. |
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
usefully—**reference other instances** by `<engine>.<name>.<attribute>`:

```hcl
sns "events_bus" {
  sqs = sqs.jobs.name              # reference → builds the dependency edge
}
```

A reference does two things: it resolves to the attribute's value, and it makes
the referencing instance **depend on** the referenced one (doze boots and holds
the dependency first — no hand-declared ordering). Referencing an instance that
isn't declared is a parse-time error, and reference cycles are rejected.

Every instance exposes these baseline attributes:

| Attribute | Description |
|---|---|
| `name` / `engine` | The instance name and its engine type. |
| `address` | Client-facing `host:port` (or `unix:/path`). |
| `host` / `port` | Split address (empty/`0` for a unix socket). |
| `socket` | Unix socket path (empty for TCP). |
| `url` | The connection string doze injects (e.g. `postgres://…`). |
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

postgres "app" {
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
(count) — addressable like any other (`valkey.shard_0.url`, `sqs.worker_emails.url`).

```hcl
sqs "worker" {
  for_each = toset(["emails", "orders", "billing"])  # → worker_emails, worker_orders, worker_billing
  queue "main" {}
}

valkey "shard" {
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

### Explicit ordering: `depends_on`

References already create dependencies, so you rarely need this. For an ordering
that isn't expressed through a reference, `depends_on` adds it:

```hcl
sqs "jobs" {
  queue "main" {}
  depends_on = { "s3.media" = "healthy" }
}
```

The key is an instance address (or bare name); the value is the readiness
condition — `healthy` (wait until it accepts connections / its health probe
passes, the default for every reference) or `started` (wait only until the
dependency's process has spawned). `started` lets a service start before a peer
process becomes healthy; for a database/queue, which is ready as soon as it
accepts connections, the two are equivalent.

---

## postgres

Real PostgreSQL (14–17). On boot, doze creates the database (named after the
instance) and converges the declared roles, schemas, grants, and extensions.

```hcl
postgres "app" {
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
| `source` | string | none | Path to a source/bundle for an extension the binary doesn't ship — see [Extensions](../EXTENSIONS.md). |
| `cascade` | bool | `false` | Add `CASCADE` to also create dependency extensions. |
| `optional` | bool | `false` | When `true`, an unavailable or failed extension is a warning, not a hard error. By default a missing/failed extension **fails convergence and taints the instance**. |

---

## valkey

In-memory, Redis-protocol cache.

```hcl
valkey "cache" {
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
kvrocks "store" {
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
separate postgres to wire up. See the [DocumentDB recipe](../recipes/documentdb.md).

```hcl
ferret "shop" {
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

Full argument/block reference: `doze modules docs ferret` or its
[registry page](https://doze.nerdmenot.in/registry/doze/ferret).

---

## s3

Local object storage. Buckets are created on boot / `doze sync`.

```hcl
s3 "media" {
  bucket "uploads" {}
  bucket "thumbs" { versioning = true }
}
```

**`bucket "<name>" { … }`**

| Field | Type | Default | Description |
|---|---|---|---|
| `versioning` | bool | `false` | Enable object versioning (best-effort on the dev backend). |

---

## sqs

Local message queues.

```hcl
sqs "jobs" {
  queue "emails" { visibility_timeout = "30s" }
  queue "emails-dlq" {}
  redrive "emails" {
    dead_letter       = "emails-dlq"
    max_receive_count = 5
  }
}
```

**`queue "<name>" { … }`** — durations accept Go syntax (`30s`, `5m`, `12h`) or bare seconds.

| Field | Type | Default | Description |
|---|---|---|---|
| `fifo` | bool | `false` | FIFO queue (name must end in `.fifo`). |
| `content_based_dedup` | bool | `false` | FIFO: dedupe by body hash (5-minute window). |
| `visibility_timeout` | duration | `30s` | How long a received message stays invisible. |
| `delay` | duration | `0s` | Delivery delay for new messages. |
| `retention` | duration | `96h` (4 days) | How long messages are kept. |
| `wait_time` | duration | `0s` | Default long-poll wait (server caps at `20s`). |
| `max_message_size` | number | `262144` | Max message bytes (256 KiB). |

**`redrive "<queue>" { … }`** — dead-letter policy for the named queue.

| Field | Type | Default | Description |
|---|---|---|---|
| `dead_letter` | string | — | Target dead-letter queue (in the same `sqs` instance). **Required.** |
| `max_receive_count` | number | — | Move to the DLQ after this many receives. **Required.** |

---

## sns

Local pub/sub with SNS→SQS fanout and webhooks.

```hcl
sns "events" {
  sqs = sqs.jobs.name
  topic "signups" {}
  subscribe "signups" {
    protocol = "sqs"
    endpoint = "emails"
    raw      = true
    filter   = { eventType = ["created"] }
  }
}
```

**Block field**

| Field | Type | Default | Description |
|---|---|---|---|
| `sqs` | string | none | Name of a declared `sqs` instance to deliver to (held running while SNS runs). |

**`topic "<name>" { }`** — no fields; just declares the topic.

**`subscribe "<topic>" { … }`** — a subscription on the named topic.

| Field | Type | Default | Description |
|---|---|---|---|
| `protocol` | string | — | `sqs`, `http`, or `https`. **Required.** |
| `endpoint` | string | — | Queue name/ARN (for `sqs`) or a URL (for `http(s)`). **Required.** |
| `raw` | bool | `false` | Raw delivery (deliver the bare message, not the SNS envelope). |
| `filter` | object | none | Message-attribute filter policy, e.g. `{ type = ["a","b"] }`. |

---

## process

An application **process** run directly on the host — no Docker, no
virtualization. Unlike the database/AWS engines (which doze proxies and
idle-reaps), a process is a long-lived, supervised *client* of those backends: it
binds its own port, is exempt from the idle reaper, gates readiness on a health
probe, and restarts per a policy when it exits. Bring processes up with
[`doze up`](cli.md#doze-up-process); they boot eagerly (not on connection).

```hcl
process "api" {
  cwd     = "../approvals-engine"          # working dir, relative to this file
  command = "go run main.go -program api"  # run via `sh -c` (pipes, &&, expansion OK)
  port    = 8080                           # the port the app binds (doze does NOT bind it)

  env = {
    DATABASE_URL  = postgres.app.url       # typed ref → dependency edge + injected value
    HTTP_API_PORT = "8080"
  }
  env_file = ".env.local"                  # optional; lower precedence than env{}

  depends_on = { postgres.app = "healthy" } # explicit; the env refs above also imply edges

  hooks {
    pre_start  = ["go run main.go -program migrate -command up"]  # after deps, before start
    post_start = ["./scripts/notify-ready.sh"]                    # after the app is healthy
    pre_stop   = ["./scripts/drain.sh"]                           # before SIGINT
  }

  health {
    http     = "http://localhost:8080/health/ready"
    interval = "2s"
    timeout  = "3s"
    retries  = 30                          # readiness budget = interval × retries
  }

  restart {
    policy      = "on_failure"             # no | on_failure | always
    backoff     = "1s"                     # exponential, capped at 30s
    max_retries = 5
  }
}
```

| Field | Type | Default | Notes |
|---|---|---|---|
| `command` | string | — | Command line, run via `sh -c`. **Required.** |
| `cwd` | string | the file's dir | Working directory, resolved relative to the declaring file. |
| `port` | number | none | The port the app listens on. Exposed as `process.<name>.{url,host,port}`; doze opens **no** proxy listener for it. Omit for a worker with no endpoint. |
| `env` | map | none | Environment for the process and its hooks. Highest precedence; values may reference other instances. |
| `env_file` | string | none | Path to a `KEY=VALUE` file (lower precedence than `env`). |
| `hooks` | block | none | `pre_start` / `post_start` / `pre_stop` command lists, each run via `sh -c` in `cwd`. A non-zero `pre_start`/`post_start` aborts the boot and taints the instance. |
| `health` | block | none | One probe kind — `http` (2xx), `tcp` (`host:port` accepts), `exec` (exit 0), or `log_line` (regex over logs) — plus `interval`, `timeout`, `retries`. With no `health` block, readiness = "the process stayed alive briefly". |
| `restart` | block | `policy = "no"` | `policy` (`no` / `on_failure` / `always`), `backoff` (base, grown exponentially and capped), `max_retries` (defaults to 5 for a restarting policy). |

The process runs with the full environment [`doze run`](cli.md#doze-run----command-args)
injects (connection strings, AWS creds/region, `DOZE_<NAME>_URL`), layered as:
`os` env → doze-injected → `env_file` → `env {}`. v1 runs `go`/`bun`/`node` from
`PATH`; a `.go-version`/`.prototools` in `cwd` only triggers a warning on
mismatch. The command and all its children are reaped as a process group on stop.

> Note: HCL single-line blocks hold one argument only — write `health { … }` and
> `restart { … }` across multiple lines (one argument per line), as above.

---

## modules

Every engine except `process` is provided by a **module** — a signed plugin
doze fetches from the registry on first use. The defaults are right for almost
everyone: engine type `postgres` resolves to source `doze/postgres` on the
public registry, doze selects the newest module release compatible with your
doze and your declared engine versions, and `doze.lock` freezes the choice.
The optional `modules {}` block holds the overrides:

```hcl
modules {
  mirror  = "file:///path/to/registry"   # air-gapped / dev registry base
  enabled = true                          # (implied when mirror is set)

  cache {                                 # per engine TYPE (the block keyword)
    source  = "acme/valkey"               # a third-party publisher's module
    version = "0.2.0"                     # rare: pin the MODULE release exactly
  }
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `mirror` | string | the public registry | Registry base URL (or `file://` path). Also settable via `DOZE_MODULES_MIRROR`. |
| `enabled` | bool | on | Fetch modules at all. `DOZE_MODULES=off` disables globally (offline / `process`-only). |
| `<type> { source }` | string | `doze/<type>` | Which `<namespace>/<name>` provides this engine type. |
| `<type> { version }` | string | auto | Exact **module** release pin — the escape hatch for holding back a regressed release. Not the engine version. |

Module workflow in one breath: discovery is `doze modules search`, docs are
`doze modules docs <type>`, provenance is `doze modules info <source>`, and
updates are explicit — `doze modules upgrade --check` reports, `doze modules
upgrade` moves the pins (commit the updated `doze.lock`). A declared engine
version the pinned module doesn't support fails with exactly that upgrade
command. See the [CLI reference](cli.md#doze-modules-alias-mod).

## Splitting config across files

Root settings live in `doze.hcl`; instance blocks may be split across sibling
`*.doze.hcl` files (merged automatically), or pass `--config <dir>` to merge
every `*.hcl` in a directory. See [Files & storage](../guide/files-and-storage.md#breaking-config-into-files).

## Versions & the lockfile

A bare major (`version = 16`) resolves to the newest minor and is pinned in
`doze.lock`; a dotted string (`version = "16.14"`) pins exactly. The lock also
pins each engine's **module** (release, plugin protocol, supported engine
versions, per-platform checksums) and each registry namespace's publisher key.
**Commit `doze.lock`** — every machine then runs byte-identical modules *and*
engine binaries. Run `doze binaries available <engine>` to see engine versions;
`doze modules upgrade --check` to see waiting module updates. See
[Managing binaries](../BINARIES.md) and [Files & storage](../guide/files-and-storage.md#dozelock--commit-it).

## Connection-string environment variables

A supervised `process` block receives these env vars for the dependencies it
declares; the full set is also written to `.doze/endpoints.yaml` for external
tooling. (`doze run` does **not** inject them — see
[Workflows](../recipes/workflows.md#getting-connection-strings-into-your-code).)

| Variable | For |
|---|---|
| `DOZE_<NAME>_URL` | every instance, always |
| `DATABASE_URL` | the single postgres instance (if unambiguous) |
| `REDIS_URL` | the single valkey/kvrocks instance |
| `MONGODB_URI` | the single documentdb instance |
| `AWS_ENDPOINT_URL_S3` / `_SQS` / `_SNS` | the AWS services, plus `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION` (dummy values) |
