# Getting started

This is a hands-on tour. In about ten minutes you'll go from nothing to a real
local backend — a Postgres database and a cache — wired into an app, with the
engines booting on demand and sleeping when idle. We'll explain what you're
seeing as we go.

> **Prerequisites:** Go 1.26+, on macOS or Linux (Apple Silicon or x86-64). That's
> it — you don't install Postgres, Redis, or anything else. Hit a snag? See
> [Troubleshooting](troubleshooting.md).

## 1. Install

doze is a single binary:

```sh
go install github.com/doze-dev/doze/cmd/doze@latest
doze version
```

You do **not** install Postgres, Redis, or anything else — doze fetches the real
engine binaries for you on first use and caches them under `~/.doze`.

## 2. Describe what you need

doze reads a `doze.hcl` file. Scaffold one:

```sh
mkdir myapp && cd myapp
doze init
```

Open `doze.hcl` and trim it down to one database to start:

```hcl
postgres "app" {
  version = 16
  role "app" { password = "app" }
  grant {
    role       = "app"
    database   = "app"
    privileges = ["ALL"]
  }
}
```

You've just *declared* an instance named `app`: Postgres 16, with a login role
`app` that owns a database `app`. You haven't started anything yet — this is the
desired end state, not a command.

Sanity-check it any time:

```sh
doze doctor
#   ✓  config        doze.hcl parses cleanly
#   ✓  postgres/app  16 (not pinned; resolves on first use)
#   ✓  daemon        stopped (starts on first connect, or `doze up`)
```

## 3. Connect — and watch it boot itself

Open a SQL shell:

```sh
doze shell app
```

The first time, this does a lot for you, transparently:

1. fetches the **postgres module** — the signed plugin that provides the engine —
   from the registry, verifies its signature, and pins it in `doze.lock`
   (doze prints a nudge to commit the lockfile: do),
2. resolves and downloads the real Postgres 16 binaries (once, cached),
3. initializes a fresh data directory,
4. boots the server,
5. **converges** your declared shape — creates the `app` role, the `app`
   database, and grants — then
6. drops you into `psql`.

```
psql (16.14)
Type "help" for help.

app=#
```

It's a real, unmodified Postgres. Try `\du` and you'll see your `app` role; `\l`
shows the `app` database. Quit with `\q`.

> **doze converges *structure*, not data.** It created the database, role, and
> grants you declared — it did not insert any rows. Your app's migrations own the
> schema and data; doze owns the scaffolding around them.

## 4. See the magic: lazy boot and idle reap

See your declared instances and their state (everything's asleep until you
connect):

```sh
doze status
#   NAME   ENGINE     STATE    CONNS   RAM    UPTIME   ENDPOINT
#   app    postgres   idle     0       5M     3s       127.0.0.1:6432
```

`app` is **idle** — booted, but with no live connections. Connect to it:

```sh
psql "postgresql://app:app@127.0.0.1:6432/app" -c "select 1"
```

While that query runs, `doze status` shows the instance **active** with a live
connection. Close the client, wait for the idle timeout (5 minutes by default),
and it **reaps** back to zero — the process exits, RAM goes back to your laptop,
and the next connection boots it again. The reap keeps the data directory, so
waking back up is **sub-second** (only the very first boot of an instance takes a
few seconds) — you never have to think about starting or stopping it. See
[Waking back up](concepts.md#waking-back-up) for the full cost model.

You can watch all of this live:

```sh
doze dash      # an interactive dashboard; select a row to wake/sleep it
```

## 5. Add a cache

Real apps need more than a database. Add a Valkey (Redis-compatible) cache —
just declare it:

```hcl
postgres "app" {
  version = 16
  role "app" { password = "app" }
  grant {
    role       = "app"
    database   = "app"
    privileges = ["ALL"]
  }
}

valkey "cache" {
  version   = 9
  maxmemory = "256mb"
}
```

```sh
doze status
#   app    postgres   idle     …
#   cache  valkey     reaped   …    (boots when something connects)
```

Two engines, one file. Each has its own endpoint and its own lifecycle.

## 6. Wire it into your app

Your app shouldn't hardcode ports — except it doesn't have to. Every instance
listens on the explicit `port` you declared, so its URL is **stable and
deterministic**. Two honest ways to wire it in:

```sh
doze run -- <your dev server>     # npm run dev · rails server · go run ./... · python manage.py runserver
```

`doze run` just ensures the daemon and your backends are up, then runs your
command — whatever language it's in. Your app connects to the stable URLs you put
in its config (cold-booting each instance on first connect):

- `postgresql://app:app@127.0.0.1:5432/app` → the `app` Postgres
- `redis://127.0.0.1:6379` → the `cache` Valkey

Drop those straight into your `.env`, no magic required.

> Prefer doze to inject them for you? Declare your app as a `process` block and
> doze sets each dependency's `env_var` to its URL automatically. The daemon also
> writes the current set to `.doze/endpoints.yaml` for other tooling to read.

## 7. A throwaway database for your tests

Want a clean, real database for a test run? Reset the instance to a fresh slate,
then run your suite against it:

```sh
doze reset app          # wipe app's data; it re-provisions + converges on next boot
doze run -- pytest      # ensures backends are up, then runs your tests
```

`doze reset` gives you back a pristine `app` (structure converged, zero rows),
perfect when a run needs a known-clean database.

## You've got the model

That's the whole loop: **declare** in `doze.hcl`, **use** via `doze run` or a
direct connection to the stable port, and let doze **boot on demand** and **reap
when idle**. From here:

- **[Why doze](why-doze.md)** — the case against docker-compose / native installs,
  and **[the footprint numbers](resource-footprint.md)** behind "quiet laptop."
- **[The engines](engines.md)** — what Valkey, Kvrocks, and DocumentDB actually are.
- **[Core concepts](concepts.md)** — how lazy boot, reaping, convergence, and the
  proxy actually work.
- **[Files & storage](files-and-storage.md)** — where doze keeps everything, what
  to commit, and how to split your config across files.
- **[Recipes](../recipes/README.md)** — roles & grants, FIFO queues, SNS fanout,
  CI, and much more.
- **[Configuration reference](../reference/configuration.md)** — every field.
