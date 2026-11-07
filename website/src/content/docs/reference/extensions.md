---
title: "Postgres extensions"
description: Which Postgres extensions work out of the box, and how to install one that doesn't ship with Postgres.
---

This page shows how to get a Postgres extension into a doze database, whether it ships with Postgres or not.

A Postgres install can only `CREATE EXTENSION` what is on disk in its
`share/extension` and library folders. doze creates the extensions you declare,
but the files have to be there first. There are three ways they get there.

## 1. Contrib extensions: already there

The standard contrib set ships with every Postgres doze runs: `uuid-ossp`,
`pgcrypto`, `pg_stat_statements`, `pg_trgm`, `hstore`, `citext`, `btree_gin`,
`postgres_fdw` and more. Declare them by name:

```hcl
module postgres "db" {
  version    = 18
  extensions = ["uuid-ossp", "pg_stat_statements", "pg_trgm"]
}
```

## 2. A bundle from a `source`

For an extension that does not ship with Postgres, give doze a bundle to
install. doze copies it into the Postgres it manages, then runs
`CREATE EXTENSION`:

```hcl
module postgres "db" {
  version = 18
  extension "vector" {
    version = "0.8.0"
    source  = "https://example.com/pgvector/pg18-aarch64-apple-darwin.tar.gz"
  }
}
```

`source` is an http(s) URL or a path (relative to the folder of `doze.hcl`) to a `.tar.gz` laid out like part of a Postgres install:

```text
share/extension/vector.control
share/extension/vector--0.8.0.sql
lib/vector.so
```

doze puts `share/*` under the install's `pg_config --sharedir` and `lib/*`
under its `--pkglibdir`. A `.so` is built for one Postgres major and one
platform, so you need one bundle per major and platform; build it against that
Postgres' headers with `pg_config`.

## 3. SQL-only extensions

Some extensions are only SQL or PL/pgSQL and need no shared library. Their
bundle has just the `.control` and `.sql` files, with no `lib/`. This is the
lightest `source` bundle, good for in-house helpers. It is not an option for C
extensions such as pgvector.

## The `extension` block

| Field | Type | Default | What it does |
|---|---|---|---|
| `version` | string | the default in its `.control` file | The extension version to create. |
| `schema` | string | none | The schema to install it into. |
| `source` | string | none | A bundle to install from: a URL or a local path. |
| `optional` | bool | `false` | Warn and carry on if the extension is not available, instead of failing. |
| `cascade` | bool | `false` | `CREATE EXTENSION … CASCADE`, so its dependencies are created too. |

`extensions = [ … ]` is the short form: one name each, no other fields.

## When an extension is missing

An extension that is neither on disk nor installable from its `source` stops
the database from converging, and the error says so: declare a `source`, use a
Postgres build that includes it, or set `optional = true`. With
`optional = true`, doze logs a warning and starts the database without it.

The other Postgres fields are on [Postgres](/services/postgres/).
