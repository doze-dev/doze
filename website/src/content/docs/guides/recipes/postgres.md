---
title: "Recipes — PostgreSQL"
---


Real, unmodified PostgreSQL (14–17). On first boot doze creates the database and
converges your declared roles, schemas, grants, and extensions — then gets out of
the way. Everything below is a config plus the commands to use it.

- [A database for your app](#a-database-for-your-app)
- [Connect a client or GUI](#connect-a-client-or-gui)
- [Users, roles & permissions](#users-roles--permissions)
- [Schemas](#schemas)
- [Grants](#grants)
- [Extensions](#extensions)
- [Multiple databases & versions](#multiple-databases--versions)
- [Migrations & seeding](#migrations--seeding)
- [Reset a database](#reset-a-database)
- [Tuning & tips](#tuning--tips)

## A database for your app

The 90% case — one database, one user that owns it:

```hcl
module postgres "app" {
  version = 16
  owner   = "app"
  role "app" { password = "app" }
  grant {
    role       = "app"
    database   = "app"
    privileges = ["ALL"]
  }
}
```

```sh
doze run -- <your app>     # ensures backends are up; the DB boots on first use
doze shell app              # or open a SQL shell directly (boots `app` if cold)
```

Point your app at the stable URL — `postgresql://app:app@127.0.0.1:5432/app`
(connecting cold-boots the instance), or declare your app as an `app` block and pass
the URL by reference: `DATABASE_URL = module.app.url`.

## Connect a client or GUI

doze gives each instance a stable address. Find it:

```sh
doze status
#   NAME   ENGINE     STATE    …   ENDPOINT
#   app    postgres   idle         127.0.0.1:6432
```

Point any tool at it — `psql`, TablePlus, DBeaver, pgAdmin, your ORM:

| Field | Value |
|---|---|
| Host | `127.0.0.1` |
| Port | from `doze status` (e.g. `6432`) |
| Database | `app` (the instance name) |
| User / Password | a role you declared, e.g. `app` / `app` |

The connection cold-boots the instance just like any other client. The URL is
stable and deterministic — it's just the role, the explicit `port`, and the
instance name:

```sh
# postgresql://app:app@127.0.0.1:6432/app
```

(or read it from `.doze/endpoints.yaml`, the manifest the daemon writes).

## Users, roles & permissions

A "user" is a role with `login` (the default). Group roles set `login = false`
and are granted to members via `member_of`. A complete pattern — an app user, a
read-only group, an analyst that inherits it, and an admin:

```hcl
module postgres "shop" {
  version = 16
  owner   = "shop"

  role "shop" {                 # the app's login user
    password         = "shop"
    connection_limit = 20
  }

  role "readonly" { login = false }   # a group role to hang SELECT grants on

  role "analyst" {              # a human who should only read
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

Role attributes: `password`, `login`, `superuser`, `createdb`, `createrole`,
`replication`, `inherit`, `connection_limit`, `valid_until`, `member_of`.

## Schemas

```hcl
module postgres "app" {
  version = 16
  owner   = "app"
  role "app" { password = "app" }

  schema "billing" { owner = "app" }
  schema "audit"   { owner = "app" }
}
```

## Grants

Scope a grant with `database`, or with `schema` (+ optional `objects` to cover
current *and future* objects):

```hcl
module postgres "shop" {
  version = 16
  owner   = "shop"
  role "shop"     { password = "shop" }
  role "readonly" { login = false }

  grant {                       # full rights on the database
    role       = "shop"
    database   = "shop"
    privileges = ["ALL"]
  }
  grant {                       # read every current + future table in public
    role       = "readonly"
    schema     = "public"
    objects    = "tables"
    privileges = ["SELECT"]
  }
}
```

`objects` accepts `tables`, `sequences`, or `functions`.

## Extensions

```hcl
module postgres "app" {
  version    = 16
  owner      = "app"
  role "app" { password = "app" }

  extensions = ["uuid-ossp", "pg_trgm"]   # the simple case: CREATE EXTENSION IF NOT EXISTS

  extension "vector" { version = "0.7.0" }  # pin a version
  extension "hstore" { schema  = "extensions" }   # install into a specific schema
}
```

For an extension your binary doesn't ship, point `source` at a bundle to build it
— see [Extensions](/reference/extensions/).

## Multiple databases & versions

Each `postgres` block is its own instance — own data, own endpoint, own lifecycle.
Run different majors side by side without conflict:

```hcl
module postgres "app" {
  version = 17
  role "app" { password = "app" }
}
module postgres "legacy" {
  version = 14
  role "app" { password = "app" }
}
```

Each has its own stable URL — they differ only by the explicit `port` you declared
(and the instance name):

```sh
# new=postgresql://app:app@127.0.0.1:5432/app
# old=postgresql://app:app@127.0.0.1:5433/legacy
```

An `app` block references each one by name — `module.<name>.url` — so two
instances of the same engine never collide.

## Migrations & seeding

doze converges *structure* (database, roles, schemas, extensions); your tools own
the **schema and data**. Run them with the backends guaranteed up (each tool reads
the stable URL from its own config/.env):

```sh
doze run -- npx prisma migrate dev
doze run -- bin/rails db:migrate db:seed
doze run -- alembic upgrade head
doze run -- ./scripts/seed.sh        # reads its configured DATABASE_URL
```

## Reset a database

Sometimes you want a clean slate:

```sh
doze reset app                                   # wipe its data
doze shell app                                   # next connect re-provisions + converges
```

For an isolated, freshly-converged database before a test run — isolation is now
per-database-within-an-instance:

```sh
doze reset app && doze run -- pytest        # real Postgres, clean slate, backends up
```

## Tuning & tips

- **Dev tuning** — fast, not crash-safe (perfect for tests):
  ```hcl
  module postgres "app" {
    version         = 16
    shared_buffers  = "16MB"
    max_connections = 50
    fsync           = false
    autovacuum      = false
  }
  ```
- **Idle reaping** is by connection count — a pool holding idle connections keeps
  the backend alive; close them (or `doze sleep app`) to let it sleep.
- **Pin versions** for the team: `version = "16.14"` (exact) or `version = 16`
  (newest, pinned in `doze.lock`). Run `doze binaries available postgres` to see options.
- **TLS** for `sslmode=require` clients: see the
  [TLS reference](/reference/configuration/#tls).
- **Cold boots are instant** — doze runs `initdb` once into a template
  and clones it copy-on-write.
