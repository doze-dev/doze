<div align="center">

# doze

### Real databases on your laptop — asleep until you need them.

doze runs **Postgres, Valkey, Kvrocks, FerretDB**, and local **S3, SQS, and SNS**
as real services — no Docker, no JVM. Declare what your app needs in one file;
doze fetches the real engines, boots each the moment something connects, and puts
it back to sleep when you walk away. At rest, your laptop is quiet: only a tiny
daemon runs.

[![CI](https://github.com/NerdMeNot/doze/actions/workflows/ci.yml/badge.svg)](https://github.com/NerdMeNot/doze/actions/workflows/ci.yml)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue)](LICENSE)
[![Platforms](https://img.shields.io/badge/platform-macOS%20%7C%20Linux-lightgrey)](#install)

</div>

```hcl
# doze.hcl — your whole local backend, declared
postgres "app"   { version = 16 }
valkey   "cache" { version = 9 }
s3 "uploads" {
  bucket "files" {}
}
```

```sh
$ doze run -- <your tests>      # npm test · pytest · go test · cargo test · rails test
  ⏵ postgres/app (16.14) ready 0.2s   ⏵ valkey/cache ready 0.05s   ⏵ s3/uploads ready
  DATABASE_URL, REDIS_URL, AWS_ENDPOINT_URL_S3 injected — and your command just runs
```

No `docker-compose.yml`. No 2 GB daemon humming while you're at lunch. No "wait,
which Postgres is on 5432?" Just the services your app needs, exactly when it
needs them — and nothing when it doesn't.

---

## Why doze?

Building a real app locally means running real infrastructure: a database, a
cache, maybe a queue, a bucket, a document store. Today that usually means one of
three bad options.

**You reach for Docker**, and now a 2 GB daemon idles in the background whether
you're using it or not. Every project carries a `docker-compose.yml` you
copy-paste and tweak forever. Cold starts are slow, healthchecks flake, volumes
fight you over permissions, and on Apple Silicon half your images run emulated
while the fan never stops. The *entire* stack runs all the time — even when you're
only touching one piece of it.

**Or you `brew install` everything**, and now you're juggling versions across
projects, fighting port conflicts, guessing which `postgres` is actually running,
and there's no way to pin the exact versions your teammate has.

**Or you want local AWS**, so you bolt on LocalStack — Python, a JVM, and yet
more Docker.

It's heavy, it's slow, and it's never *quite* reproducible.

doze is the opposite:

|  | Without doze | With doze |
|---|---|---|
| **Setup** | a `docker-compose.yml` per project | one `doze.hcl` |
| **Idle cost** | ~2 GB always running | ~0 — a tiny daemon; engines sleep |
| **Footprint** | emulated images, hot fan | native binaries, quiet laptop |
| **Reproducible** | "works on my machine" | exact versions pinned in `doze.lock` |
| **Startup** | boot the whole stack | each service boots on first connect |
| **Local AWS** | LocalStack (Python + JVM + Docker) | S3/SQS/SNS built into the binary |

And because doze runs **real, unmodified engines** — not emulations — every
client, every extension, every wire-protocol feature behaves exactly like
production. It just gets out of your way the moment you stop using it.

## See it work (60 seconds)

```sh
# 1. Build it (one binary, Go 1.26+)
go install github.com/nerdmenot/doze/cmd/doze@latest

# 2. Describe what you need
doze init                      # writes a starter doze.hcl

# 3. Use it — the database boots on the first connection
doze psql app                  # opens a real psql shell (cold-boots `app` for you)
doze run -- <your command>     # injects DATABASE_URL & co. into any command/language
eval "$(doze env)"             # or export the connection strings into your shell
```

That's the whole loop. The first connection boots the engine and converges it to
your declared shape (database, roles, schemas, extensions); the next connection
is instant; stop touching it and, a few minutes later, it reaps back to zero.

```sh
$ doze status
  NAME    ENGINE   STATE    CONNS   RAM    UPTIME   ENDPOINT
  app     postgres active   1       28M    8s       127.0.0.1:6432
  cache   valkey   reaped   0              -        127.0.0.1:6433
```

## What you can run

| Service | What it's for | Declare |
|---|---|---|
| **PostgreSQL** | your primary database — roles, schemas, extensions, the works | `postgres "app" { … }` |
| **Valkey** | a Redis-compatible in-memory cache | `valkey "cache" { … }` |
| **Kvrocks** | Redis-compatible, RocksDB-backed durable KV | `kvrocks "store" { … }` |
| **FerretDB** | MongoDB-wire document store (on a Postgres backend) | `ferretdb "docs" { … }` |
| **S3** | local object storage (buckets, multipart, presigned URLs) | `s3 "media" { … }` |
| **SQS** | message queues (standard, FIFO, dead-letter) | `sqs "jobs" { … }` |
| **SNS** | pub/sub with SNS→SQS fanout and webhooks | `sns "events" { … }` |

Mix as many as you want in one file. → **[See real recipes for each](docs/recipes/README.md)**

## How it works

Five ideas, and you've got the whole model:

1. **Declare, don't orchestrate.** You describe the *end state* in `doze.hcl` —
   which engines, which versions, which databases and roles. doze makes it so.
2. **Lazy boot.** A tiny daemon listens on one address per instance. The first
   connection cold-boots the real engine behind it; doze then splices your
   connection straight through, byte for byte.
3. **Idle reap.** When an instance has had zero connections for a while, doze
   shuts it down. Walk away and your laptop goes quiet on its own.
4. **Real engines, pinned.** doze downloads the actual upstream binaries, verifies
   their checksums, and records exact versions in `doze.lock` — so your whole
   team and your CI run byte-identical software.
5. **Structure, not data.** doze converges the *shape* of things (databases,
   roles, schemas, grants, extensions, buckets, queues, topics). Your app and
   migrations own the data.

→ **[The full mental model](docs/guide/concepts.md)**

## Is doze for you?

**Great for** local development, automated tests, CI pipelines, demos, and
spinning up a realistic backend for a new project in seconds. If you've ever run
`docker-compose up` just to work on one service, doze is for you.

**Not for** production. doze runs **single** local instances (no replication, no
HA, no failover), tuned toward fast iteration over durability, and reaps them when
idle — so it's not a place to keep data you can't lose. The local AWS services
(S3/SQS/SNS) are dev-grade conveniences, not a stand-in for real AWS. Use managed
Postgres/Redis and real AWS in production. (Full rationale in the
[FAQ](docs/guide/faq.md#is-doze-production-ready).)

**Platforms:** macOS and Linux, on Apple Silicon and x86-64. (No native Windows;
WSL2 works.)

## Install

**Prerequisites:** Go 1.26+, macOS or Linux (Apple Silicon or x86-64). You do
*not* install Postgres, Redis, etc. — doze fetches those for you.

```sh
# Recommended: install the CLI
go install github.com/nerdmenot/doze/cmd/doze@latest

# Or build from a clone
git clone https://github.com/NerdMeNot/doze && cd doze
go build -o doze ./cmd/doze
```

Engine binaries are fetched and cached automatically on first use — you don't
install Postgres or Redis yourself. They're built and published by the companion
repo **[NerdMeNot/doze-binaries](https://github.com/NerdMeNot/doze-binaries)**;
see [Managing binaries](docs/BINARIES.md) for the mirror format and self-hosting.

## Documentation

New here? Read these in order:

1. **[Getting started](docs/guide/getting-started.md)** — from zero to a running app, step by step.
2. **[Core concepts](docs/guide/concepts.md)** — the daemon, lazy boot, reaping, convergence, endpoints.
3. **[Files & storage](docs/guide/files-and-storage.md)** — where things live, what to commit, splitting config.
4. **[Recipes](docs/recipes/README.md)** — copy-pasteable examples for every engine and workflow.

Reference, when you need it:

- **[Configuration](docs/reference/configuration.md)** — every block and field in `doze.hcl`.
- **[CLI](docs/reference/cli.md)** — every command and flag.
- **[Managing binaries](docs/BINARIES.md)** — the mirror, the lockfile, self-hosting.
- **[Extensions](docs/EXTENSIONS.md)** · **[Architecture](docs/ARCHITECTURE.md)** (for contributors)

Stuck or curious? **[Troubleshooting](docs/guide/troubleshooting.md)** ·
**[FAQ](docs/guide/faq.md)**

## Contributing

Issues and PRs welcome — see [CONTRIBUTING](CONTRIBUTING.md). By participating you
agree to the [Code of Conduct](CODE_OF_CONDUCT.md). To report a vulnerability, see
the [security policy](SECURITY.md).

## License

Apache 2.0 — see [LICENSE](LICENSE).
