---
title: FerretDB
description: Run a MongoDB-compatible database that any Mongo driver, mongosh or Compass connects to.
redirect_from:
  - /guides/recipes/documentdb/
---

Run a document database your MongoDB drivers, `mongosh` and Compass connect to
unchanged, without running MongoDB.

MongoDB moved its server to the SSPL in 2018, a licence the OSI never approved.
This module speaks the MongoDB wire protocol instead: a FerretDB v2 gateway
(Apache-2.0) in front of a private PostgreSQL that carries Microsoft's
DocumentDB extension. You declare one block; doze runs the Postgres and the
gateway and shows you only the Mongo side. The block type is `ferret`, after
the gateway.

## A database

```hcl
module ferret "docs" {
  version = "2.7"
}
```

`version` is the FerretDB gateway's; the Postgres underneath comes with it.

```sh
doze url docs
# mongodb://docs.shop.doze:27017/
mongosh "$(doze url docs)" --eval 'db.runCommand({ ping: 1 })'
```

Hand it to an app by reference:

```hcl
app "api" {
  run  = "bun run dev"
  port = 3000
  env  = { MONGODB_URI = module.docs.url }
}
```

The first start does more work than later ones: doze creates the private
Postgres and its extension once. After that it wakes like any other service.
To get the first start out of the way, run `doze wake docs`.

## Databases, collections and seed data

```hcl
module ferret "shop" {
  version = "2.7"

  database "catalog" {
    collection "products" { seed = "./seed/products.json" }
    collection "orders" {}
  }
}
```

`seed` is a JSON array loaded into the collection when it is empty.

## Use it like Mongo

```sh
mongosh "$(doze url docs)" --eval '
  db.users.insertOne({ name: "Ada", roles: ["admin"] });
  printjson(db.users.find().toArray());
'
```

## What to expect

FerretDB aims at broad MongoDB compatibility, not all of it. If one command
matters to you, check the [FerretDB docs](https://docs.ferretdb.io/).
