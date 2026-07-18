---
title: "The trust model"
description: Why a dev tool has supply-chain rigor — signatures, key pinning, and a lockfile with three layers.
---

`doze up` downloads and executes binaries. That sentence is why this page
exists: a tool that fetches executables owes you a story about why you can
trust what it runs — especially a *dev* tool, because dev machines hold source
code, credentials, and production access.

## The chain, end to end

Two supply chains meet on your machine, and every link is verified:

**Modules** (the plugins that provide each engine) come from a **signed
registry**:

1. Each publisher namespace (`doze/`, or a third party's) has an ed25519
   keypair. The public key is served at `<registry>/<ns>/keys.json` and
   **pinned on first use** into your `doze.lock`. After that, a changed key is
   a hard error — a compromised registry cannot silently rotate identity.
2. Each module's **index is signed as a whole** — not just its artifacts. The
   index carries the metadata that *selects* what you run: which releases
   exist, which engine versions each supports, which plugin protocol it
   speaks, where the channel points. Signing the index means a compromised CDN
   can't claim false compatibility or roll the channel back to a vulnerable
   release.
3. Each **archive's checksum is signed** individually. The archive host
   (GitHub releases) is untrusted *by design* — tampered bytes fail
   verification no matter who serves them.

**Engine binaries** (the actual Postgres, Valkey, …) are fetched by the module
from the [doze-binaries mirror](/reference/binaries/): built from upstream
source in public CI, published append-only (a released version is never
rebuilt), and checksum-pinned in your lock.

## The lockfile is the contract

`doze.lock` — committed next to your config — pins three layers:

```yaml
engines:            # engine binaries: version = 16 -> 16.14.0 + per-platform SHA-256
modules:            # per engine: module release, plugin protocol, supported
                    # engine versions, per-platform hashes
keys:               # each namespace's publisher key (trust-on-first-use)
```

The consequences are the guarantees:

- **Reproducibility.** A teammate's clone and your CI resolve to byte-identical
  software — verified, not hoped.
- **No drift.** A moving registry can't change what a locked project runs.
  Warm caches resolve **fully offline**.
- **Explicit change.** Pins move only via `doze modules upgrade` (and the
  updated lock goes through code review like anything else).
- **Immutability upstream.** Published `(version, platform)` artifacts are
  never rebuilt — the tooling hard-errors on changed bytes — so a lock
  committed today resolves forever.

## Two version axes, deliberately

You declare the **engine** version (`version = 18`) — the software your app
talks to. The **module** version (the plugin's own release) is selected
automatically: newest release compatible with your doze's plugin protocol and
every engine version you declared, then locked. You never write it; you meet
it only in `doze modules` output and in error messages that name their own fix
(`run 'doze modules upgrade postgres'`).

This split is what makes evolution safe: a module can ship a new config
argument or support a new Postgres major without touching what any locked
project runs, and your project adopts it in one reviewed commit.

## What this does *not* claim

Honesty section: signatures prove *who published*, not that the code is
benign — a malicious publisher signs malware happily. The official `doze/`
namespace is built in public CI from public source; for third-party modules,
the trust decision is yours, and the tooling's job is to make identity
unforgeable and changes visible (each module's registry page shows its
provenance, signature status, and exactly what config it accepts —
generated from the module itself). Verification also
starts at first fetch — the initial key pin is trust-on-first-use, the same
model as SSH.

For operating your own end of this — hosting a registry, mirroring binaries,
air-gapping — see the [operator guide](/operate/trust-architecture/).
