---
title: Valkey
description: Run Valkey, the open-source Redis, as an in-memory cache any Redis client can use.
redirect_from:
  - /guides/recipes/valkey-kvrocks/
---

Run a Redis-compatible cache for your app, reached by any Redis client
without a code change.

Valkey is the fork of Redis that stayed open source. In March 2024 Redis Inc.
moved Redis 7.4 and later to the RSALv2 and SSPLv1 licences, which are not
open source; the community forked the last open release, 7.2.4, as Valkey, a
BSD-3-Clause project under the Linux Foundation. It speaks the same protocol
(RESP), so `ioredis`, `redis-py`, `go-redis` and `redis-cli` work unchanged.

Valkey keeps its data in memory. For keys that should survive sleep and a
restart without living in RAM, use [Kvrocks](/services/kvrocks/): same
protocol, on disk.

## A cache

```hcl
module valkey "cache" {
  version   = 9
  maxmemory = "256mb"
}
```

```sh
doze url cache
# redis://cache.shop.doze:6379/0
redis-cli -u "$(doze url cache)" ping
```

The address is the block's name in your stack (`shop` here) on Redis's port
([Names and addresses](/hcl/names/)). Hand it to an app by reference:

```hcl
app "api" {
  run  = "go run ./cmd/api"
  port = 8080
  env  = { REDIS_URL = module.cache.url }
}
```

Larder, the made-up shop in these docs, calls its Valkey `redis` and passes
`module.redis.address` (host and port, no scheme) to the services whose
clients want that form.

## A password

```hcl
module valkey "cache" {
  version  = 9
  password = "cache"
}
```

The URL then carries it: `redis://:cache@cache.shop.doze:6379/0`.

## Evict, or keep

With `maxmemory` set, `maxmemory_policy` says what goes when it is full, such
as `"allkeys-lru"`. To keep keys across a sleep, turn on `appendonly = true`
or set `save`; or use Kvrocks, which is built for it.

## Everyday commands

```sh
redis-cli -u "$(doze url cache)" info keyspace   # how many keys
redis-cli -u "$(doze url cache)" monitor         # every command, live
redis-cli -u "$(doze url cache)" flushall        # empty it
doze sleep cache                                 # stop it now; it wakes on the next connection
```

A client that holds its connection open keeps Valkey awake; it sleeps two
minutes after the last one closes ([Sleep and wake](/hcl/sleep/)).
