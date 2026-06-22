# Recipes — FerretDB (MongoDB wire)

FerretDB speaks the MongoDB wire protocol but is stateless: it stores data in a
PostgreSQL backend built with the `documentdb` extension. doze models this as an
instance **dependency** — booting FerretDB boots and holds its Postgres backend,
and releases it when FerretDB stops.

## Mongo-compatible store

```hcl
# The storage backend: Postgres with FerretDB v2's documentdb extension.
postgres "events_pg" {
  version    = 16
  extensions = ["documentdb"]
}

# The Mongo-wire front end, backed by it.
ferretdb "events" {
  version = 2
  backend = "events_pg"      # a declared postgres instance (required)
}
```

```sh
doze run -- sh -c 'mongosh "$MONGODB_URI" --eval "db.runCommand({ping:1})"'
doze status     # `events` active holds `events_pg` active (CONNS shows the hold)
```

Notes:
- `MONGODB_URI` is injected for the `ferretdb` instance.
- Booting `events` first boots `events_pg`, injects its URL into FerretDB, and
  keeps it running for as long as `events` runs; stopping `events` releases it.
- The backend Postgres must have the `documentdb` extension available in its
  binary (it's one of the engines doze-binaries builds for FerretDB v2).
