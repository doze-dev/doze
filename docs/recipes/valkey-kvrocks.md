# Recipes — Valkey & Kvrocks

Both speak the Redis (RESP) protocol, so every Redis client and library works
unchanged — these are doze's **cheap, open-source ways to run "Redis" locally**.
(Valkey is the Linux-Foundation fork that kept Redis open source after its 2024
relicense; Kvrocks is an Apache project with the same API on disk. The full story
is in **[The engines](../guide/engines.md)**.) The difference is where the data
lives:

- **Valkey** — in-memory, like Redis. Fast, volatile. Use it as a **cache**.
- **Kvrocks** — RocksDB-backed (on disk). Redis API, durable storage, lower RAM.
  Use it as a **persistent KV store** that survives reaps and restarts.

## A cache (Valkey)

```hcl
valkey "cache" {
  version   = 9            # newest 9.x; or pin "9.1.0"
  maxmemory = "256mb"      # optional cap
  password  = "cache"      # optional
}
```

```sh
doze run -- sh -c 'redis-cli -u "$REDIS_URL" ping'      # -> PONG
eval "$(doze env)"
redis-cli -u "$REDIS_URL" set greeting hello
redis-cli -u "$REDIS_URL" get greeting
```

## A durable KV store (Kvrocks)

```hcl
kvrocks "store" {
  version  = 2
  password = "store"       # optional
}
```

Identical client experience, but writes persist to disk — stop touching it, let it
reap, reconnect later, and your keys are still there.

## Connecting clients & GUIs

Find the address and point any redis tool at it (RedisInsight, TablePlus, `redis-cli`):

```sh
doze status
#   NAME    ENGINE   STATE   …   ENDPOINT
#   cache   valkey   idle        127.0.0.1:6433
redis-cli -h 127.0.0.1 -p 6433
```

If you set a `password`, pass it with `-a` (or it's already in `REDIS_URL`):

```sh
redis-cli -h 127.0.0.1 -p 6433 -a cache
```

## Cache + durable store together

```hcl
valkey "cache" {
  version   = 9
  maxmemory = "128mb"
}
kvrocks "store" {
  version = 2
}
```

Both claim `REDIS_URL`, so with two of them use the per-instance variables:

```sh
doze run -- sh -c '
  redis-cli -u "$DOZE_CACHE_URL" set session:42 active
  redis-cli -u "$DOZE_STORE_URL" set user:42  "{...}"
'
```

## Common tasks

```sh
eval "$(doze env)"
redis-cli -u "$REDIS_URL" flushall            # wipe everything
redis-cli -u "$REDIS_URL" info keyspace       # how many keys
redis-cli -u "$REDIS_URL" monitor             # watch commands live
doze stop cache                                # put it to sleep now
```

## Tips

- **Valkey is a drop-in Redis fork** — your `ioredis`/`redis-py`/`go-redis` code
  needs no changes; just read `REDIS_URL`.
- **Pick by durability:** ephemeral cache → `valkey`; data you don't want to lose
  on a reap → `kvrocks`.
- **`maxmemory`** (Valkey) caps memory; pair it with an eviction policy from your
  client if you want LRU behavior.
- Reaping is by connection count — a client that keeps a connection open keeps the
  instance awake.
