# The engines

doze runs a deliberately chosen set of engines. Postgres is the real thing. The
rest are picked to be **cheap, real, and license-clean local stand-ins** for
software that's otherwise heavy, costly, or legally encumbered to run yourself —
so you get the API your code already speaks, without the baggage.

Every engine boots on first connect and reaps when idle, the same way
([concepts](concepts.md)). What differs is *what* each one is and *when* you'd
reach for it.

## At a glance

| Engine | Speaks | Use it as | Why this one |
|---|---|---|---|
| **PostgreSQL** | the Postgres wire protocol | your primary SQL database | the real, unmodified upstream |
| **Valkey** | the Redis (RESP) protocol | an in-memory cache | the open-source Redis after the 2024 relicense |
| **Kvrocks** | the Redis (RESP) protocol | a durable, disk-backed KV store | Redis API without keeping everything in RAM |
| **DocumentDB** | the MongoDB wire protocol | a document store | "Mongo" without MongoDB's license, on Postgres |
| **S3 / SQS / SNS** | the AWS APIs | object storage, queues, pub/sub | local AWS with no LocalStack, Docker, or JVM |

## PostgreSQL — the real database

doze runs genuine upstream PostgreSQL (majors 14–17). Not a fork, not an
emulation — the same binary you'd run in production, so every extension, every
client, and every wire feature behaves identically. On first boot doze creates
the database and converges your declared roles, schemas, grants, and extensions,
then gets out of the way.

→ [PostgreSQL recipes](../recipes/postgres.md)

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

→ [Valkey & Kvrocks recipes](../recipes/valkey-kvrocks.md)

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
  version = "2.7"        # the FerretDB gateway version
  port    = 27017
}
```

→ [DocumentDB recipes](../recipes/documentdb.md)

## S3, SQS, SNS — local AWS, no LocalStack

The usual way to fake AWS locally is LocalStack: a ~1.2 GB Docker image running
Python, with a JVM behind some services. doze takes a different path — **S3, SQS,
and SNS are implemented in pure Go and compiled into the doze binary.** There's no
image to pull, no Python, no Java; doze runs each as a short-lived child process
behind the same proxy, so they cold-boot, persist to disk, and reap like every
other engine. Your AWS SDK talks to them through the injected
`AWS_ENDPOINT_URL_*` variables, unchanged.

- **S3** — object storage with buckets, multipart uploads, and presigned URLs
  (embeds [gofakes3](https://github.com/johannesboyne/gofakes3)).
- **SQS** — standard and FIFO queues, dead-letter redrive, long polling.
- **SNS** — topics, SNS→SQS fanout, filter policies, and HTTP webhooks.

These are dev-grade conveniences — fast and faithful enough to build and test
against, not a replacement for real AWS in production.

→ [S3](../recipes/s3.md) · [SQS](../recipes/sqs.md) · [SNS](../recipes/sns.md)
recipes

---

**Next:** put them together in **[Recipes](../recipes/README.md)**, or see every
field in the **[Configuration reference](../reference/configuration.md)**.

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
