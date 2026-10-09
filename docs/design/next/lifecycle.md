# Contract 4: how a service, an app and a task run

Tier: core for services and apps, should for tasks with `inputs` and for
`watch`, dream for snapshots. Built in phase 4. Background in
[gap-analysis.md](gap-analysis.md#services-tasks-watching-snapshots).

## One engine for three nouns

`service`, `app` and `task` are all "run a command". They are one built-in
engine, grown from `engine/process`, with three presets:

| | `service` | `app` | `task` |
|---|---|---|---|
| Starts | on first connection, or when a dependant starts | at `doze up` | when triggered |
| Sleeps | after the idle time | never, by default | not applicable |
| Proxied | always | when it has a `port` | no |
| Ends | when stopped | when stopped | on exit; 0 is `done`, anything else is `failed` |
| Restart on crash | yes, with backoff | yes, with backoff | no |

## The moments

```
download → setup → run → ready → on_ready → … → stop
  once      once    every wake                    every sleep
```

| Moment | Runs | Failure means |
|---|---|---|
| `download` | Once per URL. | `failed`. Nothing starts. |
| `setup` | Once per data folder: when `$DATA` is new, after `doze reset`, or when the list changes. | `failed`. `run` does not start. |
| `run` | Every wake. The one command that stays running. | `crashing`, then restart with backoff, then `failed`. |
| `ready` | Polled after `run` starts. Default: the private port accepts a connection. | Timeout is a failed start. |
| `on_ready` | Every wake, after ready. Connections that triggered the wake are held until it finishes. | The service keeps running but is not ready. Dependants do not start. Fixing and re-running `doze up <name>` reruns `on_ready` only. |
| `stop` | Every sleep, instead of the default signals. | Logged. Falls back to signals. |

Every step is a journal event with a duration, and its output goes to the log
store with a step tag ([daemon-api.md](daemon-api.md)).

### What maps to today's code

| New | Today | Change |
|---|---|---|
| `run` | `command` | Renamed. Gets `$PORT`, `$DATA`, `$HOST`. |
| `on_ready` | `hooks.post_start` | Same timing. Failure no longer stops the process. |
| `stop` | `hooks.pre_stop` | Becomes the stop command instead of a hook before signals. |
| `setup` | none | New. `hooks.pre_start` runs on every boot, including crash restarts. |
| `ready` | `health { … }` | Same probes. New default of `tcp()`. |

### Setup runs once

doze writes `<DATA>/.doze-setup` holding a hash of the `setup` list when every
step has succeeded. Setup runs when that file is missing or the hash differs.
`doze reset` removes the data folder, so setup runs again.

### Stopping

Today stop sends SIGINT, then SIGQUIT, then SIGKILL, and never SIGTERM. The
default becomes SIGTERM, then SIGKILL after a grace period, which is what most
software expects. Modules keep their own stop behaviour.

A command in the foreground is required. `docker run -d` returns at once and
looks like an exit. Lint warns when a `run` line contains ` -d` after
`docker run` or `podman run`.

## Download

`doze-sdk/binaries` already downloads, verifies a sha256, unpacks and caches
per platform (`EnsureArtifact`). The service path reuses it with these
changes:

| Gap | Change |
|---|---|
| tar.gz only | Add zip and a bare, single-file binary. |
| The archive must contain `bin/` | The archive root becomes the working directory for `run`; no layout is required. |
| The archive is read into memory | Stream to disk while hashing. |
| No hash is known up front | First fetch records the sha256 in `doze.lock` under a new `downloads:` map keyed by URL. Every later fetch must match. |
| A warm cache is not re-verified | Verify the cached archive hash against the lock at wake. |

`doze.lock` gains:

```yaml
downloads:
  "https://fastdl.mongodb.org/osx/mongodb-macos-arm64-8.0.4.tgz":
    sha256: …
```

With the map form of `download`, only the current platform's URL is fetched,
and each URL is pinned the first time someone on that platform runs it.

## Tasks

A task runs to completion and records the result.

| Trigger | Notes |
|---|---|
| Something that lists it in `after` is starting | The task runs first if it has never succeeded, or its inputs changed. |
| `inputs` changed | Checked when a trigger fires, and by the watcher if one is running. |
| `doze task <name>` | Always runs. |
| `manual = true` | Only the line above. |

- State is `done` or `failed`, plus the last exit code, duration, the reason
  it ran, and the hash of its inputs. This lives in `.doze/state.json`.
- A failed task stops whatever is `after` it.
- Each run is a log run and a pair of journal events, which is what the task
  screen's history reads.
- A task wakes what it references, holds it while it runs, and releases it
  when it exits.

Today a process that exits 0 is left stopped with the error "backend exited
unexpectedly". Tasks give that case a real state.

## Watch

Tier: should. `watch = ["**/*.go"]` on an app restarts it when a matching file
changes.

- Needs a file watcher. There is none in the repo; this adds one dependency.
- The watcher runs only while the app is awake, and is debounced.
- The same watcher re-checks task `inputs`.
- A restart caused by a file change is a journal event with
  `Cause{Kind: watch}` and the path.

## Snapshots

Tier: dream. The canvas shows named snapshots of a database and a snapshot
taken automatically before reset.

- Nothing exists beyond `engine.Templater`, which clones a template data
  directory at first provision.
- A generic version copies the data folder while the instance is asleep. It
  is correct for anything, and slow for large data.
- Engine-aware snapshots would be a new optional SDK interface.
- Not planned before phase 7. The reset confirmation ships without the
  automatic snapshot until then, and says the data is gone.

## Measures

| Measure | How |
|---|---|
| A two-line service wakes on connect | The fixture stack's `mailpit`: connect to its port while asleep and get an answer. |
| Setup runs exactly once | Wake, sleep, wake: one `step` event for setup in the journal. |
| A changed download is refused | Serve a different archive at the same URL in a test: the wake fails with the hash mismatch. |
| A task re-runs only on change | Touch a file outside `inputs`: no run. Change one inside: one run. |
