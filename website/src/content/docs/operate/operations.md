---
title: "Operations"
description: The publish pipeline, provisional indexes, key rotation, and the failure modes worth rehearsing.
---

Day-two topics for whoever owns a registry — learned operating the official
one.

## The automated pipeline

The official flow, reusable shape for your own:

```
module repo: push → CI tests → builds (skipping published artifacts)
           → uploads archives + unsigned index to the archive host
           → repository_dispatch to the registry (module names + versions)

registry: publish workflow → fetches the release index
        → WAITS until it contains the expected version (CDN staleness is real)
        → signs artifacts + index → validate + validate:remote
        → bot commits → deploy
```

Hard-won details baked into that shape:

- **Dispatch carries expected versions.** A dispatch fired seconds after an
  upload can see the CDN's *previous* index; the signer retries until the
  release it was told about appears, instead of signing a stale index into a
  silent no-op.
- **Send structured payloads properly.** `gh api -f` stringifies JSON — the
  receiver must parse tolerantly or, better, send with `--input` and real
  types. (Yes, this one bit us.)
- **Retry archive uploads.** The release API throws transient 5xxs; a retry
  loop belongs in the workflow.
- **Chain the deploy explicitly.** Pushes made with a workflow's default token
  don't trigger other workflows — dispatch the deploy after the bot commit.

## Provisional indexes

`validate` (offline: structure + every signature) must **always** pass — it
gates deploys. `validate:remote` (archives match signed checksums) can
legitimately fail in one state: you signed a locally built release whose
archives aren't uploaded yet. That's a *provisional* index — fine to commit,
self-healing on the next real publish. A red offline validate is always a bug;
a red remote validate right after a local publish usually means "the upload
isn't live yet."

## Key rotation

The severe one. The registry key is compiled into **every doze binary** —
rotate it and every installed doze rejects the registry until it is upgraded
(by design: silent key swaps are the attack). So:

- **Routine rotation isn't a thing.** Rotate on suspected compromise, not on a
  calendar. Custody discipline beats rotation cadence here.
- **When you must:** ship a doze release carrying the new key, announce
  loudly, and re-sign the registry with it. Users upgrade doze. Locked+cached
  projects keep working offline throughout — only registry *contact* fails.
- **Compromise response order:** rotate key → audit what the old key signed
  (the registry's git history is the ledger — another reason indexes are
  committed files) → republish clean indexes → announce.

## Monitoring that matters

Cheap and sufficient for static files:

- CI: `validate` on every PR/push; `validate:remote` scheduled daily (catches
  archive-host bitrot).
- A canary: `doze modules upgrade --check` against a fixture project in a
  scheduled job — exercises the exact client path, signatures included.
- Availability is the CDN's problem; *integrity* failures are what your alerts
  are for, and clients enforce those anyway. The registry being down never
  breaks locked projects — they resolve from cache.
