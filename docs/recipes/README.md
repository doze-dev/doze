# doze recipes

Practical, copy-pasteable examples of what doze can do. Every recipe is a
**config** (`doze.hcl`) plus the **commands** to use it, with a note on what you
get.

New to doze? Start with **[Getting started](../guide/getting-started.md)** and
**[Core concepts](../guide/concepts.md)** first — these recipes assume you know
the basic loop (declare → `doze run` → boots on connect, reaps when idle). For
what each engine *is* and when to reach for it, see **[The
engines](../guide/engines.md)**.

## How every recipe works

1. **Declare** instances in `doze.hcl` (or split across `doze.d/*.hcl`).
2. **Use** them — either let doze inject connection strings:
   ```sh
   doze run -- <your command>     # ensures up, injects env, runs the command
   eval "$(doze env)"             # or export the env into your shell
   ```
   or connect a client directly to an instance's endpoint (doze boots it on the
   first connection and reaps it when idle).

Each instance gets a unique **`DOZE_<NAME>_URL`**, and the conventional variable
for its engine when exactly one instance claims it:

| Engine | Conventional var |
|---|---|
| postgres | `DATABASE_URL` |
| valkey / kvrocks | `REDIS_URL` |
| ferretdb | `MONGODB_URI` |
| s3 / sqs / sns | `AWS_ENDPOINT_URL_S3` / `_SQS` / `_SNS` (+ dummy `AWS_*` creds) |

doze converges **structure** (databases, roles, schemas, grants, extensions,
buckets, queues, topics) — never data. Your app/migrations own the data.

## Index

- [PostgreSQL](postgres.md) — roles, schemas, grants, extensions, multiple DBs, tuning, versions
- [Valkey & Kvrocks](valkey-kvrocks.md) — Redis-protocol cache and durable KV
- [FerretDB](ferretdb.md) — MongoDB wire on a Postgres backend
- [S3](s3.md) — local object storage (buckets, multipart, presigned URLs)
- [SQS](sqs.md) — queues, FIFO, DLQ + redrive
- [SNS](sns.md) — topics, SNS→SQS fanout, filter policies, webhooks
- [Workflows](workflows.md) — `run`/`env`, ephemeral test DBs, status/dash/logs, CI
- [Config layout](config-layout.md) — splitting config across `doze.d` files + per-dev overrides
- [Full stacks](stacks.md) — polyglot apps end to end + framework wiring

For where doze stores engines, data, sockets, and logs — and what to commit vs
ignore — see the **[Files & storage guide](../guide/files-and-storage.md)**.
