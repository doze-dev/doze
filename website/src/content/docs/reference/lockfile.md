---
title: "doze.lock"
description: What doze.lock pins, field by field, and what changes it.
---

This page describes every part of `doze.lock`, so you can read one in a diff and know what changed.

`doze.lock` lives beside `doze.hcl`. doze writes it; you commit it. With it,
every machine runs the same engine binaries, the same modules and the same
downloads. It is YAML (an older JSON lockfile still loads).

It has a top-level `doze:` line and three layers: `engines`, `modules` and
`downloads`.

```yaml
doze: 0.2.0
engines:
    postgres:
        "18":
            resolved: 18.6.0
            source: mirror
            hashes:
                aarch64-apple-darwin: sha256:f112ac93…
modules:
    doze/postgres:
        version: 0.2.4
        protocol: 1
        engines:
            - "14"
            - "15"
            - "16"
            - "17"
            - "18"
        hashes:
            aarch64-apple-darwin: sha256:a07134d0…
            aarch64-unknown-linux-gnu: sha256:f29c48e5…
            x86_64-unknown-linux-gnu: sha256:7c920599…
downloads:
    https://github.com/axllent/mailpit/releases/download/v1.21.8/mailpit-darwin-arm64.tar.gz:
        sha256: sha256:9ebb052f…
```

This is the lock of Larder, the made-up example stack, with the hashes
shortened. Its `doze:` line is what a released doze 0.2.0 would write.

## `doze`

The newest doze that wrote the file. A released doze writes its own version
when it is newer than the one there. An older doze that reads the file says so,
since a newer one may have pinned what the older one cannot run.

## `engines`

The engine binaries: the real Postgres, Valkey and so on. Keyed by engine type,
then by the version spec as `doze.hcl` declares it (`"18"` or `"18.1"`).

| Field | What it is |
|---|---|
| `resolved` | The full version the spec resolved to, e.g. `18.6.0`. |
| `source` | Where it came from: `mirror` for a verified download from [doze-binaries](/reference/binaries/). |
| `hashes` | The archive's checksum per platform. Each platform adds its own the first time it downloads. |

A bare major pins the newest minor at the moment it is first resolved. It
never moves by itself. Changing `version = 18` to `"18.1"` adds a new entry
under the new spec.

## `modules`

The modules: the signed plugins from the [registry](/contribute/registry/) that
run each engine. Keyed by source, such as `doze/postgres`; one pin per source.

| Field | What it is |
|---|---|
| `version` | The module's own release, e.g. `0.2.4`. Not an engine version. |
| `protocol` | The plugin protocol it speaks. |
| `engines` | The engine majors it supports. Empty means no limit, as for `aws`. |
| `hashes` | The checksum of every published platform, from the signed index, so teammates on other platforms check against the same pin. |
| `doze` | The oldest doze that runs this release, when the index says. |

`protocol`, `engines` and `doze` are copied from the signed index, so doze can
check a module against your config without the network. Only
`doze modules upgrade` moves a module pin.

## `downloads`

What a [`service`](/hcl/your-own-services/) with a `download` fetched. Keyed by
URL.

| Field | What it is |
|---|---|
| `sha256` | The checksum of what the URL served the first time. Every later fetch must match, or doze unpacks nothing and says so. |

## What changes it

| What you do | What happens to the lock |
|---|---|
| First run of an engine, module or download | Adds the pin |
| A new platform runs it | Adds that platform's hash to the engine pin |
| `doze modules upgrade [type]` | Moves module pins |
| `doze modules upgrade --check` | Nothing; exits 1 if a pin would move |
| A new `version` spec in `doze.hcl` | Adds an engine entry under the new spec |
| `doze up`, `doze run`, time passing, a new registry release | Nothing |
| `--dry-run` on any command | Nothing |

A pinned and cached resolve touches no network. A checksum that does not match
the pin is an error, never a new pin.

Deleting `doze.lock` is safe but loud: everything resolves fresh, majors may
land on newer minors and modules on newer releases. For modules,
`doze modules upgrade` does the same on purpose.
