---
title: "Recipes — Valkey & Kvrocks"
---


Both speak the Redis (RESP) protocol, so every Redis client and library works
unchanged — these are doze's **cheap, open-source ways to run "Redis" locally**.
(Valkey is the Linux-Foundation fork that kept Redis open source after its 2024
relicense; Kvrocks is an Apache project with the same API on disk. The full story
is in **[The engines](/guides/engines/)**.) The difference is where the data
lives:

- **Valkey** — in-memory, like Redis. Fast, volatile. Use it as a **cache**.
- **Kvrocks** — RocksDB-backed (on disk). Redis API, durable storage, lower RAM.
  Use it as a **persistent KV store** that survives reaps and restarts.

## A cache (Valkey)

```hcl
module valkey "cache" {
  version   = 9            # newest 9.x; or pin "9.1.0"
  maxmemory = "256mb"      # optional cap
  password  = "cache"      # optional
}
```

The URL is stable — `redis://127.0.0.1:6379` (add `:cache@` for the password), and
connecting cold-boots the instance:

```sh
redis-cli -u redis://127.0.0.1:6379 ping              # -> PONG
redis-cli -u redis://127.0.0.1:6379 set greeting hello
redis-cli -u redis://127.0.0.1:6379 get greeting
```

## A durable KV store (Kvrocks)

```hcl
module kvrocks "store" {
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
module valkey "cache" {
  version   = 9
  maxmemory = "128mb"
}
module kvrocks "store" {
  version = 2
}
```

Each has its own stable URL — they differ only by the explicit `port` you declared:

```sh
redis-cli -u redis://127.0.0.1:6379 set session:42 active   # cache
redis-cli -u redis://127.0.0.1:6380 set user:42  "{...}"    # store
```

An `app` block references each one by name — `module.<name>.url` — so two
instances of the same engine never collide.

## Common tasks

```sh
redis-cli -u redis://127.0.0.1:6379 flushall            # wipe everything
redis-cli -u redis://127.0.0.1:6379 info keyspace       # how many keys
redis-cli -u redis://127.0.0.1:6379 monitor             # watch commands live
doze sleep cache                                         # put it to sleep now
```

## Tips

- **Valkey is a drop-in Redis fork** — your `ioredis`/`redis-py`/`go-redis` code
  needs no changes; just point it at the stable `redis://127.0.0.1:<port>`.
- **Pick by durability:** ephemeral cache → `valkey`; data you don't want to lose
  on a reap → `kvrocks`.
- **`maxmemory`** (Valkey) caps memory; pair it with an eviction policy from your
  client if you want LRU behavior.
- Reaping is by connection count — a client that keeps a connection open keeps the
  instance awake.
