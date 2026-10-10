---
title: "Recipes — A microservices stack"
description: A complete, copy-pasteable multi-service dev environment — services, backing stores, ordering, health, and migrations, in one file.
---

This is the "clone the repo, run one command, everything's up" recipe. We'll
build a realistic small microservices stack — a few of your own services plus
the stores behind them — declared in one place, started in the right order, and
ready before you touch it. Copy it, rename things, and you're off. For the
*why* and the concepts, see [Running your own services](/guides/microservices/).

## The whole thing

Picture a typical shape: a **gateway/API**, an **auth service**, a background
**worker**, a **frontend** dev server, backed by **Postgres**, a **Valkey**
cache, and an **SQS** queue the worker drains.

```hcl
# doze.hcl — a full local microservices stack
defaults { idle_timeout = "10m" }

# ── backing stores ───────────────────────────────────────────────
module postgres "db" {
  version = 18
  port    = 5432
  owner   = "app"

  role "app" {
    password = "app"
    login    = true
  }
  extensions = ["uuid-ossp", "pg_trgm"]
}

module valkey "cache" {
  version = 9
  port    = 6379
}

module aws "cloud" {
  port = 4566
  queue "emails" {
    visibility    = 30
    dlq           = "auto"
    max_receives  = 5
  }
}

# ── your services ────────────────────────────────────────────────
app "auth" {
  dir     = "../auth"
  run = "go run ./cmd/auth"
  port    = 4000

  env = { DATABASE_URL = module.db.url }

  setup = "go run ./cmd/migrate up"   # auth owns its schema
  ready = http("/healthz")
}

app "api" {
  dir     = "../api"
  run = "go run ./cmd/api"
  port    = 8080

  env = {
    DATABASE_URL      = module.db.url        # → depends on db
    REDIS_URL         = module.cache.url       # → depends on cache
    AUTH_URL          = app.auth.url       # → depends on auth (waits for /healthz)
    AWS_ENDPOINT_URL  = module.cloud.url     # → depends on the local AWS
    EMAILS_QUEUE      = "emails"
  }

  setup = "go run ./cmd/migrate up"
  ready = http("/health/ready")
}

app "worker" {
  dir     = "../api"
  run = "go run ./cmd/worker"             # no port — a background consumer
  env = {
    DATABASE_URL         = module.db.url
    AWS_ENDPOINT_URL = module.cloud.url
    EMAILS_QUEUE     = "emails"
  }
  restart {
    policy = "on_failure"
    max_retries = 5
  }
}

app "web" {
  dir     = "../frontend"
  run = "npm run dev"                      # Vite/Next/whatever — hot reload intact
  port    = 3000
  env = {
    VITE_API_URL  = app.api.url
    VITE_AUTH_URL = app.auth.url
  }
  ready = http("/")
}
```

Then, from anywhere in the repo:

```sh
doze up
```

doze reads every `env` reference as a dependency edge and boots the graph in
order: **db · cache · jobs** come up first, then **auth** (its migration hook,
then the process, then waiting until `/healthz` actually answers), then **api**
(its own migrations, then health), then **worker** and **web** alongside. When
`doze up` returns, the stack isn't just launched — it's *ready*. Open
`localhost:3000` and go.

```
$ doze status
  NAME     ENGINE        STATE    ENDPOINT
  ● db     postgres 18   active   127.0.0.1:5432
  ● cache  valkey 9      active   127.0.0.1:6379
  ● jobs   sqs           active   127.0.0.1:9324
  ● auth   process       active   127.0.0.1:4000
  ● api    process       active   127.0.0.1:8080
  ● worker process       active   —
  ● web    process       active   127.0.0.1:3000
```

## Split it up so it's readable

One 90-line file is fine, but a stack like this reads better in pieces — doze
merges every sibling `*.doze.hcl` automatically ([config
layout](/guides/recipes/config-layout/)):

```
doze.hcl              # defaults + the backing stores
services.doze.hcl     # api, auth, worker, web
local.doze.hcl        # your personal tweaks — gitignored
```

Commit `doze.hcl`, `services.doze.hcl`, and `doze.lock`; a teammate clones and
runs `doze up` to the same stack, same versions, no setup doc.

## The variant most teams actually want: services local, data remote

Often you don't want to run the databases at all — there's a shared dev
Postgres, a staging Redis, real cloud queues. Keep the *services* local and
point them at the remote data. Same file, no backing-store blocks:

```hcl
# doze.hcl — only your services; data lives elsewhere
app "api" {
  dir     = "../api"
  run = "go run ./cmd/api"
  port    = 8080
  env = {
    DATABASE_URL = "postgres://app@db.dev.internal:5432/app"
    REDIS_URL    = "redis://cache.dev.internal:6379"
    AUTH_URL     = app.auth.url            # this one's still local
  }
  ready = http("/health/ready")
}

app "auth" {
  dir     = "../auth"
  run = "go run ./cmd/auth"
  port    = 4000
  env     = { DATABASE_URL = "postgres://app@db.dev.internal:5432/app" }
  ready = http("/healthz")
}
```

Mix and match by choosing what's a doze block versus a URL in `env` — run the
database locally but the queue remotely, or the reverse. doze doesn't care which
side of the network a dependency sits on; it just orders and health-gates what
it *does* run.

## Living in it

```sh
doze run -- ./scripts/e2e.sh   # bring the whole stack up, run a command against it, done
doze logs -f api               # follow one service
doze dash                      # watch every service + store live, in one screen
doze sync                      # you edited the config — reconcile without a full restart
doze down                      # everything sleeps; your machine goes quiet
```

A couple of touches worth knowing:

- **Migrations belong in `setup`** — they run after the database is up
  and before the service starts, every boot, idempotently.
- **A crashy worker?** `restart { policy = "on_failure" }` brings it back with
  capped backoff instead of you re-running a terminal.
- **Frontend hot-reload just works** — `web` is your real `npm run dev` process;
  edit a component and it reloads, no container in the loop.
- **Everything's debuggable** — attach your debugger to `api` and `worker` at
  once and step through a job that spans both; they're native processes on your
  machine.

That's a full microservices environment in one declarative file, booting in
seconds, sleeping when you walk away — no compose file, no VM, no eight terminal
tabs.
