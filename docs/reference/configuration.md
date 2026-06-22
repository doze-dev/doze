# Configuration reference

doze reads HCL from `doze.hcl` (and any sibling `doze.d/*.hcl`, merged
automatically — see [splitting config](#splitting-config-across-files)). The file
has a fixed **root** plus one **block per instance**, keyed by engine.

```hcl
# root settings
defaults { idle_timeout = "5m" }

# instances
postgres "app"   { version = 16 }
valkey   "cache" { version = 9 }
```

## Root

| Key | Default | Meaning |
|---|---|---|
| `listen` | `127.0.0.1:6432` | Base client address. Each instance gets the next port; override per instance with `listen`, or use a `unix:/path`. |
| `home` | `$DOZE_HOME` or `~/.doze` | Shared toolchain store + cache (deduplicated across projects). |
| `data_dir` | `<home>/projects/<slug>` | This project's state (data dirs, sockets, logs). |
| `defaults { idle_timeout }` | `5m` | Reap an instance after this long at **zero connections**. A Go duration. |
| `tls { … }` | off | Terminate client TLS for Postgres — see [TLS](#tls). |

### TLS

```hcl
tls {}                          # auto-generate a self-signed cert; sslmode=require works
```
```hcl
tls {
  cert     = "./server.crt"     # bring your own PEM cert + key (set both or neither)
  key      = "./server.key"
  required = true               # reject plaintext TCP clients
}
```
TLS is terminated at the proxy; the backend speaks plaintext over a local unix
socket.

## Instances: common fields

Every instance block is `<engine> "<name>" { … }` and accepts:

| Field | Applies to | Meaning |
|---|---|---|
| `version` | databases | A major (`16` → newest 16.x, pinned) or exact (`"16.14"`). **Required** for database engines. |
| `listen` | all | Per-instance client address override (e.g. `"127.0.0.1:5544"` or `"unix:/tmp/app.sock"`). |

> The built-in AWS engines (`s3`, `sqs`, `sns`) ship inside doze and take **no**
> `version`.

## `postgres`

```hcl
postgres "app" {
  version         = 16
  owner           = "app"        # role that owns the database (defaults to "postgres")
  encoding        = "UTF8"
  locale          = "C"
  template        = "template0"
  shared_buffers  = "16MB"
  max_connections = 50
  fsync           = false        # fast, not crash-safe — ideal for dev/tests
  autovacuum      = false
  extensions      = ["uuid-ossp", "pg_trgm"]
}
```

The database is named after the instance. Nested blocks:

**`role "<name>" { … }`** — a user (a role with `login`, the default) or group role.

| Field | Meaning |
|---|---|
| `password` | login password |
| `login` | `true` (default) for a user; `false` for a group role |
| `superuser`, `createdb`, `createrole`, `replication`, `inherit` | role attributes (bool) |
| `connection_limit` | max concurrent connections |
| `valid_until` | password expiry timestamp |
| `member_of` | list of roles to inherit (`["readonly"]`) |

**`schema "<name>" { owner = "<role>" }`** — a schema and its owner.

**`grant { … }`** — a privilege grant. Requires `role` + `privileges`; scope with
`database`, or `schema` (+ optional `objects`).

| Field | Meaning |
|---|---|
| `role` | grantee role (required) |
| `privileges` | e.g. `["ALL"]`, `["SELECT", "INSERT"]` (required) |
| `database` | grant at the database level |
| `schema` | grant within a schema |
| `objects` | with `schema`: `tables` / `sequences` / `functions` (covers future objects too) |

**`extension "<name>" { … }`** — a single extension with options (`version`,
`schema`, `source`). The shorthand `extensions = [...]` covers simple cases. See
[Extensions](../EXTENSIONS.md).

## `valkey` / `kvrocks`

Redis-protocol engines. Valkey is in-memory; Kvrocks is RocksDB-backed (durable).

```hcl
valkey "cache" {
  version   = 9
  maxmemory = "256mb"     # valkey only
  password  = "secret"    # optional
}

kvrocks "store" {
  version  = 2
  password = "secret"     # optional
}
```

## `ferretdb`

MongoDB-wire front end backed by a Postgres instance (with the `documentdb`
extension). See [the FerretDB recipe](../recipes/ferretdb.md).

```hcl
ferretdb "docs" {
  version = 2
  backend = "docs_pg"     # name of a declared postgres instance (required)
}
```

## `s3`

```hcl
s3 "media" {
  bucket "uploads" {}
  bucket "thumbs" {
    versioning = true     # honored by clients; the dev backend is best-effort
  }
}
```

`bucket "<name>" { versioning }` — buckets created on boot / `doze up`.

## `sqs`

```hcl
sqs "jobs" {
  queue "emails" {
    visibility_timeout = "30s"
    delay              = "0s"
    retention          = "96h"    # 4 days (Go durations or bare seconds)
    wait_time          = "10s"    # default long-poll wait
    max_message_size   = 262144
  }
  queue "orders.fifo" {           # FIFO names must end in .fifo
    fifo                = true
    content_based_dedup = true
  }
  queue "emails-dlq" {}
  redrive "emails" {              # dead-letter after N receives
    dead_letter       = "emails-dlq"
    max_receive_count = 5
  }
}
```

## `sns`

```hcl
sns "events" {
  sqs = "jobs"                    # backing SQS instance for fanout (optional)

  topic "signups" {}
  subscribe "signups" {
    protocol = "sqs"              # sqs | http | https
    endpoint = "emails"          # queue name/ARN, or a webhook URL
    raw      = true              # raw delivery (no SNS envelope)
    filter   = { eventType = ["created", "updated"] }   # message-attribute filter policy
  }
}
```

## Splitting config across files

Root settings live in `doze.hcl`; instance blocks may be split into a sibling
`doze.d/*.hcl` directory (merged automatically), or pass `--config <dir>` to merge
every `*.hcl` in a directory. Useful for grouping by concern, or per-developer
overrides in a gitignored `doze.d/local.hcl`. See the
[config-layout recipe](../recipes/config-layout.md).

## Environment variables doze injects

`doze run` / `doze env` export:

| Variable | For |
|---|---|
| `DOZE_<NAME>_URL` | every instance, always |
| `DATABASE_URL` | the single postgres instance (if unambiguous) |
| `REDIS_URL` | the single valkey/kvrocks instance |
| `MONGODB_URI` | the single ferretdb instance |
| `AWS_ENDPOINT_URL_S3` / `_SQS` / `_SNS` | the AWS services, plus `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION` (dummy values) |
