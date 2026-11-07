---
title: Start order and readiness
description: Say what each service waits for with after, and when it counts as ready with ready.
---

Make each service start after what it needs, and tell doze when a service is
ready to use.

## References order things for you

```hcl
app "orders" {
  run  = "go run ./services/orders"
  port = 8003
  env = {
    DATABASE_URL  = module.postgres.url
    KAFKA_BROKERS = module.kafka.address
    CATALOG_URL   = app.catalog.url
  }
}
```

Every block a body references is something it waits for. Before `orders`
starts, doze wakes `postgres`, `kafka` and `catalog` and waits until each is
ready. While `orders` runs, it holds them awake. This is Larder's `orders`;
Larder is the made-up shop the docs use as an example.

Most stacks need nothing more.

## `after`, for what a reference does not say

```hcl
app "catalog" {
  run   = "go run ./services/catalog"
  port  = 8002
  after = [task.migrate-catalog]
}
```

`after` lists blocks to wait for that the body does not reference, most
often a [task](/hcl/tasks/). Each entry is a whole block: `module.<name>`,
`app.<name>`, `service.<name>` or `task.<name>`.

A bare entry waits until the block is ready (for a task: until it
succeeded). Wrap it to wait for less:

| Entry | Waits until |
|---|---|
| `module.cloud` | it is ready |
| `started(module.cloud)` | it is running. Today doze still waits until it is ready; a later release will start sooner. |
| `lazy(module.cloud)` | nothing. Its address and env are filled in, it is not woken, and the first connection to it wakes it. |

```hcl
app "worker" {
  run   = "go run ./cmd/worker"
  after = [task.migrate, lazy(module.cloud)]
  env   = { AWS_ENDPOINT_URL = module.cloud.url }
}
```

When a block is both referenced and listed in `after` with `started()` or
`lazy()`, the `after` entry wins. `lazy` matters when one service wakes on
its own; `doze up` starts everything it was asked for anyway.

A cycle (`a` waits for `b`, which waits for `a`) is an error, and the message
shows the loop. `doze status --graph` draws what waits for what.

## `ready`: when it counts as ready

```hcl
app "catalog" {
  run   = "go run ./services/catalog"
  port  = 8002
  ready = http("/healthz")
}
```

| `ready =` | Ready when |
|---|---|
| `tcp()` | its port accepts a connection |
| `http("/healthz")` | a request to that path answers |
| `log("consuming")` | it prints a line containing that text |
| `exec("pg_isready -h 127.0.0.1")` | that command exits 0 |

The `http` path starts with `/`: the host and port are the block's own.
`tcp()` and `http()` need a `port`.

Without `ready`, an app or service with a port is ready when its port
accepts a connection (`tcp()`). One without a port is ready once its process
is still running three quarters of a second after it started.

Modules know when they are ready; they take no `ready`.

## How long to wait

Each app and service gets 60 seconds to become ready. Change it for one
block, or for all of them:

```hcl
defaults {
  ready_timeout = "3m"
}

app "catalog" {
  run           = "go run ./services/catalog"
  port          = 8002
  ready_timeout = "90s"
}
```

Larder sets three minutes in `defaults`: the first `go run` of each service
compiles it. When the time runs out, doze stops the process and marks it
failed, with `did not become ready within 3m0s` and its last log lines, and
what waits for it does not start. `doze up --timeout` caps how long `doze up`
waits for any one service.
