---
title: Kvrocks
description: Run Kvrocks, the Redis protocol on disk, for keys that survive sleep and restarts without living in RAM.
---

Run a durable key-value store that any Redis client can use, with its data on
disk instead of in memory.

Apache Kvrocks speaks the Redis protocol (RESP) and stores its data in
RocksDB. It is an Apache Software Foundation project, under the Apache-2.0
licence. Pick it over [Valkey](/services/valkey/) when the data is large or
should survive a sleep and a restart: same client code, different trade-off.
Valkey is fast and in memory; Kvrocks is durable and light on RAM.

## A store

```hcl
module kvrocks "store" {
  version = 2
}
```

```sh
doze url store
# redis://store.shop.doze:6380/0
redis-cli -u "$(doze url store)" set user:42 '{"name":"Ada"}'
```

Kvrocks's own port is 6380, so it sits beside a Valkey on 6379 without a
clash. Hand it to an app by reference: `REDIS_URL = module.store.url`.

Writes are on disk: let it sleep, connect again later, and the keys are
there. `doze reset store` deletes them.

## Cache and store together

```hcl
module valkey "cache" {
  version   = 9
  maxmemory = "128mb"
}

module kvrocks "store" {
  version = 2
}
```

Each is its own server with its own address; an app reads `module.cache.url`
and `module.store.url`.

## Namespaces

A namespace is a separate keyspace with its own token. Declaring one needs a
`password`, which is the admin's:

```hcl
module kvrocks "store" {
  version  = 2
  password = "admin"

  namespace "tenant_a" { token = "tenant-a-token" }
}
```

A client that authenticates with `tenant-a-token` sees only that namespace.
