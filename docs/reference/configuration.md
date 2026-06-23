# Configuration reference

doze reads HCL from `doze.hcl` (and any sibling `doze.d/*.hcl`, merged
automatically — see [splitting config](#splitting-config-across-files)). The file
has a fixed **root** plus one **block per instance**, keyed by engine:

```hcl
defaults { idle_timeout = "5m" }          # root settings

postgres "app"   { version = 16 }         # instances
valkey   "cache" { version = 9 }
```

Jump to an engine: [postgres](#postgres) · [valkey](#valkey) ·
[kvrocks](#kvrocks) · [ferretdb](#ferretdb) · [s3](#s3) · [sqs](#sqs) ·
[sns](#sns).

This is the field-by-field reference. For what each engine *is* and when to use
it, see **[The engines](../guide/engines.md)**.

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
| `version` | string/number | — | A major (`16` → newest 16.x, pinned) or exact (`"16.14"`). **Required** for database engines; the built-in AWS engines (`s3`/`sqs`/`sns`) take no version. |
| `listen` | string | next port from root `listen` | Per-instance client address (`"127.0.0.1:5544"` or `"unix:/path.sock"`). |

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

## ferretdb

MongoDB-wire front end backed by a Postgres instance (with the `documentdb`
extension). See the [FerretDB recipe](../recipes/ferretdb.md).

```hcl
ferretdb "docs" {
  version = 2
  backend = "docs_pg"
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `version` | string/number | — | Major or exact. **Required.** |
| `backend` | string | — | Name of a declared `postgres` instance. **Required.** |

---

## s3

Local object storage. Buckets are created on boot / `doze up`.

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
  sqs = "jobs"
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

## Splitting config across files

Root settings live in `doze.hcl`; instance blocks may be split into a sibling
`doze.d/*.hcl` directory (merged automatically), or pass `--config <dir>` to merge
every `*.hcl` in a directory. See [Files & storage](../guide/files-and-storage.md#breaking-config-into-files).

## Versions & the lockfile

A bare major (`version = 16`) resolves to the newest minor and is pinned in
`doze.lock`; a dotted string (`version = "16.14"`) pins exactly. **Commit
`doze.lock`** so every machine downloads byte-identical binaries. Run
`doze versions <engine>` to see what's available. See [Managing binaries](../BINARIES.md).

## Environment variables doze injects

`doze run` / `doze env` export:

| Variable | For |
|---|---|
| `DOZE_<NAME>_URL` | every instance, always |
| `DATABASE_URL` | the single postgres instance (if unambiguous) |
| `REDIS_URL` | the single valkey/kvrocks instance |
| `MONGODB_URI` | the single ferretdb instance |
| `AWS_ENDPOINT_URL_S3` / `_SQS` / `_SNS` | the AWS services, plus `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION` (dummy values) |
