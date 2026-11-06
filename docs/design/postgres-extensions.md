# Postgres extensions by name

Status: approved 2026-11-07 (owner: waves 1 and 2; TimescaleDB Apache-only).

## The ask

The postgres module is stock Postgres plus contrib. People want the popular
extensions (pgvector, TimescaleDB, PostGIS, pg_cron, …) by writing a name in
doze.hcl, with nothing to install and the same result on a Mac and on Linux.

## What exists today

- `doze-binaries` builds Postgres 14–18 from source for three platforms
  (x86_64 and arm64 Linux on Ubuntu 22.04, arm64 macOS), relocatable, and
  publishes them; doze pins each in `doze.lock`.
- Its DocumentDB recipe already compiles pg_cron, pgvector and PostGIS into a
  relocatable Postgres 18 on all three platforms. The hard part is proven.
- The postgres module already reads `extensions = [...]` and
  `extension "x" { version, schema, source, optional, cascade }`, runs
  `CREATE EXTENSION`, and can install a bundle from `source = <url>`
  (`modules/postgres/extensions.go`). What is missing: a catalog of bundles, so
  `source` is never needed, and preloading (`shared_preload_libraries`).

## The experience

```hcl
module postgres "db" {
  version    = 17
  extensions = ["vector", "pg_cron", "postgis"]
}

module postgres "metrics" {
  version = 17
  extension "timescaledb" { version = "2.31" }   # a pin, when it matters
}
```

- A contrib name works as today. Any other name is looked up in doze's
  extension catalog for this Postgres major and platform, fetched once into
  `~/.doze`, checked against its digest, and pinned in `doze.lock`
  (extension version and sha256 per instance), exactly as engines are.
- Extensions that must be preloaded (timescaledb, pg_cron, pgaudit,
  pg_partman_bgw, pg_search, pg_duckdb, pg_ivm) are added to
  `shared_preload_libraries` by the module, with the settings they need
  (`cron.database_name` = the instance's database). Changing that set restarts
  the instance once, and `doze up` says so.
- A name not in the catalog: "doze has no extension vectr for Postgres 17;
  did you mean vector? The catalog: vector, pg_cron, …; or give source = a
  bundle URL". Aliases: `pgvector` → `vector`, `timescale` → `timescaledb`.
- Upgrading: a new pinned version installs beside the old files and runs
  `ALTER EXTENSION x UPDATE` (PostGIS: `postgis_extensions_upgrade()`);
  `doze modules upgrade` moves extension pins like engine pins.
- `doze lint` checks names and majors offline against the cached catalog.
- The registry's postgres page lists every extension with what it is for,
  its licence, the majors it supports, and whether it preloads.

## The catalog (first waves)

Researched 2026-11-07 (stars, licences, PG support, cloud bundles: Neon,
Supabase, RDS, Postgres.app, Pigsty).

| Wave | Extension | For | Licence | PG | Build | Preload |
|---|---|---|---|---|---|---|
| 1 | pgvector | vectors | PostgreSQL | 14–18 | C/PGXS | no |
| 1 | pg_cron | scheduled jobs | PostgreSQL | 14–18 | C | yes |
| 1 | pg_partman | partitions | PostgreSQL | 14–18 | SQL + bgw | bgw only |
| 1 | pgmq | queues | PostgreSQL | 14–18 | SQL | no |
| 1 | hypopg | hypothetical indexes | PostgreSQL | 14–18 | C | no |
| 1 | pg_hint_plan | plan hints | BSD-3 | 14–18 (per-major lines) | C | optional |
| 1 | pg_repack (+ CLI) | online repack | BSD-3 | 14–18 | C | no |
| 1 | pgaudit | audit log | PostgreSQL | 14–18 (per-major lines) | C | yes |
| 1 | pg_ivm | incremental views | PostgreSQL | 14–18 | C | yes |
| 1 | pg_jsonschema | JSON Schema | Apache-2 | 14–18 | Rust/pgrx | no |
| 2 | PostGIS (+ h3, pgRouting) | geo | GPL-2 | 14–18 | heavy: GEOS, PROJ, GDAL vendored | no |
| 2 | TimescaleDB | time series | Apache-2 / TSL | 15–18 | CMake | yes |
| 2 | pg_search (ParadeDB) | BM25 search | AGPL-3 | 15–18 | pgrx, slow | yes |
| 2 | pg_duckdb | analytics, parquet | MIT | 14–18 | heavy C++ | yes |
| 2 | pgvectorscale | DiskANN | PostgreSQL | 18 (others unverified) | pgrx | no |
| 2 | Apache AGE | graph / Cypher | Apache-2 | 14–18 (per-major lines) | C | unverified |

Deferred: plv8 (V8; Neon and Supabase dropped it), Citus (AGPL, little use on
one node), pgroonga, orafce, pg_uuidv7 (PG18 has `uuidv7()`).

**TimescaleDB licence.** Hypertables and `time_bucket` are Apache-2;
compression, continuous aggregates and retention policies are the Timescale
License, which allows redistributing *unmodified* binaries free of charge
(§2.1(c)) but not offering a hosted database. Options: ship the Apache-only
build (what Neon does), or ship the full Community build with the TSL text and
a one-time notice, ideally after a written OK from licensing@tigerdata.com.

**AGPL/GPL** (pg_search, PostGIS): redistributing binaries is fine with the
source offered; each bundle's manifest links the exact upstream tag and the
recipe that built it.

## Prior art

- Postgres.app (macOS only): bundles PostGIS, pgvector; downloadable pg_cron,
  TimescaleDB, pg_search; on PG18 points `extension_control_path` and
  `dynamic_library_path` at a folder of extensions. Closest to this design.
- Pigsty: ~600 extensions as RPM/DEB for Linux, no macOS. Its per-extension
  metadata (licence, preload, PG matrix) is the reference to mirror.
- pgxman (dormant since 2024) and Tembo Trunk (shut down 2025): Linux only;
  pgxman's `pgxman install pgvector=0.5.1` pin syntax is worth echoing.
- Homebrew bottles are tied to Homebrew's paths; theseus-rs and zonky offer
  little or nothing for macOS. Nobody ships relocatable extensions for macOS
  arm64 and Linux together; that is the gap.

## Design

- **Bundles.** One per (extension, extension version, PG major, platform):
  `share/extension/*`, `lib/*.so|dylib`, vendored dependency libraries with
  rpath `$ORIGIN` / `@loader_path` (re-signed ad hoc on macOS), data files
  (PROJ, GDAL), a `bundle.json` (preload, settings, licence, source tag).
  Built against the oldest minor doze ships for that major (ABI holds within
  a major; rebuilt when a minor breaks it, as Nov 2024's did for TimescaleDB).
- **Install.** PG18: a per-toolchain extension folder joined through
  `extension_control_path` and `dynamic_library_path`, nothing written into
  the Postgres tree. PG14–17: copied into the toolchain's own share and lib
  (the toolchain is doze's, relocatable and shared across projects; installs
  are additive and keyed by version, so two projects never fight).
- **Preload.** The module writes `shared_preload_libraries` and the
  extension's settings into the instance's postgresql.conf from the declared
  set, ordered (timescaledb first), and restarts when the set changes.

## The contract between the build and the module

Decided 2026-11-07 so both halves can be built at once. An extension bundle
is an **engine** in the existing mirror index, so the module's existing
`engine.Fetcher` (`ResolveMajor`, `Ensure`), digest checks and `doze.lock`
engine pins work unchanged; no SDK or doze change is needed for fetching.

- **Engine key:** `pgext-<name>-pg<major>`, e.g. `pgext-vector-pg17`. Name is
  the extension's `CREATE EXTENSION` name (`vector`, not `pgvector`).
- **Versions:** the extension's own version, verbatim (`0.8.7`, `2.31.0`).
  `major_parts: 2`, so `ResolveMajor("pgext-vector-pg17", "0.8")` gives the
  newest 0.8.x. The module's catalog names the default line per extension.
- **Archive:** `pgext-<name>-pg<major>-<version>-<triple>.tar.gz`, same index,
  same release, same sha256 rules as the engines.
- **Layout inside the archive** (paths relative to its root):
  - `bin/` always present (holds `pg_repack` etc.; a `.keep` file otherwise,
    since `Ensure` treats a non-empty `bin/` as installed);
  - `share/extension/<name>.control` and its `--*.sql` scripts;
  - `lib/<name>.so` (Linux) or `lib/<name>.dylib` / `.so` (macOS, whatever
    PGXS produces), plus vendored dependency libraries beside it, rpath
    `$ORIGIN` / `@loader_path`, ad-hoc signed on macOS;
  - `share/<other>/` for data (PostGIS `share/proj`, `share/gdal`);
  - `bundle.json`: `{ "name", "version", "pg_major", "preload": ["timescaledb"],
    "settings": { "cron.database_name": "{db}" }, "env": { "PROJ_DATA":
    "{root}/share/proj" }, "requires": ["vector"], "license": "PostgreSQL",
    "source": "<upstream repo>@<tag>", "recipe": "<doze-binaries path>" }`.
- **Built against** the oldest Postgres minor doze ships for that major,
  inside doze-binaries' own Postgres tree (never system or Homebrew Postgres).
- **Verified** by CI: install into the matching Postgres tree, start it with the
  bundle's preload, `CREATE EXTENSION` (with `requires` first), one smoke query.

## Files

- `doze-binaries`: `versions.yaml` gains `extensions:` (name, versions per
  major, licence, preload); `recipes/pgext/lib.sh` (PGXS build against our
  Postgres tree, rpath, signing) and `recipes/pgext/<name>/build.sh` where a
  build is special (PostGIS deps, pgrx); `cmd/dzb` plans and manifests
  extension bundles; `build-engine.yml` matrix; `verify.yml` runs
  `CREATE EXTENSION` and a smoke query for every bundle on every platform.
- `doze-modules/modules/postgres`: `catalog.go` (resolve name, alias, major,
  platform; did-you-mean), `extensions.go` (fetch through the host, verify,
  install per major, upgrade), `preload.go` (preload list, settings, restart),
  `config.go` (pins), `describe.go` (catalog for the registry), tests and an
  acceptance test per wave-1 extension.
- `doze-sdk`: the host fetch API may need an artifact kind for extension
  bundles (checked first; the module already fetches engine binaries through
  the host).
- `doze`: `internal/binaries/lock.go` extension pins per instance; `doze lint`
  offline check; `doze modules upgrade` moves extension pins; doctor line.
- `doze-registry`: the postgres page's Extensions table from the catalog.
- Docs: `website/.../engines/postgres.md` extensions section, a guide per
  popular use (vectors, time series, geo).

## Size

Pipeline plus wave 1 (ten simple extensions, preload, pins, lint, registry
page, docs): 6–8 days. PostGIS with its vendored GEOS/PROJ/GDAL on three
platforms: 3–5 days. TimescaleDB: 1–2. The pgrx pipeline (pg_jsonschema in
wave 1, then pg_search, pgvectorscale): 1–2. pg_duckdb: 2. CI: about fifteen
builds per extension version (5 majors × 3 platforms); simple ones take
minutes, PostGIS and pgrx ones tens of minutes, built once and kept forever.
