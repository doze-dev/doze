---
title: "Recipes — Workflows"
---


How to drive doze day to day: getting connection strings into your code, running
tests and dev servers, operating the stack, and CI.

## `doze run` — the everyday command

Ensures the daemon is up, runs your command, and propagates its exit code.
Instances boot on first connect and reap after — so `run` guarantees the backends
are awake before your command touches them.

```sh
doze run -- npm test
doze run -- go test ./...
doze run -- python manage.py runserver
```

`run` injects **nothing** into the environment. Your command connects using the
connection strings you already configured (see below).

## Getting connection strings into your code

doze doesn't export env vars for you. There are three honest ways to get a
connection string into your app:

**1. Stable URLs from explicit ports (the simple default).** Every instance listens
on the explicit `port` you declared, so its URL is deterministic. Put it straight in
your app config or `.env`:

```sh
# .env
DATABASE_URL=postgresql://app:app@127.0.0.1:5432/app
REDIS_URL=redis://127.0.0.1:6379
AWS_ENDPOINT_URL_S3=http://127.0.0.1:9000
```

Connecting cold-boots the instance, so `psql postgresql://app:app@127.0.0.1:5432/app`
just works whether or not it was already awake.

**2. Declare the app as an `app` block and pass each URL by reference.**
When doze supervises your app, the app gets exactly the environment it
declares. A reference fills in the connection string and makes the app wait
for that module:

```hcl
app "api" {
  run = "go run ./cmd/api"
  env = {
    DATABASE_URL = module.app.url
    REDIS_URL    = module.cache.url
  }
}
```

```sh
doze up            # boots app + cache, then runs api with DATABASE_URL/REDIS_URL set
doze logs api -f   # follow it
```

**3. Read the manifest (for tooling).** The daemon writes `.doze/endpoints.yaml`
with every instance's address and connection string — machine-readable, for scripts
that would rather parse than hardcode.

## Test databases

Wrap your suite so the backends are up, then connect via the stable URL:

```sh
doze run -- pytest
doze run -- go test ./...
```

Want a clean slate between runs? `doze reset` wipes an instance's data and
re-converges its declared structure (roles, databases, schemas) on the next
connect — your schema back, no rows:

```sh
doze reset app     # wipe app's data
doze run -- pytest # fresh schema, re-provisioned on first connect
```

For parallel suites that need isolation, give each worker its own database within
one instance (e.g. a `test_${worker}` database your test harness creates and drops),
rather than a separate engine per worker.

## Operating the stack

```sh
doze up               # converge structure + boot every enabled service, then detach
doze up api worker    # just these (and their deps)
doze down             # sleep everything and stop the daemon

doze wake app         # boot one service now (and its deps)
doze wake             # warm every enabled service
doze sleep app        # reap one service (and its dependents); daemon keeps running
doze sleep            # reap all awake services

doze sync             # reconcile declared structure (create/update/prune)
doze sync --dry-run   # preview the changes; boots nothing
```

## Observability

```sh
doze status           # grouped table: state, endpoint, conns, MEM, CPU, deps
doze status --graph   # the dependency tree
doze ls               # alias for status
doze dash             # interactive TUI: select a row, then b boot / d reap / R restart / f follow
doze logs             # aggregate logs of every running service
doze logs app -f      # follow one service's logs
doze doctor           # diagnose config, platform, toolchains, daemon state
doze binaries available [engine]  # versions from the mirror (installed/pinned marked)
doze binaries list    # resolved/cached toolchains per instance
```

`doze status` works even when the daemon is stopped (it shows declared, on-disk
state). A backend that failed to boot shows state `error` with the reason; piped
output is plain (no color), so it's safe in scripts.

## CI

Simplest — wrap the test command so the backends are up:

```sh
doze run -- go test ./...
```

Or bring the stack up once and reuse it across steps (connections boot what they
touch):

```sh
doze up                  # converge + boot, then detach
./run-migrations && ./integration-tests
doze down                # sleep everything and stop the daemon
```

Tips for CI:
- Commit `doze.lock` so the binaries are byte-identical to local.
- Use `DOZE_<ENGINE>_BINDIR` to point at preinstalled binaries and skip downloads.
- `idle_timeout` can be short; the daemon reaps idle backends between steps.
