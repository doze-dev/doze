# Recipes — FerretDB (MongoDB wire)

FerretDB is doze's way to run a **MongoDB-compatible** document store locally
without MongoDB itself or its SSPL license (see **[The
engines](../guide/engines.md)**). It speaks the MongoDB wire protocol, so MongoDB
drivers, `mongosh`, and GUIs like Compass all work — but it's stateless and stores
everything in a **PostgreSQL** backend (with the `documentdb` extension). doze
models this as an instance **dependency**: booting FerretDB boots and *holds* its
Postgres backend, and releases it when FerretDB stops. You declare two blocks;
doze wires the rest.

## A Mongo-compatible store

```hcl
# The storage backend: Postgres with FerretDB v2's documentdb extension.
postgres "docs_pg" {
  version    = 16
  extensions = ["documentdb"]
}

# The Mongo-wire front end, backed by it.
ferretdb "docs" {
  version = 2
  backend = "docs_pg"      # name of a declared postgres instance (required)
}
```

```sh
doze run -- sh -c 'mongosh "$MONGODB_URI" --eval "db.runCommand({ping:1})"'
```

`MONGODB_URI` is injected for the `docs` instance.

## Use it like Mongo

```sh
eval "$(doze env)"
mongosh "$MONGODB_URI" --eval '
  db.users.insertOne({ name: "Ada", roles: ["admin"] });
  printjson(db.users.find().toArray());
'
```

Point a driver at the same URI:

```js
// Node — the standard mongodb driver, unchanged
new MongoClient(process.env.MONGODB_URI)
```

```python
# Python — pymongo
pymongo.MongoClient(os.environ["MONGODB_URI"])
```

## Connecting a GUI

Find the endpoint and connect MongoDB Compass (or any Mongo client) to it:

```sh
doze status
#   NAME      ENGINE     STATE    …   ENDPOINT
#   docs      ferretdb   idle         127.0.0.1:6435
#   docs_pg   postgres   active       127.0.0.1:6434   (held while `docs` runs)
```

## What's happening underneath

- Booting `docs` boots `docs_pg` first, injects its connection info into FerretDB,
  and **holds it running** for as long as `docs` runs (so the reaper won't pull
  the backend out from under it). Stopping `docs` releases it.
- In `doze status` you'll see `docs_pg` active with a held connection whenever
  `docs` is up.
- Because it's really Postgres underneath, you can even inspect the stored
  documents with `doze shell docs_pg` if you're curious.

## Notes

- The backend **must** be a Postgres built with the `documentdb` extension —
  that's one of the engines doze-binaries builds for FerretDB v2.
- FerretDB targets broad MongoDB compatibility, not 100% — check the
  [FerretDB docs](https://docs.ferretdb.io/) if a specific command matters.
