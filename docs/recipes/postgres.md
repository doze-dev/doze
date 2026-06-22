# Recipes — PostgreSQL

doze runs real, unmodified PostgreSQL (14–17) and converges the declared
database, roles, schemas, grants, and extensions on first boot.

## Minimal app database

```hcl
postgres "app" {
  version = 16          # newest 16.x; or an exact "16.14"
  owner   = "app"       # role that owns the database
  role "app" { password = "app" }
  grant {
    role       = "app"
    database   = "app"
    privileges = ["ALL"]
  }
}
```

```sh
doze run -- npm test            # DATABASE_URL is injected
doze psql app                   # open a shell (boots app if cold)
```

## Users and roles

A "user" is just a role with `login` (the default). Group roles set
`login = false` and are granted to members via `member_of`.

```hcl
postgres "app" {
  version = 16
  owner   = "app"

  role "app" {                  # login user
    password         = "app"
    connection_limit = 20
  }
  role "readonly" {             # group role (no login)
    login = false
  }
  role "analyst" {              # inherits readonly's privileges
    password  = "analyst"
    member_of = ["readonly"]
  }
  role "admin" {
    password    = "admin"
    superuser   = true
    createdb    = true
    createrole  = true
    valid_until = "2030-01-01"
  }
}
```

Role attributes: `password`, `login`, `superuser`, `createdb`, `createrole`,
`replication`, `inherit`, `connection_limit`, `valid_until`, `member_of`.

## Schemas and ownership

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

```hcl
postgres "shop" {
  version = 16
  owner   = "shop"
  role "shop"     { password = "shop" }
  role "reporter" { password = "report" }

  grant {                       # full rights on the database
    role       = "shop"
    database   = "shop"
    privileges = ["ALL"]
  }
  grant {                       # read all current + future tables in public
    role       = "reporter"
    schema     = "public"
    objects    = "tables"       # applies default privileges to future tables too
    privileges = ["SELECT"]
  }
}
```

`grant` requires `role` + `privileges`; scope it with `database`, or
`schema` (+ optional `objects`: `tables`/`sequences`/`functions`).

## Extensions

```hcl
postgres "app" {
  version    = 16
  owner      = "app"
  role "app" { password = "app" }

  extensions = ["uuid-ossp", "pg_trgm"]    # simple: CREATE EXTENSION IF NOT EXISTS

  extension "vector" {                      # pin a version
    version = "0.7.0"
  }
  extension "hstore" {
    schema = "extensions"                   # install into a specific schema
  }
}
```

For an extension that isn't in the binary's `share/`, point `source` at a bundle
to build/install it — see [docs/EXTENSIONS.md](../EXTENSIONS.md).

## Multiple databases in one project

Declare several `postgres` blocks — each is its own instance with its own
endpoint and lifecycle. When there's more than one, use `DOZE_<NAME>_URL` (the
single `DATABASE_URL` is only set when exactly one Postgres is declared).

```hcl
postgres "app" {
  version = 16
  role "app" { password = "app" }
}
postgres "legacy" {
  version = 14
  role "app" { password = "app" }
}
```

```sh
doze run -- sh -c 'echo app=$DOZE_APP_URL legacy=$DOZE_LEGACY_URL'
doze status        # both shown; each boots independently on first connect
```

## Dev tuning profile

```hcl
postgres "app" {
  version         = 16
  shared_buffers  = "16MB"
  max_connections = 50
  fsync           = false      # fast, not crash-safe — perfect for dev/tests
  autovacuum      = false
  encoding        = "UTF8"
  locale          = "C"
}
```

## Pinning versions

```hcl
postgres "app"   { version = 16 }        # newest available 16.x, pinned in doze.lock
postgres "exact" { version = "16.14" }   # always exactly 16.14.0
```

```sh
doze versions postgres     # list what the mirror offers (installed/pinned marked)
```

Commit `doze.lock` so teammates get byte-identical binaries. See
[config-layout](config-layout.md#versions--the-lockfile).

## Throwaway database per test run

`doze ephemeral` clones the instance copy-on-write (instant on APFS/reflink),
runs your command against the clone, then destroys it.

```sh
doze ephemeral app -- pytest -n auto      # isolated real Postgres for the suite
doze ephemeral app                        # boot a disposable DB, print its URL, wait
```

See [workflows](workflows.md#ephemeral-databases) for parallel-test patterns.
