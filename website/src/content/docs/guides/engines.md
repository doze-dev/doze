---
title: "The engines"
description: Every engine doze runs, what it speaks, which versions you can declare, and where it works.
---

doze runs a deliberately chosen set of engines. Postgres is the real thing. The
rest are picked to be **cheap, real, and license-clean local stand-ins** for
software that's otherwise heavy, costly, or legally encumbered to run yourself —
so you get the API your code already speaks, without the baggage.

Every engine boots on first connect and reaps when idle, the same way
([concepts](/start/concepts/)). What differs is *what* each one is and *when*
you'd reach for it.

## At a glance

The **version** column is the one you write in `doze.hcl` — it's the engine's
own version (the actual Postgres major, the Kafka protocol level), never a
plugin or SDK number. Each engine links to its own page — versions, platforms,
config reference, and release history, generated from the module itself at
build time, so they can't drift.

| Engine | Speaks | Use it as | `version =` | Runs on |
|---|---|---|---|---|
| [**PostgreSQL**](/engines/postgres/) | the Postgres wire protocol | your primary SQL database | 14 – 18 | macOS · Linux |
| [**Valkey**](/engines/valkey/) | Redis (RESP) | an in-memory cache | 8 – 9 | macOS · Linux |
| [**Kvrocks**](/engines/kvrocks/) | Redis (RESP) | a durable, disk-backed KV store | 2 | macOS · Linux |
| [**DocumentDB**](/engines/ferret/) | the MongoDB wire protocol | a document store | 2 | macOS · Linux |
| [**MariaDB**](/engines/mariadb/) | MySQL | a MySQL-compatible SQL database | 11 | macOS · Linux |
| [**Temporal**](/engines/temporal/) | Temporal gRPC + Web UI | a durable workflow engine | 1.1 | macOS · Linux |
| [**Kafka**](/engines/kafka/) | the Kafka protocol | an event stream / message log | 1 – 4 | macOS · Linux |
| [**AWS**](/engines/aws/) | the AWS APIs (S3, SQS, SNS, DynamoDB, Lambda, EventBridge, KMS, SSM, Secrets Manager) | the whole local cloud, one block | — | macOS · Linux |
| **process** | anything you can run | *your own* services (API, worker, frontend…) | — | macOS · Linux |

That last row is easy to overlook and shouldn't be: the `process` engine runs
**your** services — supervised, ordered, health-gated — so a `doze.hcl` can
orchestrate a whole microservice stack, with or without any database in it. This
page covers the backing engines; the [microservices guide](/guides/microservices/)
covers running your code.

## PostgreSQL — the real database

doze runs genuine upstream PostgreSQL (majors 14–18). Not a fork, not an
emulation — the same binary you'd run in production, so every extension, every
client, and every wire feature behaves identically. On first boot doze creates
the database and converges your declared roles, schemas, grants, and extensions,
then gets out of the way.

```hcl
postgres "app" {
  version = 18
  database "app" {}
}
```

→ [PostgreSQL recipes](/guides/recipes/postgres/)

## Valkey — the open-source Redis

In March 2024, Redis Inc. relicensed Redis (starting with 7.4) under the dual
RSALv2 / SSPLv1 licenses — by their own statement, "Redis is no longer open
source under the OSI definition."[^redis] In response, the community forked the
last open-source release (7.2.4) into **Valkey**, a BSD-3-Clause project
stewarded by the Linux Foundation.[^valkey]

For you, that means **Valkey is Redis** for all practical local-dev purposes: it
speaks the same RESP protocol, so your existing Redis clients and `REDIS_URL`
work unchanged — it's just the version that stayed open source. Use it as a fast,
in-memory cache.

```hcl
valkey "cache" {
  version   = 9
  maxmemory = "256mb"
}
```

## Kvrocks — Redis on disk, for less RAM

Apache Kvrocks is a key-value store that **speaks the Redis protocol but persists
to disk via RocksDB** instead of holding everything in memory.[^kvrocks] It's an
Apache Software Foundation project (Apache-2.0).

Reach for Kvrocks over Valkey when your dataset is large and you'd rather not pay
to keep it all resident in RAM, or when you want the keys to **survive a reap and
a restart** without configuring snapshots. Same client code, different trade-off:
Valkey optimizes for in-memory speed and volatility; Kvrocks for durability and a
small memory footprint.

```hcl
kvrocks "store" {
  version = 2
}
```

**Valkey or Kvrocks?** Cache that can vanish → Valkey. Durable KV you don't want
living in RAM → Kvrocks. Both talk Redis, so you can switch by changing one block.

→ [Valkey & Kvrocks recipes](/guides/recipes/valkey-kvrocks/)

## DocumentDB — MongoDB-compatible, on Postgres

MongoDB moved its server to the SSPL in 2018, a license the OSI never
approved.[^mongo] doze's **DocumentDB** engine speaks the MongoDB wire protocol,
so your MongoDB drivers and `MONGODB_URI` connect unchanged — backed by
**PostgreSQL** with Microsoft's DocumentDB extension, behind a FerretDB
gateway.[^ferret]

So you get a document store with the Mongo API, locally, without running MongoDB
itself or accepting its license. It's a single, **self-contained** engine: you
declare one block, and doze runs the private Postgres and the gateway for you,
exposing only Mongo — no backend to wire up. A faithful local stand-in for
development, not a reimplementation of every MongoDB feature.

```hcl
ferret "docs" {
  version = 2
  port    = 27017
}
```

→ [DocumentDB recipes](/guides/recipes/documentdb/)

## MariaDB — MySQL, without Oracle

MariaDB is the community fork of MySQL (GPLv2, MariaDB Foundation), begun by
MySQL's original authors after the Oracle acquisition. It speaks the MySQL
protocol, so `mysql://` clients and drivers connect unchanged. doze runs the
upstream 11.4 LTS series.

MariaDB publishes a portable binary only for x86_64 Linux, so doze repackages
that one and **builds every other platform from source** (the postgres
approach) — you get the same engine on Apple Silicon Macs and arm64 Linux
without upstream shipping a binary for them.

```hcl
mariadb "db" {
  version = 11.4
}
```

## Temporal — durable workflows, one binary

Temporal's dev server is a single pure-Go binary bundling the Temporal
services, a SQLite store, and the Web UI — no JVM, no Docker, no external
database.[^temporal] doze supervises it like everything else (workers long-poll
it, so it stays awake while in use), converges your declared namespaces, and
exposes both the gRPC frontend and the Web UI.

```hcl
temporal "dev" {
  version = 1.1
  port    = 7233
  ui_port = 8233

  namespace "orders" {}
}
```

## Kafka — the protocol, without the JVM

Kafka itself is a JVM heavyweight; the usual local answer is Docker plus a
gigabyte of images. doze's Kafka engine is a **single-node, Kafka-protocol
broker written in Go** (the [doze-kafka](https://github.com/doze-dev/doze-kafka)
project) — real wire protocol, real consumer groups (classic and KIP-848),
compaction, retention — verified against franz-go, usable from any Kafka
client. The `version` you declare is the **Kafka protocol profile** (1–4) your
clients expect, not a broker build.

It ships with a **web console** — topics, a live message tape, a produce
panel, consumer groups with lag — served one port above the broker
(`:9093` for a `:9092` broker).

```hcl
kafka "events" {
  version = 4

  topic "orders"   { partitions = 3 }
  topic "payments" { config = { "cleanup.policy" = "compact" } }
}
```

A dev-grade single node for building and testing against — not a replacement
for a production cluster.

→ [Kafka recipe](/guides/recipes/kafka/)

## AWS — the whole local cloud, one block

The usual way to fake AWS locally is LocalStack: a ~1.2 GB Docker image running
Python, with a JVM behind some services. doze takes a different path: **one
`aws` block runs the whole local cloud as a single ~20 MB process** — S3, SQS,
SNS, DynamoDB (with Streams), Lambda, EventBridge, KMS, SSM Parameter Store,
and Secrets Manager, all pure Go (the
[doze-aws](https://github.com/doze-dev/doze-aws) project), behind one endpoint
your SDK already understands.

Buckets, queues, topics, tables, functions, and rules are **declared inside the
block** and converged on boot. A **web console** at `/_console` covers all of it
— browse a bucket, peek a queue, invoke a function, watch live API traffic.

```hcl
aws "local" {
  bucket "uploads" {}
  queue  "emails"  { dlq = "auto" }
  topic  "signups" { subscribe { queue = "emails" } }
  table  "sessions" { key = "session_id:S"  ttl = "expires_at" }
}
```

There is no `version =` here — the services track current AWS APIs, and the
whole thing is dev-grade: fast and faithful enough to build and test against,
not a replacement for real AWS in production.

→ [AWS recipe](/guides/recipes/aws/)

---

**Next:** put them together in **[Recipes](/guides/recipes/postgres/)**, or see every
field in the **[Configuration reference](/reference/configuration/)**.

[^redis]: Redis adopted dual RSALv2 / SSPLv1 licensing starting with Redis 7.4
    (March 2024); per Redis, neither is an OSI-approved open-source license. See
    [Redis's announcement](https://redis.io/blog/redis-adopts-dual-source-available-licensing/).

[^valkey]: Valkey forked from the Redis 7.2.4 codebase and is a BSD-3-Clause
    project under the Linux Foundation. See
    [the Linux Foundation launch announcement](https://www.linuxfoundation.org/press/linux-foundation-launches-open-source-valkey-community)
    and [valkey.io](https://github.com/valkey-io/valkey).

[^kvrocks]: Apache Kvrocks is a RESP-compatible, RocksDB-backed key-value store
    and an Apache Software Foundation project. See
    [kvrocks.apache.org](https://kvrocks.apache.org/).

[^mongo]: MongoDB issued the SSPL for MongoDB Community Server in October 2018;
    the SSPL is not OSI-approved. See
    [MongoDB's announcement](https://www.mongodb.com/company/newsroom/press-releases/mongodb-issues-new-server-side-public-license-for-mongodb-community-server).

[^ferret]: doze's DocumentDB engine pairs Microsoft's [DocumentDB
    extension](https://github.com/microsoft/documentdb) for PostgreSQL with a
    [FerretDB](https://github.com/FerretDB/FerretDB) gateway (Apache-2.0) that
    speaks the MongoDB wire protocol — all run privately and exposed as one engine.

[^temporal]: The [Temporal CLI](https://github.com/temporalio/cli) dev server
    (MIT) bundles the server, persistence, and Web UI in one process — the same
    tool `temporal server start-dev` runs.
