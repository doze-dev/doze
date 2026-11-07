---
title: Teams and CI
description: Share a stack with your team through two committed files, and run it in CI with stable output and exit codes.
redirect_from:
  - /guides/teams-ci/
---

Give every teammate and every CI run the same stack, from two files in your
repo.

## What to commit

- **`doze.hcl`** and any shared `*.doze.hcl`: what the stack is.
- **`doze.lock`**: the exact builds. It has four parts:
  - `engines`: each engine version you declared, the build it resolved to,
    and its checksum per platform;
  - `modules`: each module's pinned release and its checksums;
  - `downloads`: the checksum of each file a `service` downloads;
  - `doze:`, the newest doze that wrote it.

A clone resolves to the same builds, checked against the same checksums.
Pins never move on their own. Keep `.doze/` and your personal override files
out of git ([Splitting files](/hcl/splitting-files/#what-to-commit)).

A new teammate needs doze and the repo:

```sh
git clone … && cd shop
doze up
```

The first `doze up` fetches what `doze.lock` pins, starts the stack, and runs
the [tasks](/hcl/tasks/) that set it up. No wiki page of things to install.

doze converges structure, not data: databases, roles, buckets, queues and
topics come from doze.hcl, and your migrations and seed tasks fill them. So
there is no snapshot to pass around, and `doze reset` gives anyone a clean
service.

## Keeping modules up to date

Upgrades are explicit and go through review like any dependency:

```sh
doze modules upgrade --check   # exit 1 when an upgrade is waiting
doze modules upgrade           # move the pins, then commit doze.lock
```

A scheduled CI job running the first line goes red when there is something to
take. The lock's diff shows what moved.

## Running a stack in CI

doze is one binary and needs no service containers. On Linux, the release
tarball is `doze_<version>_linux_amd64.tar.gz` (or `_arm64`) on the
[releases page](https://github.com/doze-dev/doze/releases).

```yaml
# GitHub Actions
env:
  DOZE_VERSION: "0.2.0"
  DOZE_ZONE: "off"

steps:
  - uses: actions/checkout@v4

  - name: Install doze
    run: |
      curl -fsSL "https://github.com/doze-dev/doze/releases/download/v${DOZE_VERSION}/doze_${DOZE_VERSION}_linux_amd64.tar.gz" \
        | tar -xz doze
      sudo mv doze /usr/local/bin/

  - uses: actions/cache@v4
    with:
      path: |
        ~/.doze
        !~/.doze/projects
      key: doze-${{ runner.os }}-${{ hashFiles('doze.lock') }}

  - run: doze lint
  - run: doze up
  - run: go test ./...
  - run: doze down
```

- **`DOZE_ZONE=off`** turns [names](/hcl/names/) off for every doze program on
  the runner: services listen on `127.0.0.1:<port>`, and nothing asks for
  sudo. Give each service its own port.
- **Cache `~/.doze`** keyed on `doze.lock`, without `projects/` (the stack's
  data). What is cached is checked against the lock's checksums, so a stale
  cache is refused, not trusted. A warm cache needs no network.
- **No terminal.** A plain `doze up` with no terminal starts everything,
  without asking. A command that needs a yes, such as `doze reset`, fails
  with exit code 9 instead of guessing; pass `--yes` or set `DOZE_YES=1`.
- **`doze run -- <command>`** starts doze if it is not running and runs the
  command. It sets no environment unless you pass `--env`; it exits with the
  command's exit code.

## Checks for a pull request

```sh
doze lint            # the config is valid; changes nothing, runs nothing
doze fmt --check     # exit 1 if a file is not formatted
doze modules upgrade --check
```

`doze lint --offline` skips the modules' own checks, which may need the
network.

## Output for scripts

`--json` (or `DOZE_JSON=1`) prints one JSON envelope on stdout. `doze url nope --json` in a stack that has no `nope`:

```json
{
  "schema": "doze/v1",
  "command": "doze url",
  "ok": false,
  "error": {
    "code": "not_found",
    "message": "instance \"nope\" is not declared; declared: db (see doze status)",
    "exit": 4
  }
}
```

A command that worked has `"ok": true` and its result in `data`; `warnings` and `error` appear only when set. `-q` prints nothing:
the exit code is the answer. `NO_COLOR=1` or `--no-color` turns colour off;
it is off anyway when output is not a terminal.

## Exit codes

Codes are added, never renumbered.

| Exit | `error.code` | Means |
|---|---|---|
| 0 | | it worked |
| 1 | `failed` | it failed, for a reason with no code of its own |
| 2 | `usage` | the command line is wrong: an unknown command or flag, a missing argument |
| 4 | `not_found` | the stack declares nothing by that name |
| 5 | `config_invalid` | the config does not load, or a module it names won't fetch or start |
| 6 | `no_config` | no doze.hcl here or in any folder above |
| 7 | `daemon_down` | the command needs a running stack, and doze is not running |
| 8 | `timeout` | it took longer than allowed |
| 9 | `needs_confirmation` | it needs a yes and there is no terminal to ask: pass the flag the message names |
| 130 | `interrupted` | Ctrl-C or SIGTERM |

`doze run` exits with its command's code.
