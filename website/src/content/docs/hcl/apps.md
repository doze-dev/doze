---
title: Apps
description: Declare your own code in doze.hcl, an API, a worker or a web front end, so doze starts it, restarts it and gives it a name.
---

Declare each piece of your own code as an `app`, so doze starts it with what
it needs, restarts it when its files change, and lets it sleep when nothing
uses it.

An `app` is your code: an API, a worker, a web front end. A
[shared service](/hcl/shared-services/) like Postgres comes from the registry
as a `module`; a program you didn't write, like mailpit, is
[your own service](/hcl/your-own-services/).

## An app with a port

```hcl
app "api" {
  run   = "go run ./cmd/api"
  port  = 8080
  ready = http("/healthz")
}
```

- `run` is the command line, run with `sh -c` in the folder of the file that
  declares the block (or `dir`, relative to that file).
- `port` is the port everyone else uses. doze holds it and starts your
  command with `$PORT` set to a private port, so **listen on `$PORT`**, not on
  8080 (and on `$HOST`, which is 127.0.0.1). That is how doze can wake the app on its first request and let it
  sleep when no one is connected.
- `ready` says when it is ready to serve. Without it, doze waits until the
  port accepts a connection. See [Start order and readiness](/hcl/start-order/).

With [names](/hcl/names/) on, the app is also served at
`http://api.<stack>.doze`, with no port.

Most frameworks read `PORT` already. One that does not takes it on its
command line:

```hcl
app "storefront" {
  dir  = "apps/storefront"
  run  = "bun run vite --host $HOST --port $PORT --strictPort"
  port = 5173
}
```

## A worker

An app with no `port` is a worker: it consumes a queue, a topic or a
schedule. Nothing can connect to it, so nothing could wake it: a worker runs
until it is stopped and never sleeps on its own.

```hcl
app "notifications" {
  dir   = "services/notifications"
  run   = "bun src/index.ts"
  ready = log("consuming")
}
```

`log("consuming")` waits for that text in its output before the worker
counts as ready. This is Larder's `notifications`; Larder is the made-up shop
the docs use as an example.

## Wire it to what it uses

`env` sets environment variables, and a value can reference another block:

```hcl
app "catalog" {
  run   = "go run ./services/catalog"
  port  = 8002
  ready = http("/healthz")

  env = {
    DATABASE_URL     = module.postgres.url
    AWS_ENDPOINT_URL = module.aws.url
    IMAGES_BUCKET    = "product-images"
  }
}
```

A reference does two things: it fills in the value, and it makes `catalog`
wait until `postgres` and `aws` are ready, waking them if they are asleep.
More in [References and env](/hcl/references-and-env/).

## Restart on change

```hcl
app "catalog" {
  run   = "go run ./services/catalog"
  port  = 8002
  watch = ["services/catalog/**/*.go", "platform/**/*.go"]
}
```

`watch` restarts the app when a file matching one of the globs changes, while
it is awake. `**` matches any number of folders. The `inputs` of the
[tasks](/hcl/tasks/) it waits for are watched too: edit a migration and the
migration runs, then the app restarts.

## When it crashes

An app that exits with an error is started again, up to five times, waiting
one second and then twice as long each time (up to 30 seconds). After that it
is marked failed and waits for you. Change that with a `restart` block:

```hcl
app "api" {
  run  = "go run ./cmd/api"
  port = 8080

  restart {
    policy      = "always"
    max_retries = 10
  }
}
```

`policy` is `"on_failure"` (the default), `"always"` or `"no"`. A run that
stayed up for a minute resets the count. What the app shows when this happens
is in [When something breaks](/app/when-something-breaks/).

## Commands around the run

| Field | Runs |
|---|---|
| `setup` | once per data folder, before `run`: again only after a reset, or when you change it |
| `on_ready` | every time it wakes, once it is ready |
| `stop` | before it is signalled to stop; it then has ten seconds to exit |

Each takes one command or a list. Don't put migrations in `setup`: a
[task](/hcl/tasks/) runs them before the app, and again when they change.

## Apps that bind their own port

Some programs cannot listen on a port they are given. `direct = true` lets the
app bind `port` itself, on 127.0.0.1: doze passes no `$PORT` and holds
nothing, so nothing can wake it and it never sleeps on its own.

```hcl
app "legacy" {
  run    = "./bin/legacy-server"
  port   = 9000
  direct = true
}
```

## Turn one off without deleting it

`enabled = false` keeps the block in the file but never starts it. To take an
app out of today's run only, use the switch in the app or `doze off`
([Sleep and wake](/hcl/sleep/)).

Every field an app takes is in the [doze.hcl reference](/reference/doze-hcl/).
