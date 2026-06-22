# Using extensions

Extensions are where "just run real Postgres" gets interesting. A Postgres
*build* includes a fixed set of extension binaries; `CREATE EXTENSION` can only
load what's physically present in the install's `share/extension` and library
directories. doze declares extensions in config and creates them on
convergence — but something has to put the files there first.

There are four ways an extension becomes available, from easiest to most
involved.

## 1. Contrib extensions — already there

The standard "contrib" set ships with every Postgres build: `uuid-ossp`,
`pgcrypto`, `pg_stat_statements`, `pg_trgm`, `hstore`, `citext`, `btree_gin`,
`postgres_fdw`, and many more. Just declare them:

```hcl
postgres "app" {
  version    = 16
  extensions = ["uuid-ossp", "pg_stat_statements", "pg_trgm"]
}
```

`doze doctor` classifies each declared extension and flags any third-party one
that has no install source, so you find out before convergence, not during it.

## 2. Bundled into your binaries — best for the popular heavy hitters

The cleanest answer for extensions everyone on your team needs (pgvector,
PostGIS) is to **build them into the Postgres binaries you publish** (see
[BINARIES.md](BINARIES.md)). Your archive's `lib/` and `share/extension/`
already contain the extension, so `CREATE EXTENSION vector` just works with
nothing extra in the project config.

This couples extension availability to the binary version — which is exactly
what you want: one pinned artifact gives every developer the same engine *and*
the same extensions.

```hcl
postgres "app" {
  version    = 16
  extensions = ["pgvector"]   # available because the binary bundles it
}
```

## 3. Per-extension bundles — for the long tail

For extensions you don't want to bake into every binary, doze can install a
**prebuilt bundle** into the toolchain at convergence time, then
`CREATE EXTENSION`:

```hcl
postgres "app" {
  version = 16
  extension "pgvector" {
    version = "0.7.0"
    source  = "https://ext.mycorp.dev/pgvector/0.7.0/pg16-x86_64.tar.gz"
  }
}
```

`source` is a local path or http(s) URL to a `.tar.gz` whose entries are laid
out like a partial install prefix:

```
share/extension/vector.control
share/extension/vector--0.7.0.sql
lib/vector.so
```

doze routes `share/*` under the toolchain's `pg_config --sharedir` and `lib/*`
under `--pkglibdir`, then runs `CREATE EXTENSION`. Bundles are keyed, by
convention, to a (Postgres major × platform) pair — a `.so` is ABI-specific, so
publish one bundle per `pg<major>-<triple>`.

> **Toolchain must be writable.** Bundles install into doze-managed toolchains
> (the share/lib dirs live in the writable cache). If you override with
> `DOZE_POSTGRES_BINDIR` pointing at a read-only system Postgres, its share dir is
> usually root-owned and the install will fail with a clear warning — install
> the extension via your OS package manager instead (e.g.
> `apt install postgresql-16-pgvector`).

### Producing bundles

In the same separate build repo that produces your binaries, add a matrix job
that, per (extension × pg-major × target), builds the extension against that
Postgres' headers (`pg_config`), then tars up the resulting
`share/extension/*` and `lib/*.so`. Publish them next to your binaries with
their own checksums.

## 4. Trusted Language Extensions (TLE) — no filesystem access

Some extensions are pure SQL / PL/pgSQL and need no shared library at all. These
can be delivered as a `share/extension` bundle with just a `.control` and
`.sql` (no `lib/`), which is the lightest possible `source` bundle. This is also
the model [pg_tle](https://github.com/aws/pg_tle) generalizes: install the TLE
loader once (as a bundled or system extension), then register pure-SQL
extensions as data. Good for in-house helper extensions; not an option for C
extensions like pgvector.

## Recommendation

- **Contrib**: declare and go.
- **pgvector / PostGIS and other team-wide must-haves**: bundle them into your
  self-hosted binaries (option 2). One pinned artifact, zero per-project setup.
- **Occasional / project-specific extensions**: per-extension `source` bundles
  (option 3).
- **In-house SQL-only helpers**: ship them as lightweight TLE-style bundles
  (option 4).

In all cases, `doze doctor` will tell you up front whether a declared extension
is satisfiable, and convergence warns (rather than aborting the boot) when one
isn't — so a missing optional extension never blocks development.
