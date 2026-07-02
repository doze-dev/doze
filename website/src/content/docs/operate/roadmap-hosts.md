---
title: "Roadmap: registry hosts in sources"
description: The accepted design for multi-registry coexistence — Terraform-style host segments in module sources.
---

**Status: accepted, not yet implemented.** Documented here so operators can
plan against it; the full decision record lives in the doze repo
(`docs/design/registry-hosts.md`).

## The limitation today

A doze project has **one registry base** (`modules { mirror = … }` /
`DOZE_MODULES_MIRROR`) — all namespaces resolve from it. You can *replace* the
official registry (the self-host pattern) but not *mix* registries in one
project: `doze/postgres` from the official base and `acme/secretsauce` from
`registry.acme.dev` can't coexist yet, which also means third-party publishers
currently either PR into the official registry or take over the whole mirror
for their users.

## The accepted design

Sources grow an optional host segment, Terraform-style:

```hcl
modules {
  sauce { source = "registry.acme.dev/acme/secretsauce" }  # explicit host
  cache { source = "acme/valkey" }                          # default host, as today
}
```

- A host is recognized by containing a dot (namespaces never do) — every
  existing source string stays valid; this is a pure extension.
- Resolution, caching, and layout are unchanged per host:
  `<host>/<ns>/keys.json`, `<host>/<ns>/<name>/index.yaml`.
- **TOFU keys become host-qualified** in `doze.lock` (`<host>/<ns>`), with
  bare `<ns>` read as the default host — existing locks stay valid.
- The mirror override keeps meaning "the default host"; explicit hosts are
  never redirected — a project that names `registry.acme.dev` means it.
- Discovery (`doze modules search`) stays on the default host; explicit hosts
  are for use, not federation.

## What operators should do about it now

Nothing structural — that's the point of deciding early. Two gentle
guidelines so the future lands cleanly: don't build tooling that assumes a
source has exactly one `/`, and don't key any persistent state by bare
namespace where a host could later qualify it. When the feature ships, your
registry works with it on day one, because the file layout and trust model
don't change at all.
