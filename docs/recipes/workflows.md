# Recipes — Workflows

How to drive doze day to day: injecting connections, ephemeral test databases,
operating the daemon, and CI.

## `doze run` — the everyday command

Ensures the daemon is up, injects every instance's connection string, runs your
command, and propagates its exit code. Instances boot on first connect, reap
after.

```sh
doze run -- npm test
doze run -- go test ./...
doze run -- python manage.py runserver
doze run -- sh -c 'echo "$DATABASE_URL"'
```

## `doze env` — export into your shell

```sh
eval "$(doze env)"          # DATABASE_URL, REDIS_URL, AWS_ENDPOINT_URL_*, …
psql "$DATABASE_URL" -c '\dt'
```

`doze env` prints `export` lines; the current set is also written to
`.doze/endpoints.yaml` for other tooling.

## Ephemeral databases

`doze ephemeral <instance>` boots a throwaway **copy-on-write clone** (instant on
APFS/reflink), injects its connection string, runs the command, then reaps and
deletes it — an isolated, real database per run.

```sh
doze ephemeral app -- pytest                 # one disposable DB for the suite
doze ephemeral app -- go test ./...
doze ephemeral app                           # boot one, print its URL, wait (Ctrl-C destroys)
```

Per-test isolation in parallel suites — give each worker its own clone:

```sh
# pytest-xdist: one ephemeral DB per worker process
pytest -n 4 -p no:cacheprovider \
  --dist=loadgroup
# or wrap each invocation:
doze ephemeral app -- pytest tests/billing
doze ephemeral app -- pytest tests/orders   # fully isolated from the first
```

## Lifecycle

The daemon starts automatically; you drive **instances**:

```sh
doze start app        # boot one backend now (warms it up; daemon auto-starts)
doze start --all      # boot every declared instance
doze start -f         # run the daemon in the foreground (styled output, debugging)
doze stop app         # reap one backend (data persists; next connect re-boots)
doze stop --all       # stop everything — every backend and the daemon itself
doze restart app      # restart one backend (reap + re-boot)
```

## Observability

```sh
doze status           # table: engine, colored state, conns, RAM, uptime, endpoint, PID
doze ls               # alias for status
doze dash             # interactive TUI: select a row, then b boot / d reap / R restart / l logs
doze logs             # tail the daemon log
doze logs app -f      # follow a backend's logs
doze doctor           # diagnose config, platform, toolchains, daemon state
doze binaries available [engine] # versions available from the mirror (installed/pinned marked)
doze binaries list    # resolved/cached toolchains per instance
```

`doze status` works even when the daemon is stopped (it shows on-disk state). An
instance that failed to boot shows state `error` with the reason; piped output is
plain (no color), so it's safe in scripts.

## CI

Simplest — wrap the test command:

```sh
doze run -- go test ./...
```

Or bring up the env once and reuse it (connections boot what they touch):

```sh
eval "$(doze env)"
./run-migrations && ./integration-tests
doze stop --all
```

Tips for CI:
- Commit `doze.lock` so the binaries are byte-identical to local.
- Use `DOZE_<ENGINE>_BINDIR` to point at preinstalled binaries and skip downloads.
- `idle_timeout` can be short; the daemon reaps idle backends between steps.
