---
title: "Running your own services"
description: doze isn't just for databases — it's a local orchestrator for your microservices, with or without any database at all.
---

Here's a thing the rest of these docs undersell: doze runs **your** services too,
not just the databases behind them. And in a world where "the app" is rarely one
process — it's an API, a worker, an auth service, a frontend dev server, a
scheduler, and three more you inherited — that turns out to be the bigger story.

A modern local stack isn't "a database." It's a mesh of processes that need to
start in the right order, find each other, and be *ready* before you point a
browser at them. doze has a `process` block for exactly that, and you can build a
`doze.hcl` out of nothing but processes if you want — no database in sight.

## How people do this today (and it's not bad)

Credit where it's due — there are good tools here already:

- **A wall of terminal tabs.** Honest, universal, and it falls apart at four
  services. Which one crashed? Which log is which? Did you start them in the
  right order?
- **A `Procfile` with [foreman](https://github.com/ddollar/foreman) or
  [overmind](https://github.com/DarthSim/overmind).** Genuinely lovely for
  "run these N commands together" — overmind especially. What they don't do is
  know about *readiness*, *dependencies between services*, or the backing
  stores those services need.
- **docker-compose with build contexts.** Now every code change is an image
  rebuild (or a bind-mount dance), your debugger is across a VM boundary, and
  you're back to the [container tax](/why/not-containers/) — for code you wrote
  and are actively editing.
- **Tilt / Skaffold / dev-on-k8s.** The right answer if your inner loop is
  genuinely Kubernetes; a lot of machinery if it isn't.

doze sits where most application work actually lives: run your services as
**native processes** — so your debugger just attaches and your file-watcher just
works — but with the ordering, health-gating, and backing-service wiring the
Procfile approach leaves you to hand-roll.

## A real multi-service stack

Say you've got an API, a background worker, and an auth service, and the API
needs a database ready before it starts:

```hcl
# doze.hcl — a stack of your services

postgres "db" {
  version = 18
  port    = 5432
  role "app" { password = "app"; login = true }
}

process "auth" {
  cwd     = "../auth-service"
  command = "go run ./cmd/auth"
  port    = 4000

  env = { DATABASE_URL = postgres.db.url }

  health {
    http     = "http://localhost:4000/healthz"
    interval = "1s"
    retries  = 30
  }
}

process "api" {
  cwd     = "../api"
  command = "go run ./cmd/api"
  port    = 8080

  env = {
    DATABASE_URL = postgres.db.url      # wires the dependency to the db
    AUTH_URL     = process.auth.url     # …and to the auth service
  }

  # run migrations after the db is up, before the api starts
  hooks {
    pre_start = ["go run ./cmd/migrate up"]
  }

  health { http = "http://localhost:8080/health/ready"; retries = 30 }
}

process "worker" {
  cwd     = "../api"
  command = "go run ./cmd/worker"       # no port — a background worker has no endpoint
  env     = { DATABASE_URL = postgres.db.url }
  restart { policy = "on_failure"; max_retries = 5 }
}
```

Then:

```sh
doze up
```

doze reads the references — `postgres.db.url`, `process.auth.url` — as the
**dependency graph**, and boots in order: the database, then `auth` (waiting
until its `/healthz` actually answers), then the API's migration hook, then the
API, with the worker alongside. "Up" means *ready*, not "the processes were
launched and good luck." Change a file, your `go run` (or `bun --watch`, or
`vite`) reloads in place — it's your real process, on your machine.

## The pattern you're really here for: local services, remote data

You don't have to run the databases in doze at all. Plenty of teams keep data in
a shared or remote Postgres — a staging instance, a team database, a cloud
dev tier — and just want their **services** running locally. doze is happy to be
a pure process runner:

```hcl
# doze.hcl — only your services; the database lives elsewhere

process "api" {
  cwd     = "../api"
  command = "npm run dev"
  port    = 8080
  env = {
    # point straight at your remote/shared database — doze runs nothing for it
    DATABASE_URL = "postgres://app@db.staging.internal:5432/app"
    REDIS_URL    = "redis://cache.staging.internal:6379"
  }
  health { http = "http://localhost:8080/health"; retries = 30 }
}

process "worker" {
  cwd     = "../api"
  command = "npm run worker"
  env     = { DATABASE_URL = "postgres://app@db.staging.internal:5432/app" }
}
```

No `postgres` block, no database engine — just your two services, ordered,
health-gated, and supervised. This is a legitimate and common way to use doze:
**your code local (fast, debuggable), your data wherever it already lives.**

And you can mix freely: run the database locally but the search cluster remotely,
or vice versa, by choosing which things are doze blocks and which are just URLs
in `env`. doze doesn't care which side of the line a dependency lives on.

## Why native processes matter *more* for your own code

Everything on the [containers page](/why/not-containers/) applies double here,
because this is code you're editing every few minutes:

- **Your debugger attaches directly** — set a breakpoint in the API while the
  worker keeps running, step through a request that spans both.
- **Your watcher/hot-reload just works** — no image rebuild between edits, no
  sync delay; the process is right there.
- **Logs are real logs** — `doze logs -f api`, or watch every service at once in
  the [dashboard](/cli/dashboard/), which lists your processes right alongside
  the engines.
- **Crashes are debuggable** — a native stack trace, a core dump on your disk,
  not a container that exited and got reaped.

## The pieces, briefly

Each `process` block gives you the orchestration a Procfile doesn't:

- **`command` + `cwd`** — what to run and where (run via `sh -c`, so pipes and
  `&&` work).
- **`env` + `env_file`** — environment, with typed references to other instances
  (`postgres.db.url`, `process.auth.url`) that double as dependency edges.
- **`health`** — an `http`, `tcp`, `exec`, or `log_line` readiness probe, so
  dependents wait for *ready*, not just *launched*.
- **`hooks`** — `pre_start` (migrations, codegen), `post_start`, `pre_stop`
  (graceful drain).
- **`restart`** — `no` / `on_failure` / `always`, with capped exponential
  backoff, for the flaky worker that should just come back.
- **`depends_on`** — explicit ordering when there's no env reference to imply it.

The full field-by-field reference is in
[configuration → process](/reference/configuration/#process), and there's a
complete, copy-pasteable [microservices stack recipe](/guides/recipes/microservices-stack/)
— API, auth, worker, frontend, and their stores — ready to adapt.

## When doze *isn't* your process runner

Honest boundaries: doze supervises processes for **local development**, not
production — no clustering, no rolling deploys, no autoscaling. If you need your
services to match a Linux production userland byte-for-byte, run *those* in a
container or CI while doze handles the backing stores. And if your team's inner
loop is already a happy Kubernetes setup, doze isn't trying to replace it. But
for "I have eight services and I just want them running, ordered, and reachable
so I can build" — that's exactly the job.
