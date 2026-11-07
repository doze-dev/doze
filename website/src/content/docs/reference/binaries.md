---
title: "Binaries"
description: Where the engine binaries doze runs come from, how they are checked and pinned, and where they live on disk.
redirect_from: ["/operate/mirror-binaries/"]
---

This page explains where the engine binaries come from and where doze keeps them, so you know what runs on your machine.

doze runs real, unmodified engines: the actual `postgres`, `valkey-server`,
`mariadbd`. It never compiles anything at runtime. It turns an engine and a
version into a folder of executables and runs them.

There are two supply chains, and both pin into [`doze.lock`](/reference/lockfile/):

| What | From | Checked by |
|---|---|---|
| **Modules**, the plugins that run each engine | the [doze registry](/contribute/registry/) | an ed25519 signature, with the registry's key compiled into doze |
| **Engine binaries**, the engines themselves | [doze-binaries](https://github.com/doze-dev/doze-binaries) | a checksum from the engine's index, then pinned in `doze.lock` |

A module asks doze for its binaries, so a module never carries its own
downloader.

## doze-binaries

[doze-binaries](https://github.com/doze-dev/doze-binaries) builds each engine
from upstream source in public CI, bundles the libraries it needs, tests it
from a moved folder, and publishes it. Each engine has its own rolling GitHub
release (the tag is the engine's name), so one slow engine never holds up the
rest. Each release has an `index.yaml` that maps majors to full versions and
names every archive with its checksum.

Publishing is append-only: a published archive never changes or disappears, so
every committed `doze.lock` keeps resolving.

Platforms:

| Triple | Machine |
|---|---|
| `aarch64-apple-darwin` | Apple silicon Mac |
| `aarch64-unknown-linux-gnu` | 64-bit Arm Linux |
| `x86_64-unknown-linux-gnu` | 64-bit Intel and AMD Linux |

Intel Macs are not supported. The list of versions each engine ships is in
doze-binaries' `versions.yaml`.

## How doze finds a binary

For an engine and a version, doze tries, cheapest first:

1. **The cache.** `~/.doze/<engine>/<full>-<triple>/bin`, for example
   `~/.doze/postgres/18.6.0-aarch64-apple-darwin/bin`. Keyed by full version
   and platform in the shared home, so every project on the machine reuses one
   copy.
2. **A verified download** from doze-binaries, checked against the checksum in
   the index and, when there is one, the pin in `doze.lock`, then unpacked
   into the cache.

There is no system fallback. doze runs only binaries it manages, so the engine
is the same on every machine. A Postgres you installed with Homebrew or apt is
never used.

A version is a major (`version = 18`, the newest 18.x the index has, then
pinned) or exact (`version = "18.1"`). The engine versions each module supports
are on its page under [Services](/services/postgres/).

## Checks

- The first run on a machine resolves the version, downloads it, checks the
  checksum and writes the pin with this platform's hash. A teammate on another
  platform adds that platform's hash on their first run.
- A download whose checksum does not match the pin is an error. doze unpacks
  nothing and names both checksums.
- A pinned and cached engine starts with no network at all.

## On disk

Everything lives under the shared home, `~/.doze` (or `$DOZE_HOME`).

| Path | What it holds |
|---|---|
| `~/.doze/<engine>/<full>-<triple>/` | Engine binaries. |
| `~/.doze/modules/doze/<module>/` | Module releases. |
| `~/.doze/downloads/` | What `service` blocks with a `download` fetched. |
| `~/.doze/projects/<slug>/` | Each project's data, sockets and logs (`data_dir`). |

The caches are safe to delete when no stack is running: doze downloads again
on the next wake, checked against the same pins. A project's data is not a
cache; delete it with `doze reset <name>`.
