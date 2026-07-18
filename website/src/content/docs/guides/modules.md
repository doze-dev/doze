---
title: "Modules, for users"
description: How engines are provided, selected, locked, and upgraded — and the one version that's actually yours.
---

Every engine except `process` is a **module**: a signed plugin doze fetches
from the registry the first time your config names its type. This page is the
user's side of that machinery — [authors go here](/modules/overview/).

## The one version that's yours

The only version you ever write is the **engine version** — the thing itself:

```hcl
postgres "app" { version = 18 }   # the actual Postgres major
kafka    "bus" { version = 4  }   # the Kafka protocol level
```

Behind that sit two numbers you'll see in `doze.lock` and occasionally in an
error, and it's worth knowing what they are so you can ignore them with
confidence:

- **Module version** (`0.2.3`) — the plugin's own release. doze picks the
  newest one compatible with your doze and the engine versions you declared,
  pins it in `doze.lock`, and never moves it on its own. It says nothing about
  the engine — postgres module 0.2.3 runs Postgres 14–18.
- **Plugin protocol** (`1`) — the module↔doze wire contract. Purely internal;
  if a module and your doze can't speak, doze tells you before anything runs.

Neither of these tells you whether an engine "is usable" — that's the engine
version list and the platform list, both on each module's
[registry page](https://doze.nerdmenot.in/registry/), generated from the module
itself so they can't drift.

## The lifecycle

```sh
doze up                        # first use fetches, verifies, pins — done
doze modules upgrade --check   # anything newer that's compatible? (CI: exit 1)
doze modules upgrade           # move the pins; commit the updated doze.lock
```

That's the whole command surface. Discovery lives on the
[registry](https://doze.nerdmenot.in/registry/) — every module's page shows its
engine versions, platforms, full config reference, and release history.

Pins **never move on their own** — a moving registry can't drift a locked
project, and warm caches resolve fully offline.

## When doze asks you to upgrade

Two situations produce it, both with the exact command in the error:

1. **A new engine major.** You set `version = 19` but the pinned module
   supports 14–18:
   ```
   postgres 19 needs a newer doze/postgres module: pinned 0.2.1 supports
   14, 15, 16, 17, 18 — run 'doze modules upgrade postgres'
   ```
2. **A new config argument.** You used something added in a later module
   release; the decode error names the module and, when a compatible upgrade
   exists, says so.

Some arguments are **version-gated by the engine**, not the module — using a
Postgres-18-only setting with `version = 16` fails at `doze lint` naming the
argument and the required major. The docs mark these (*engine ≥ 18*).

## The `modules {}` block (rarely needed)

```hcl
modules {
  mirror = "file:///path/to/registry"   # air-gapped / dev registry

  cache {
    source  = "acme/valkey"             # a third-party publisher's module
    version = "0.2.0"                   # hold back an exact MODULE release
  }
}
```

Defaults are right for almost everyone: type `postgres` → source
`doze/postgres` → public registry. The `version` knob exists for bisecting a
module regression; the lock is the normal pin. Full field reference:
[configuration → modules](/reference/configuration/#modules).

## Development overrides

- `DOZE_<TYPE>_PLUGIN=/path/to/plugin` — run a local plugin binary, skipping
  the registry entirely (the module-author loop).
- `DOZE_MODULES_MIRROR=…` — point every fetch at another registry base.
- `DOZE_MODULES=off` — no fetching at all (offline, `process`-only).

## Trust, in one paragraph

The registry index that *selects* your module is ed25519-signed; every archive
checksum is signed; the publisher key pins on first use into `doze.lock`.
Tampered, unsigned, or key-rotated ⇒ hard error. The full story:
[the trust model](/why/trust/).
