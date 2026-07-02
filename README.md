<div align="center">

<img src="docs/assets/logo.png" alt="doze" width="110" />

# doze

### Real databases on your laptop — asleep until you need them.

doze runs **Postgres, Valkey, Kvrocks, DocumentDB**, and local **S3, SQS, and SNS**
as real services — no Docker, no JVM, no always-on stack. Declare what your app
needs in one file; doze fetches the real engines, boots each the moment something
connects, and puts it back to sleep when you walk away. At rest, your whole
backend is one ~15 MB daemon.

[![CI](https://github.com/doze-dev/doze/actions/workflows/ci.yml/badge.svg)](https://github.com/doze-dev/doze/actions/workflows/ci.yml)
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
  ✓ app (postgres 16) ready   ✓ cache (valkey 9) ready   ✓ uploads (s3) ready
  real engines, booted on demand on the ports you declared — gone again when you walk away
```

No `docker-compose.yml`. No multi-gigabyte VM humming while you're at lunch. No
"wait, which Postgres is on 5432?" Just the services your app needs, exactly when
it needs them — and nothing when it doesn't.

---

## Why doze?

Running real infrastructure locally — a database, a cache, a queue, a bucket —
usually means one of three taxes:

- **Docker / docker-compose:** a Linux VM that reserves ~half your RAM and runs
  all day, the whole stack up even when you're touching one service, images
  emulated on Apple Silicon.
- **`brew install` everything:** version drift across projects, port conflicts,
  no pinning, services idling since login.
- **LocalStack for AWS:** a ~1.2 GB image running Python and a JVM, inside Docker.

doze is the opposite — **real engines that sleep**. It runs the genuine binaries,
boots each on first connect, splices your connection straight through, and
returns the RAM when you walk away.

| | Docker / compose | `brew install` | LocalStack | **doze** |
|---|---|---|---|---|
| Setup | a compose file per project | manual, per-machine | a container | **one `doze.hcl`** |
| Idle cost | a VM, ~½ your RAM | services run at login | Docker + Python + JVM | **~15 MB, engines asleep** |
| Run only what you use | no — whole stack | no | no | **yes — boot on connect** |
| Pinned versions | image tags | none | image tag | **`doze.lock`, exact** |
| Apple Silicon | often emulated | native | emulated | **native** |
| Local AWS | extra tooling | n/a | yes (heavy) | **built in, pure Go** |

Because doze runs **real, unmodified engines** — not emulations — every client,
extension, and wire-protocol feature behaves exactly like production. It just gets
out of your way the moment you stop using it.

→ **[The full case](docs/guide/why-doze.md)** · **[Measured footprint](docs/guide/resource-footprint.md)**

## See it work (60 seconds)

```sh
# 1. Install it (one binary — see Install below for mise/binary options)
brew install doze-dev/tap/doze

# 2. Describe what you need
doze init                      # writes a starter doze.hcl

# 3. Use it — the database boots on the first connection
doze shell app                 # opens a real psql shell (cold-boots `app` for you)
doze run -- <your command>     # ensures the backends are up, then runs your command
```

That's the whole loop. The first connection boots the engine and converges it to
your declared shape (database, roles, schemas, extensions); the next connection
is instant; stop touching it and, a few minutes later, it reaps back to zero.

Each instance listens on the explicit `port` you declared, so your connection
strings are stable — put `postgresql://app:app@127.0.0.1:5432/app` in your app
config, or declare the app as a [`process`](docs/recipes/workflows.md) block and
doze injects its dependencies' URLs for you.

```sh
$ doze status
  NAME      ENGINE        STATE    ENDPOINT         CONNS   MEM     CPU
Modules
  ● app     postgres 16   active   127.0.0.1:5432   1c      42.5M   -
  ○ cache   valkey 9      asleep   -                -       -       -

  1 awake · 42.5M resident · connect to any endpoint to wake it
```

## What you can run

The engines are chosen to be **cheap, real, license-clean local stand-ins** —
the API your code already speaks, without the heavy or encumbered originals.

| Service | What it's for | Why this one |
|---|---|---|
| **PostgreSQL** | your primary database — roles, schemas, extensions | the real, unmodified upstream (14–17) |
| **Valkey** | a Redis-compatible in-memory cache | the open-source Redis after the 2024 relicense |
| **Kvrocks** | Redis-compatible, RocksDB-backed durable KV | Redis API without keeping it all in RAM |
| **DocumentDB** | a MongoDB-wire document store | "Mongo" on Postgres, without MongoDB's license |
| **S3 / SQS / SNS** | object storage, queues, pub/sub | local AWS with no LocalStack, Docker, or JVM |

Mix as many as you want in one file. → **[The engines](docs/guide/engines.md)** ·
**[Recipes for each](docs/recipes/README.md)**

## How it works

Five ideas, and you've got the whole model:

1. **Declare, don't orchestrate.** You describe the *end state* in `doze.hcl` —
   which engines, versions, databases, roles. doze makes it so.
2. **Lazy boot.** A tiny daemon listens on one address per instance. The first
   connection cold-boots the real engine behind it; doze then splices your
   connection straight through, byte for byte.
3. **Idle reap.** Zero connections for a while and doze shuts the instance down.
   Walk away and your laptop goes quiet on its own.
4. **Real engines, pinned.** doze downloads the actual upstream binaries, verifies
   their checksums, and records exact versions in `doze.lock` — so your whole team
   and CI run byte-identical software.
5. **Structure, not data.** doze converges the *shape* of things (databases,
   roles, schemas, grants, buckets, queues, topics). Your app and migrations own
   the data.

→ **[The full mental model](docs/guide/concepts.md)**

## Is doze for you?

**Great for** local development, automated tests, CI pipelines, demos, and
spinning up a realistic backend for a new project in seconds. If you've ever run
`docker compose up` just to work on one service, doze is for you.

**Not for** production. doze runs **single** local instances (no replication, no
HA, no failover), tuned toward fast iteration over durability, and reaps them when
idle — so it's not a place to keep data you can't lose. The local AWS services
(S3/SQS/SNS) are dev-grade conveniences, not a stand-in for real AWS. Use managed
databases and real AWS in production. (Full rationale in the
[FAQ](docs/guide/faq.md#is-doze-production-ready).)

**Platforms:** macOS and Linux, on Apple Silicon and x86-64. (No native Windows;
WSL2 works.)

## Install

**No toolchain needed** — doze is one static binary, and it fetches Postgres,
Redis, etc. for you. macOS (Apple Silicon) or Linux (x86-64 / arm64).

```sh
# Homebrew (macOS / Linux)
brew install doze-dev/tap/doze

# mise
mise use -g ubi:doze-dev/doze

# Or grab a binary from the releases page
# https://github.com/doze-dev/doze/releases
```

Have Go 1.26+? These work too:

```sh
go install github.com/doze-dev/doze/cmd/doze@latest

# Or build from a clone
git clone https://github.com/doze-dev/doze && cd doze
go build -o doze ./cmd/doze
```

Engine binaries are fetched and cached automatically on first use. They're built
and published by the companion repo
**[doze-dev/doze-binaries](https://github.com/doze-dev/doze-binaries)**; see
[Managing binaries](docs/BINARIES.md) for the mirror format and self-hosting.

## Documentation

New here? Read these in order:

1. **[Why doze](docs/guide/why-doze.md)** — the case for it, and whether it's for you.
2. **[Getting started](docs/guide/getting-started.md)** — from zero to a running app, step by step.
3. **[Core concepts](docs/guide/concepts.md)** — the daemon, lazy boot, reaping, convergence, endpoints.
4. **[The engines](docs/guide/engines.md)** — what each engine is and when to reach for it.

Going deeper:

- **[Resource footprint](docs/guide/resource-footprint.md)** — measured numbers vs Docker & LocalStack.
- **[Files & storage](docs/guide/files-and-storage.md)** — where things live, what to commit, splitting config.
- **[Recipes](docs/recipes/README.md)** — copy-pasteable examples for every engine and workflow.

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
