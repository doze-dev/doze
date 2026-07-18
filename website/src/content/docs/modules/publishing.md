---
title: "Publishing modules"
description: Namespaces, keys, signing — getting your module where users can trust it.
---

A built release becomes *usable* when a **signed registry** points at it. The
registry is the trust layer: your archives can live on any host (GitHub
releases, S3, a file server) because nothing is believed until it verifies
against your namespace key.

## 1. Your namespace is a keypair

```sh
# in a checkout of doze-registry (or your own registry repo)
npm run keygen acme
```

This writes `registry/acme/keys.json` (the public key — committed, served,
and **pinned by every user's doze on first use**) and `acme.secret.key` (the
private key — a vault or CI secret; it *is* your identity, and rotating it is
a breaking event for every user who pinned it).

## 2. Sign your release into a registry

```sh
export DOZE_SIGNING_KEY="$(cat acme.secret.key)"
npm run publish acme/httpd -- \
  --release-base https://github.com/acme/httpd-module/releases/download/module
```

`publish` fetches your release's `index.yaml`, rewrites artifact URLs to
absolute, signs **each archive's checksum** and then **the index itself**
(protocol, engine-support, and channel metadata are attestable — a compromised
CDN can't lie about compatibility), copies your generated `meta.yaml`, and
writes the signed index into the registry tree. `npm run validate` replays
exactly the checks user doze binaries enforce; `validate:remote` additionally
re-downloads every archive and confirms the signed checksums match reality.

## 3. Where does your namespace live?

Today, two options:

- **The official registry** (`doze.nerdmenot.in/registry`): a PR to
  [doze-registry](https://github.com/doze-dev/doze-registry) adding your
  `keys.json` and signed index. Your users then need only
  `modules { mytype { source = "acme/httpd" } }`.
- **Your own registry**: it's static files — host the same layout anywhere and
  users point at it with `modules { mirror = … }` or `DOZE_MODULES_MIRROR`.
  The full walkthrough is in the
  [operator guide](/operate/self-host/).

(A future source form carries the host in the address —
`registry.acme.dev/acme/httpd` — so third-party registries coexist with the
official one per-module. The design is
[accepted and documented](/operate/roadmap-hosts/).)

## 4. What users see

```sh
# your registry page now shows the module: tagline, engine versions, the
# config reference rendered from your Describe(), releases + signature status
```

…and in `doze.hcl`:

```hcl
modules {
  httpd { source = "acme/httpd" }
}

httpd "site" {
  port = 8080
  root = "./public"
}
```

First use pins your key and release in their `doze.lock`; from then on your
module updates reach them only through `doze modules upgrade` — reviewable,
explicit, reversible. Exactly the deal users get from the official modules,
because the machinery is identical.
