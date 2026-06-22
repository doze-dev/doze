# Recipes — Valkey & Kvrocks

Both speak the Redis (RESP) protocol, so any redis client works. **Valkey** is
in-memory (a cache); **Kvrocks** is RocksDB-backed (disk, low RAM) — a durable
KV store that talks Redis.

## In-memory cache (Valkey)

```hcl
valkey "cache" {
  version   = 9            # newest 9.x; or "9.1.0"
  maxmemory = "256mb"
  password  = "cache"      # optional
}
```

```sh
doze run -- sh -c 'redis-cli -u "$REDIS_URL" ping'
eval "$(doze env)"; redis-cli -u "$REDIS_URL" set hello world
```

## Durable KV (Kvrocks)

```hcl
kvrocks "store" {
  version  = 2
  password = "store"       # optional
}
```

Kvrocks keeps data on disk in its data dir, so it survives reaps and restarts —
use it where you'd want Redis semantics without losing data when idle.

## Cache + durable store side by side

```hcl
valkey "cache" {
  version   = 9
  maxmemory = "128mb"
}
kvrocks "store" {
  version = 2
}
```

Both claim `REDIS_URL`, so when you declare more than one, use the per-instance
variables:

```sh
doze run -- sh -c '
  redis-cli -u "$DOZE_CACHE_URL" set k v
  redis-cli -u "$DOZE_STORE_URL" set k v
'
```

## Connecting a GUI / app

Each instance has its own doze endpoint; point your client at it (doze boots it
on connect):

```sh
doze status          # shows the ENDPOINT column, e.g. 127.0.0.1:6433
redis-cli -h 127.0.0.1 -p 6433
```
