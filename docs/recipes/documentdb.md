# Recipes — DocumentDB (MongoDB wire)

DocumentDB is doze's way to run a **MongoDB-compatible** document store locally
without MongoDB itself or its SSPL license (see **[The
engines](../guide/engines.md)**). It speaks the MongoDB wire protocol, so MongoDB
drivers, `mongosh`, and GUIs like Compass all work.

It's a single, **self-contained** engine — the block type is `ferret`, after the
gateway: doze quietly runs a private PostgreSQL carrying Microsoft's DocumentDB
extension behind a FerretDB v2 gateway, and exposes only the Mongo wire protocol.
The `version` you declare is the gateway's; the Postgres underneath is an
implementation detail.

## A Mongo-compatible store

```hcl
ferret "docs" {
  version = "2.7"
  port    = 27017
}
```

That's the whole declaration. The `docs` instance listens on its stable URI —
`mongodb://127.0.0.1:27017/` (connecting cold-boots it), or declare your app as a
`process` block so doze injects `MONGODB_URI` for you.

```sh
doze run -- mongosh mongodb://127.0.0.1:27017/ --eval "db.runCommand({ping:1})"
```

> **First boot is slow.** doze builds the cluster and runs `CREATE EXTENSION` the
> first time `docs` boots (a few minutes). After that it's a normal lazy engine —
> sub-second cold boots. Warm it ahead of time with `doze wake docs`.

## Use it like Mongo

```sh
mongosh mongodb://127.0.0.1:27017/ --eval '
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
#   NAME   ENGINE       STATE   …   ENDPOINT
#   docs   documentdb   idle        127.0.0.1:6441
```

Or open `mongosh` directly — `doze shell` picks the right client for the engine:

```sh
doze shell docs
```

## Notes

- DocumentDB targets broad MongoDB compatibility, not 100% — check the
  [FerretDB docs](https://docs.ferretdb.io/) if a specific command matters.
- It is **versionless**: the Postgres + extension + gateway are a curated bundle
  doze pins as a unit, so a `documentdb` block takes no `version`.
