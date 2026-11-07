---
title: Postgres
description: Run a real PostgreSQL server, with its roles, schemas, grants and extensions declared in doze.hcl.
redirect_from:
  - /guides/recipes/postgres/
---

Declare a Postgres server in doze.hcl, with the roles, schemas, grants and
extensions your app needs, and connect to it like any other Postgres.

doze runs the upstream PostgreSQL server, not a fork, so every client, ORM and
extension behaves as it does in production. On its first start doze creates
the database and then converges what you declared: it creates what is new,
updates what changed and drops what you removed. Your migrations own the
tables and the data.

## A database for your app

```hcl
module postgres "db" {
  version = 18
}
```

That is a whole server. Its database is named after the block (`db`), and
`doze url db` prints the line a client needs:

```sh
doze url db
# postgres://postgres@db.shop.doze:5432/db?sslmode=disable
psql "$(doze url db)"
```

The address is the block's name in your stack (`shop` here), on Postgres's own
port. See [Names and addresses](/hcl/names/) for how that works and what you
get with names off. Connecting wakes the server; it sleeps again two minutes
after the last connection closes ([Sleep and wake](/hcl/sleep/)).

To hand the URL to an app, reference it instead of copying it:

```hcl
app "api" {
  run  = "go run ./cmd/api"
  port = 8080
  env  = { DATABASE_URL = module.db.url }
}
```

The reference also orders the start: `api` waits until `db` is ready
([Start order](/hcl/start-order/)).

## One server, a role per service

Larder, the made-up grocery shop the docs use as an example, runs one Postgres
for seven services. Each service connects as its own role and owns a schema of
the same name, so the default `search_path` finds its tables and it cannot
touch anyone else's:

```hcl
module postgres "postgres" {
  version = 18
  port    = 5432

  role "catalog" { password = "catalog" }
  role "orders" { password = "orders" }

  schema "catalog" { owner = "catalog" }
  schema "orders" { owner = "orders" }
}

app "catalog" {
  run  = "go run ./services/catalog"
  port = 8002
  env = {
    DATABASE_URL = "postgres://catalog:catalog@${module.postgres.address}/postgres?sslmode=disable"
  }
}
```

`module.postgres.address` is the host and port; the URL around it says which
role to log in as.

## Roles

A role logs in unless you say `login = false`; a role that does not log in is
a group to hang grants on. A complete set: an app user, a read-only group, an
analyst in that group, and an admin:

```hcl
module postgres "shop" {
  version = 18
  owner   = "shop"

  role "shop" {
    password         = "shop"
    connection_limit = 20
  }

  role "readonly" { login = false }

  role "analyst" {
    password  = "analyst"
    member_of = ["readonly"]
  }

  role "admin" {
    password   = "admin"
    superuser  = true
    createdb   = true
    createrole = true
  }
}
```

`owner` makes a role the owner of the server's database. Every role field is
in the reference below.

## Grants

Scope a grant to a database, or to a schema, and with `objects` to every
current and future table, sequence or function in it:

```hcl
module postgres "shop" {
  version = 18
  owner   = "shop"

  role "shop" { password = "shop" }
  role "readonly" { login = false }

  grant {
    role       = "shop"
    database   = "shop"
    privileges = ["ALL"]
  }

  grant {
    role       = "readonly"
    schema     = "public"
    objects    = "tables"
    privileges = ["SELECT"]
  }
}
```

## Extensions

```hcl
module postgres "db" {
  version    = 18
  extensions = ["uuid-ossp", "pg_trgm"]

  extension "vector" { version = "0.7.0" }
  extension "hstore" { schema = "extensions" }
}
```

`extensions` is the short form; an `extension` block pins a version or a
schema. Which extensions ship with each server, and how to build one that
does not, is in [Postgres extensions](/reference/extensions/).

## Two servers side by side

Each block is its own server, with its own data, address and sleep. Two
majors run side by side:

```hcl
module postgres "db" {
  version = 18
}

module postgres "legacy" {
  version = 16
  port    = 5433
}
```

Moving a server's data to a newer major is `doze migrate`:
[Postgres major upgrades](/guides/postgres-upgrades/).

## Migrations and seed data

doze converges structure: the database, roles, schemas, grants and
extensions. Run your migrations as a [task](/hcl/tasks/), so they run before
the app that needs them, and again only when the migration files change:

```hcl
task "migrate" {
  run    = "bun x prisma migrate deploy"
  inputs = ["prisma/migrations/**/*.sql"]
  env    = { DATABASE_URL = module.db.url }
}

app "api" {
  run   = "bun run dev"
  port  = 8080
  after = [task.migrate]
  env   = { DATABASE_URL = module.db.url }
}
```

For a one-off command against a running stack, `doze run` wakes what is
declared first; `--env` puts `DATABASE_URL` and the other connection
variables in the command's environment:

```sh
doze run --env -- bin/rails db:seed
```

## Start again from nothing

```sh
doze reset db
```

`doze reset` deletes the server's data and asks first. The next connection
creates it again from doze.hcl.

## In the app

The Postgres service page has a console tab: what is running against the
database now, and which databases there are. Its overview shows the address,
what is holding it awake and when it will sleep.

## Tips

- **Dev settings by default.** `fsync` is off and `shared_buffers` is small,
  which is fast and fine for data you can recreate. Turn `fsync = true` on if
  you keep anything you would miss.
- **A pool keeps it awake.** Sleep counts connections, so a pool holding idle
  connections keeps the server up. That is usually what you want while you
  work.
- **Exact versions.** `version = 18` pins the newest 18 build in `doze.lock`;
  `version = "18.6"` asks for that build. The table below lists them.
