---
title: "Mirror engine binaries"
description: The doze-binaries format, per-engine overrides, and the append-only rule.
---

Modules fetch their **engine binaries** (the actual Postgres, Valkey, …) from
a binaries mirror — a separate, simpler supply chain than the module registry:
checksum-verified rather than signature-verified, because the checksums
themselves are pinned in every project's `doze.lock`.

## The format

Per engine, a base URL serving:

```
<base>/<engine>/index.yaml                      # versions: major -> full;  artifacts: full -> triple -> {url, sha256}
<base>/<engine>/<engine>-<full>-<triple>.tar.gz # e.g. postgresql-16.14.0-aarch64-apple-darwin.tar.gz
```

Archives contain a relocatable tree with `bin/` (plus `lib/`, `share/` as the
engine needs); doze extracts into the content-addressed cache at
`~/.doze/<engine>/<full>-<triple>/`.

The official mirror is
[doze-binaries](https://github.com/doze-dev/doze-binaries): every engine built
from upstream source in public CI, bundled with its non-system libraries,
smoke-tested from a relocated path, and published to per-engine rolling GitHub
releases.

## Mirroring it

For most companies, mirroring is `curl` in a loop: fetch each engine's
`index.yaml`, fetch the archives your platforms need, host them under your own
base, done — the `index.yaml` needs no rewriting if you preserve relative
archive names. Point doze at it:

| Scope | How |
|---|---|
| Everything | `DOZE_MIRROR=https://mirror.acme.dev/doze-binaries` (engine name is appended) |
| One engine | `DOZE_POSTGRES_MIRROR=https://…/postgresql` |
| Skip mirrors entirely | `DOZE_POSTGRES_BINDIR=/opt/pg16/bin` (CI, local builds — bypasses lock) |

## The append-only rule (yours too)

The official mirror never rebuilds or deletes a published
`(engine, version, platform)` artifact — and if you host your own, **you
shouldn't either**. Every committed `doze.lock` in every repo pins exact
checksums; replacing published bytes strands all of them with verification
failures. Grow the mirror monotonically; disk is cheaper than a company-wide
"checksum mismatch" morning.

## Building your own engine archives

The doze-binaries repo's `recipes/<engine>/build.sh` scripts are the reference
for producing new archives (a pinned upstream ref, a lean feature profile,
`patchelf`/`install_name_tool` rpath rewriting so the tree relocates, and a
smoke test that actually boots the result). If you need an engine version or
platform the official mirror lacks, a recipe run + your mirror is the whole
path — and `doze binaries available <engine>` will show it to your users.
