# Recipes — PostgreSQL

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
postgres "app" {
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
doze run -- <your app>     # DATABASE_URL is injected; the DB boots on first use
doze shell app              # or open a SQL shell directly (boots `app` if cold)
```

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

The connection cold-boots the instance just like any other client. Or grab the
ready-made URL:

```sh
eval "$(doze env)"; echo "$DATABASE_URL"
# postgresql://app:app@127.0.0.1:6432/app
```

## Users, roles & permissions

A "user" is a role with `login` (the default). Group roles set `login = false`
and are granted to members via `member_of`. A complete pattern — an app user, a
read-only group, an analyst that inherits it, and an admin:

```hcl
postgres "shop" {
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
postgres "app" {
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
postgres "shop" {
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
postgres "app" {
  version    = 16
  owner      = "app"
  role "app" { password = "app" }

  extensions = ["uuid-ossp", "pg_trgm"]   # the simple case: CREATE EXTENSION IF NOT EXISTS

  extension "vector" { version = "0.7.0" }  # pin a version
  extension "hstore" { schema  = "extensions" }   # install into a specific schema
}
```

For an extension your binary doesn't ship, point `source` at a bundle to build it
— see [Extensions](../EXTENSIONS.md).

## Multiple databases & versions

Each `postgres` block is its own instance — own data, own endpoint, own lifecycle.
Run different majors side by side without conflict:

```hcl
postgres "app" {
  version = 17
  role "app" { password = "app" }
}
postgres "legacy" {
  version = 14
  role "app" { password = "app" }
}
```

With more than one Postgres, the friendly `DATABASE_URL` isn't set (it'd be
ambiguous) — use the per-instance variable:

```sh
doze run -- sh -c 'echo "new=$DOZE_APP_URL  old=$DOZE_LEGACY_URL"'
```

## Migrations & seeding

doze converges *structure* (database, roles, schemas, extensions); your tools own
the **schema and data**. Run them with the injected connection string:

```sh
doze run -- npx prisma migrate dev
doze run -- bin/rails db:migrate db:seed
doze run -- alembic upgrade head
doze run -- ./scripts/seed.sh        # reads DATABASE_URL
```

## Reset a database

Sometimes you want a clean slate:

```sh
doze stop app                                   # stop it
rm -rf "$(doze doctor | awk '/project/{print $3}')/clusters/app"
doze shell app                                   # next connect re-provisions + converges
```

Or use a **disposable** clone that vanishes on its own — ideal for tests:

```sh
doze ephemeral app -- pytest        # real Postgres, isolated, deleted afterward
```

## Tuning & tips

- **Dev tuning** — fast, not crash-safe (perfect for tests):
  ```hcl
  postgres "app" {
    version         = 16
    shared_buffers  = "16MB"
    max_connections = 50
    fsync           = false
    autovacuum      = false
  }
  ```
- **Idle reaping** is by connection count — a pool holding idle connections keeps
  the backend alive; close them (or `doze stop app`) to let it sleep.
- **Pin versions** for the team: `version = "16.14"` (exact) or `version = 16`
  (newest, pinned in `doze.lock`). Run `doze binaries available postgres` to see options.
- **TLS** for `sslmode=require` clients: see the
  [TLS reference](../reference/configuration.md#tls).
- **Ephemeral cold boots are instant** — doze runs `initdb` once into a template
  and clones it copy-on-write.
