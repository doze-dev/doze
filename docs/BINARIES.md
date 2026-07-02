# Managing binaries

doze runs real, unmodified engine binaries. It never compiles anything at
runtime — it resolves an `(engine, version)` to a directory of executables and
runs them. This document covers how that resolution works, how versions are
pinned for reproducibility, and how to host your own mirror.

> **Scope.** This is the **engine binaries** supply chain (Postgres 16.14.0,
> valkey-server, …), fetched by each engine's *module* from the checksum-only
> [doze-binaries](https://github.com/doze-dev/doze-binaries) mirror. The
> **modules themselves** (the plugins providing each engine) come from the
> *signed registry* — a separate chain with ed25519 signatures and TOFU key
> pinning, covered in [Core concepts → Engines are modules](guide/concepts.md#engines-are-modules)
> and the [`modules {}` reference](reference/configuration.md#modules). Both pin
> into the same `doze.lock`.

## Resolution order

For a given `(engine, version)`, doze tries, cheapest first:

1. **`DOZE_<ENGINE>_BINDIR`** — an explicit bin directory used for that engine
   (e.g. `DOZE_POSTGRES_BINDIR`, `DOZE_VALKEY_BINDIR`). The escape hatch for CI,
   tests, and local builds. Bypasses the lock.
2. **Content-addressed cache** — `<home>/<engine>/<full>-<triple>/bin`, e.g.
   `~/.doze/postgres/16.14.0-aarch64-apple-darwin/bin`. Keyed by the full version
   and target triple in the shared home, so patches never collide and every
   project on the machine reuses one download.
3. **Download** — from the [doze-binaries](https://github.com/doze-dev/doze-binaries)
   mirror (or your own), verified against a checksum and extracted into the cache.

doze deliberately has **no system fallback**. It only ever runs binaries it
manages — the override, the cache, or a verified download — so the engine is
identical on every machine. To use an existing local build, point
`DOZE_<ENGINE>_BINDIR` at it.

A version is either a **major** (`16` → the newest minor the mirror has) or an
**exact** full version (`"16.14"`). `doze binaries available [engine]` lists what the
mirror offers, marking which versions are installed and pinned.

## The lockfile

`doze.lock` lives next to `doze.hcl` and is meant to be committed. It is doze's
`go.sum`: it pins each `(engine, version)` to an exact resolved version and
records the checksum each platform's archive was verified against. It is YAML
(an older JSON lockfile still loads — JSON is a YAML subset — and is rewritten as
YAML on the next change).

```yaml
engines:
  postgres:
    "16":
      resolved: 16.14.0
      source: mirror
      hashes:
        x86_64-unknown-linux-gnu: "sha256:1f3a…"
        aarch64-apple-darwin: "sha256:9b22…"
  valkey:
    "9":
      resolved: 9.1.0
      source: mirror
      hashes: { … }
```

- The first run on a machine resolves the version, downloads it, verifies the
  checksum, and writes the pin (recording this platform's hash). Teammates on the
  same platform get a byte-identical binary; another platform adds its hash on
  first download.
- A locked hash that doesn't match a freshly downloaded archive is a **hard
  error** — tampering or a moved release is caught.

```
doze binaries list          # declared instances: pinned + cached toolchains
doze binaries which <name>  # resolve and print an instance's bin directory
doze binaries available [engine]      # versions the mirror offers (installed/pinned marked)
```

## Hosting your own mirror

The default mirror is the companion repo
[`doze-dev/doze-binaries`](https://github.com/doze-dev/doze-binaries), which
builds the engines in CI and publishes them (append-only). **Each engine has its
own rolling release** (tag = the engine name), so a slow or failing engine never
holds up the rest; doze resolves each engine from `…/releases/download/<engine>`.
Point doze elsewhere with a per-engine or global override:

```sh
export DOZE_MIRROR=https://bin.mycorp.dev          # all engines (engine name is
                                                   # appended: …/postgres, …/valkey)
export DOZE_POSTGRES_MIRROR=https://pg.mycorp.dev  # just one engine (used as-is)
```

A mirror is static files behind a base URL (`http(s)://` or `file://` for a local
path): a per-engine `index.yaml` plus the archives it names. `DOZE_MIRROR` is a
root that the engine name is joined to; `DOZE_<ENGINE>_MIRROR` is the exact base
for that one engine.

### The manifest

`<engine-base>/index.yaml` carries that engine's slice of the schema: a
`major → full` map and, per full version, a `triple → artifact` map with
checksums. (The shape is multi-engine so a single combined index also validates,
but each release ships only its own engine.)

```yaml
engines:
  postgres:
    versions:
      "16": 16.14.0
      "17": 17.10.0
    artifacts:
      16.14.0:
        x86_64-unknown-linux-gnu:
          url: postgresql-16.14.0-x86_64-unknown-linux-gnu.tar.gz
          sha256: "1f3a…"
        aarch64-apple-darwin:
          url: "…"
          sha256: "…"
```

`url` may be relative to the mirror base (as above) or absolute (an S3/CDN URL).
Each archive contains a `bin/` directory with the engine's executables, ideally
**relocatable** so doze can run it from the cache. Archive naming is
`<engine>-<full>-<triple>.tar.gz` (Postgres keeps the `postgresql-` prefix and a
three-part version).

### Building and publishing

Building, packaging, and publishing the binaries — and generating `index.yaml` —
lives in the [`doze-binaries`](https://github.com/doze-dev/doze-binaries) repo,
not in doze itself: it's a different cadence, toolchain, and review surface.
Fork it (or build your own) to control your supply chain. It builds only what
upstreams lack (Postgres, Kvrocks, Valkey/macOS), re-hosts the rest, and is
**append-only** so committed `doze.lock` files keep resolving forever.

Because doze verifies checksums from `index.yaml` and pins them in `doze.lock`,
the supply chain is end-to-end integrity-checked: the manifest names the hash,
doze enforces it, and the lockfile freezes it for the whole team.

### Air-gapped environments

The same mechanism works fully offline: mirror each engine's archives +
`index.yaml` onto an internal host (or a local directory via
`DOZE_MIRROR=file:///path`, with a `<engine>/` subdir per engine), commit
`doze.lock`, and the cache does the rest. No outbound internet is needed.
