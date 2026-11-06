---
title: "Teams & CI"
description: Two committed files, a cache directory, and one bot command — the whole team story.
---

doze's collaboration model is deliberately boring: **commit two files, and
everyone runs the same thing.**

## The team contract

- **Commit `doze.hcl`** (and any `*.doze.hcl` splits) — the declaration.
- **Commit `doze.lock`** — the pins: exact engine versions, exact module
  releases, publisher keys. A teammate's clone resolves to byte-identical,
  signature-verified software. doze reminds you the first time it creates one.
- **Don't commit** `.doze/` (runtime manifest) or any in-repo `data_dir` —
  see [Files & storage](/guides/files-and-storage/#whats-what-committing-and-ignoring).

Per-developer tweaks go in gitignored files beside `doze.hcl`: a different
port or a block switched off in `local.override.doze.hcl`, an extra instance of
your own in `local.doze.hcl`. Both are read automatically; neither changes what
anyone else runs. Add `*.override.doze.hcl` and `local.doze.hcl` to `.gitignore`.

A new teammate's onboarding is:

```sh
brew install doze-dev/tap/doze
git clone … && cd app
doze run -- make test        # fetches pinned modules + engines, converges, runs
```

No wiki page, no "install postgres 16 but NOT 17", no VM to provision.

## CI

doze works unmodified in CI — it's a static binary and needs no daemon
pre-started (`doze run` manages lifecycle around your command):

```yaml
# GitHub Actions sketch
- name: Install doze
  run: |
    curl -fsSL https://github.com/doze-dev/doze/releases/latest/download/doze_0.1.1_linux_amd64.tar.gz \
      | tar -xz && sudo mv doze /usr/local/bin/

# Cache the shared home: engine binaries + modules, keyed by the lockfile.
- uses: actions/cache@v4
  with:
    path: ~/.doze
    key: doze-${{ runner.os }}-${{ hashFiles('doze.lock') }}

- name: Test against real backends
  run: doze run -- go test ./...
```

Notes that save you a debugging afternoon:

- **Cache `~/.doze`** keyed on `doze.lock` — cold runs download a Postgres
  toolchain; warm runs touch no network at all (pinned + cached resolution is
  fully offline).
- **Linux runners are first-class** — x86-64 and arm64 both ship.
- **Everything is localhost** — no service containers, no port mapping; your
  tests connect to the ports declared in `doze.hcl`.

## Keeping modules fresh, as a team

Updates are explicit and reviewable:

```yaml
# a scheduled job that fails when upgrades are waiting
- run: doze modules upgrade --check
```

When it goes red, someone runs `doze modules upgrade`, commits the lock, and
the diff shows exactly what moved (release, supported engine versions,
checksums) — module updates go through code review like any dependency bump.

## Sharing structure, not data

doze converges *structure* (databases, roles, buckets, queues); your
migrations own schema and data. That's why the two-file contract is enough —
there's no snapshot to pass around, and `doze reset` gives anyone a pristine
backend in seconds. Seeding beyond structure belongs to your app
(`doze run -- make seed`).
