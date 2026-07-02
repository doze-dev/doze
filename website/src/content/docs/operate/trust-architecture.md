---
title: "The architecture of trust"
description: What each signature proves, what the CDN can and cannot lie about, and where the keys live.
---

A doze registry is **static files plus signatures** — no server, no database,
no login. This page is the operator's precise map of what protects what.
(Users get the narrative version in [the trust model](/why/trust/).)

## The three artifacts

```
<base>/<namespace>/keys.json            # { namespace, key } — raw 32-byte ed25519 pubkey, base64
<base>/<namespace>/<name>/index.yaml    # schema-1 module index, SIGNED
<base>/<namespace>/<name>/meta.yaml     # generated docs (prose; unsigned by design)
```

…plus `<base>/index.json`, the generated discovery catalog (`doze modules
search`), derived from the above at build time.

## Two signatures, two jobs

**Per-artifact** (`releases.<v>.artifacts.<triple>.sig`): ed25519 over the
archive's lowercase-hex SHA-256. This is the *execution gate* — the archive
host is untrusted by design; a tampered download fails no matter who serves it.

**Index-level** (`signature`): ed25519 over the SHA-256 of a canonical-JSON
payload of `{module, namespace, releases, channels}`. This is the *selection
gate* — it makes the compatibility metadata attestable. Without it, a
compromised host could serve a syntactically-valid index that lies about which
engine versions a release supports, points the `stable` channel at an old
vulnerable release (rollback), or withholds releases. With it, any such edit
breaks verification. Byte-compatible implementations sign in the registry's
JS tooling and verify in the doze client's Go — sorted keys, no insignificant
whitespace, empty optionals omitted.

## What each party can and cannot do

| Party | Can | Cannot |
|---|---|---|
| **Archive host** (GitHub releases, S3…) | Deny service | Alter what runs (checksums signed), claim compatibility |
| **Registry host / CDN** | Deny service, serve stale-but-valid | Forge indexes, rotate channels, alter engine-support, swap keys (TOFU pin) |
| **Namespace key holder** | Publish anything under that namespace | Retroactively change what locked projects run (locks pin checksums) |
| **doze client** | Refuse anything unverifiable | Be silently drifted (pins move only on explicit `upgrade`) |

The last row is the point of the whole design: even a *fully compromised*
registry can only affect projects at the moment they explicitly resolve or
upgrade — never a locked, cached project, which resolves offline.

## Key custody

The private key exists in exactly one place: the publishing environment (a CI
secret for the official registry). It never touches the archive host, the
module repos, or any build machine. Public keys are committed, served, and
**pinned trust-on-first-use** into every consumer's `doze.lock` — after first
contact, key substitution is a hard client-side error until a human clears the
pin. Consequences for rotation are real; see
[operations](/operate/operations/#key-rotation).

## The lifecycle in one diagram

```
module repo CI ── builds archives + unsigned index ──▶ archive host (untrusted)
                                                            │
registry publish (holds the key) ◀── fetches, verifies expected release
        │  signs artifacts + index, commits
        ▼
static registry ── validate (offline: every signature)
        │          validate --remote (archives match signed checksums)
        ▼
     deploy ──▶ CDN ──▶ doze clients (verify everything again, pin, lock)
```

Every arrow assumes the previous step might be hostile — which is why running
your own is safe and simple: [self-host](/operate/self-host/).
