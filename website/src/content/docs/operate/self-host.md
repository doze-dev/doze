---
title: "Host your own registry"
description: It's static files — a company registry in an afternoon, on any host you already have.
---

Companies run their own registries for the usual reasons: internal modules,
vetted-versions-only policies, air-gapped networks. Because a registry is
static files, "run" is generous — you're hosting a directory.

## The layout

```
registry/
  <namespace>/
    keys.json                 # your public key
    <name>/
      index.yaml              # signed module index
      meta.yaml               # generated docs
  index.json                  # generated catalog (search)
```

Serve it from anything that returns bytes over HTTPS: Cloudflare Pages, S3 +
CloudFront, GitHub Pages, nginx on an internal host. There is no dynamic
behavior at all.

## Standing one up

The official registry repo is a working reference — fork
[doze-registry](https://github.com/doze-dev/doze-registry) or copy its
`scripts/` (plain Node, no framework required):

```sh
npm run keygen acme                # once: your namespace keypair
                                   # commit registry/acme/keys.json
                                   # vault acme.secret.key

export DOZE_SIGNING_KEY="$(cat acme.secret.key)"
npm run publish acme/httpd -- \
  --release-base file:///path/to/module/dist/httpd \
  --artifact-base https://artifacts.acme.dev/httpd    # where archives really live

npm run validate                   # every check clients will make, offline
npm run validate:remote            # …plus: archives match the signed checksums
npm run prepare:data               # generates index.json + serves meta.yaml
# → upload registry output to your host
```

`--release-base` is where publish *reads* the built index (a local `dzm`/
`modtool` dist works via `file://`); `--artifact-base` is the URL prefix
*written* into it — so you can sign locally-built releases against their
eventual public home.

## Pointing doze at it

Per project:

```hcl
modules {
  mirror = "https://registry.acme.dev"
}
```

or environment-wide: `DOZE_MODULES_MIRROR=https://registry.acme.dev`. Clients
then resolve *all* namespaces from your base — the common company pattern is
mirroring the `doze/` namespace (copy the official signed files verbatim; the
signatures still verify, since they're the publisher's) alongside your own
`acme/` modules.

## Curated / vetted-versions registries

Because clients trust *signatures*, not hosts, a curation registry is just a
selective copy: include only the module releases your security team has
reviewed. Locked projects can't resolve anything you didn't publish, and
`doze modules upgrade` can only move pins to releases present in *your* index.
That's a real supply-chain control with zero custom tooling.

## Air-gapped, fully

Three things need to be inside the wall — all static:

1. **The registry** (this page).
2. **Engine binaries** — mirror the doze-binaries releases;
   [next page](/operate/mirror-binaries/).
3. **doze itself** — the release tarball on your internal file share.

A `file://` mirror works for the truly disconnected:
`DOZE_MODULES_MIRROR=file:///mnt/doze-registry`. Every doze verification —
signatures, checksums, key pinning — behaves identically against `file://`.
