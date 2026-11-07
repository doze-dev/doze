---
title: "How the registry works"
description: Where doze gets modules, what the signed index holds, where the key lives, and what a module must meet to get in.
redirect_from: ["/operate/trust-architecture/", "/operate/operations/", "/reference/module-index/"]
---

This page explains where doze gets its modules and what a module has to meet before the registry will carry it.

## One registry, one key

Every `module <engine> "…"` block in a `doze.hcl` resolves to the module
`doze/<engine>` in the doze registry, and nowhere else. doze 0.2 reads it from
`registry.doze-dev.com`. There is no setting that points doze at another
registry, another publisher or a local build. Anything the registry does not
carry runs as [your own service](/hcl/your-own-services/).

The registry is static files and signatures: no server, no database, no login.
For each module it serves two files:

```text
registry.doze-dev.com/doze/<name>/index.yaml   # the module index, signed
registry.doze-dev.com/doze/<name>/meta.yaml    # the module's docs, generated from its code
```

The plugin archives themselves live on GitHub Releases. The index points at
them and carries their signed checksums, so the archive host does not need to
be trusted. You can browse the modules at
[doze-dev.com/registry](https://doze-dev.com/registry/).

## The module index

`index.yaml` is schema 1. This is the shape, trimmed from the real
`doze/valkey` index:

```yaml
schema: 1
module: valkey                  # must equal the directory name
namespace: doze                 # always doze
releases:
  "0.2.1":                      # the MODULE version (not the engine version)
    protocol: 1                 # the doze plugin protocol this build speaks
    engines: ["8", "9"]         # engine majors it supports; absent = versionless
    artifacts:
      aarch64-apple-darwin:
        url: https://github.com/doze-dev/doze-modules/releases/download/valkey/valkey-plugin-0.2.1-aarch64-apple-darwin.tar.gz
        sha256: 4d4ccc27…       # lowercase hex
        sig: uY6lh8M7…          # ed25519 over the hex sha256, base64
      aarch64-unknown-linux-gnu: { … }
      x86_64-unknown-linux-gnu: { … }
channels:
  stable: "0.2.1"               # the release a fresh resolve prefers
signature: vGTQgqEU…            # ed25519 over the canonical payload, below
```

Two version numbers meet here. The engine version is what a user writes
(`version = 9`). The module version is the plugin's own release number, which
moves on every change to the module. `engines` connects them: it lists the
engine majors a release supports, and it is generated from the module's
`Describe().Versions` (see [Write it in doze-modules](/contribute/write/)).

A release may also carry `doze:`, the oldest doze that can run it. An older doze
skips such releases and, when none fits, says which doze to install.

## Two signatures, two jobs

**Each artifact** has a `sig`: ed25519 over the archive's lowercase-hex SHA-256.
This decides what doze will run. A tampered archive fails, whoever serves it.

**The index** has a `signature`: ed25519 over a canonical JSON form of
`module`, `namespace`, `releases` and `channels` (keys sorted at every level,
no extra whitespace, empty optional fields left out). This decides what doze
will pick. Without it, a hostile host could serve an index that lies about
which engine majors a release supports, points `stable` back at an old release,
or hides releases, while every archive still verified. The signer
(doze-registry's `scripts/lib.mjs`) and the verifier (doze-sdk's
`modindex.Verify`) produce the same bytes.

## What doze does with it

When a project declares a module, doze:

1. fetches `doze/<name>/index.yaml` and checks its signature against the key
   compiled into doze;
2. picks a release: one that speaks doze's plugin protocol and supports every
   engine major the project declares, preferring the `stable` head, otherwise
   the newest that fits;
3. downloads the archive for this machine and checks its checksum and `sig`;
4. caches it under `~/.doze/modules/doze/<name>/` and pins the version and
   checksum in `doze.lock` (see [doze.lock](/reference/lockfile/)).

Errors name the cause: "requires plugin protocol >= N … upgrade doze", or "no
release of postgres supports postgres 19; latest supports 14, 15, 16, 17, 18".

A pinned, cached project never asks the registry again. Pins move only when
you run `doze modules upgrade`; `doze modules upgrade --check` reports what
would move and exits 1 if anything would.

## Who can do what

| Party | Can | Cannot |
|---|---|---|
| Archive host (GitHub Releases) | Refuse to serve | Change what runs (checksums are signed) |
| Registry host (Cloudflare) | Refuse to serve, serve an old but valid index | Forge an index, move a channel, change engine support, swap the key |
| Holder of the registry key | Sign a new release | Change what a locked project runs (`doze.lock` pins checksums) |
| doze | Refuse anything that does not verify | Drift silently (pins move only on `doze modules upgrade`) |

Even a fully compromised registry can only reach a project at the moment its
owner resolves or upgrades on purpose.

## Where the key lives

The private key exists in one place: an Actions secret (`DOZE_SIGNING_KEY`) in
[doze-registry](https://github.com/doze-dev/doze-registry). Module repos never
see it, so doze-modules asks the registry to sign rather than signing itself.

The public key is compiled into doze (`RegistryKey` in
`internal/modules/modules.go`). doze never fetches it, so there is no first
contact to intercept. The cost: rotating the key takes a doze release, and
every older doze stops trusting the registry until it is upgraded. The key is
rotated on suspected compromise, not on a schedule. Locked and cached projects
keep working through a rotation, because they do not contact the registry.

## How a release gets in

```text
doze-modules  PR → test.yml → merge → release.yml
              (acceptance against the real backend, build three platforms,
               upload to the module's GitHub release, notify the registry)
                                   │
doze-registry publish.yml ◀────────┘
              waits for the release to appear, signs every new artifact
              and the index, validates every signature and re-downloads
              every archive, commits registry/doze/<name>/index.yaml,
              deploys
```

The registry's `publish` workflow also runs every hour and signs any release
it has not seen yet, so a missed notification only delays a release. The Kafka
and AWS modules are built in doze-kafka and doze-aws and enter the registry the
same way. [Submit and release](/contribute/submit/) walks the contributor's side.

## What a submission must meet

The registry signs only what passes these, and doze refuses anything else:

- **Schema 1.** An index without `schema: 1` is rejected.
- **Three platforms.** Artifacts for `aarch64-apple-darwin`,
  `aarch64-unknown-linux-gnu` and `x86_64-unknown-linux-gnu`.
- **Releases only grow.** A published release is never deleted, so a
  `doze.lock` written last year still resolves. The registry's `validate`
  workflow fails any change that drops or alters a published artifact.
- **Published bytes never change.** A `(version, platform)` pair is built once.
  Any change to a module means a new module version.
- **Generated, not hand-written.** `engines` and `meta.yaml` come from the
  module's `Describe()`, so the docs and the version gate cannot drift from the
  code.

`meta.yaml` is documentation only. It is not signed, and nothing doze enforces
reads it.

For the user's side of this, see [the trust model](/why/trust/).
