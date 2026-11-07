---
title: "Propose a module"
description: Ask for a new engine in the doze registry, and agree on it before anyone writes code.
---

This page shows you how to propose a new module for the doze registry, and what the proposal needs to say.

A module is a long-lived promise. Someone has to follow new engine majors,
security fixes and SDK changes for as long as people use it. So a module starts
as an issue, and the code comes after the issue is agreed.

## Before you open an issue

- **Check it isn't there.** The modules that exist are listed at
  [doze-dev.com/registry](https://doze-dev.com/registry/).
- **Ask whether a `service` is enough.** doze runs any program as
  [your own service](/hcl/your-own-services/), with names, logs and sleep, and
  `doze add` can write the block for you. A module adds what a `service`
  can't: doze downloads the engine for the user, provisions its data, converges
  declared structure (roles, databases, buckets) and exposes typed fields in
  `doze.hcl`. If your engine needs none of that, a `service` is the better
  answer.
- **Find where the binaries come from.** A module drives a real engine binary,
  its *backend*. doze downloads backends from
  [doze-binaries](https://github.com/doze-dev/doze-binaries), and a module
  cannot release until its backend is published there for all three platforms:
  `aarch64-apple-darwin`, `aarch64-unknown-linux-gnu` and
  `x86_64-unknown-linux-gnu`.

## Open the issue

Open a new issue in doze-modules with the **Propose a module** template:
[github.com/doze-dev/doze-modules/issues/new?template=propose-a-module.yml](https://github.com/doze-dev/doze-modules/issues/new?template=propose-a-module.yml).
Its title is `Module: <engine>`. Every field is required:

| Field | What to write |
|---|---|
| Engine | The engine and its upstream project, for example "ClickHouse (https://github.com/ClickHouse/ClickHouse)". |
| Why doze should have it | Who would declare it in `doze.hcl`, and why running it as your own `service` is not enough. |
| Licence | The engine's licence. doze downloads and runs the engine on users' machines, so the licence must allow that. |
| Backend availability | Where the binaries come from: pure Go, a source build with native dependencies, or upstream release archives. Which versions. Whether all three platforms build. |
| Who will maintain it | The GitHub handles who will keep it current with new engine versions and SDK changes. |

## What makes a strong proposal

- A real project that would use it, and the `doze.hcl` you would write for it.
- A clear backend story. Pure Go engines are the easy case. Engines with
  native dependencies have to build on Linux and macOS, on both ARM and x86
  Linux.
- A licence that allows redistribution. Name source-available licences
  plainly; they go in the module's description so users see them.
- A named maintainer who is not only the person proposing it, where possible.

## What happens next

A maintainer replies on the issue. Once the module is agreed:

1. If the engine is new to doze, the first pull request goes to doze-binaries:
   a build recipe under `recipes/<engine>/` and the engine's versions in
   `versions.yaml`. That publishes before the module can pass its tests.
2. The module itself is a pull request to doze-modules. Link the issue from it.
   [Write it in doze-modules](/contribute/write/) covers the code,
   [Test it](/contribute/test/) the checks, and
   [Submit and release](/contribute/submit/) the pull request and what CI does
   after the merge.
