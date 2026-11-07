---
title: Temporal
description: Run a local Temporal server, with its Web UI and namespaces, for your workflow workers.
---

Run a Temporal server for your workflows and workers, with its Web UI and the
namespaces you declare.

This is Temporal's own dev server, from the Temporal CLI: one binary with the
Temporal services, a SQLite store and the Web UI. No JVM, no Docker, no
database to set up.

## A server

```hcl
module temporal "flows" {
  version = "1.1"
  port    = 7233
  ui_port = 8233

  namespace "orders" {}
}
```

Declare `port`: it gives the server its address, which `doze url` prints and
an app references.

```sh
doze url flows
# flows.shop.doze:7233
```

```hcl
app "worker" {
  run = "go run ./cmd/worker"
  env = { TEMPORAL_ADDRESS = module.flows.address }
}
```

The Web UI listens on `ui_port`; `headless = true` turns it off.

## It stays up

Temporal does not sleep. doze runs the server itself rather than waking it on
a connection, and workers long-poll it all the time anyway, so it runs while
the stack is up. A `sleep` duration on it is an error (`sleep = "never"` is allowed and changes nothing). See
[Sleep and wake](/hcl/sleep/).
