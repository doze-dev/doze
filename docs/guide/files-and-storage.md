# Files & storage

There are two kinds of files in a doze project: **what you write** (a little
config you commit) and **what doze manages** (engine binaries, data, sockets, and
logs it keeps out of your way). This page explains exactly where everything lives,
what to commit versus ignore, how to split your config across files, and how to
reset or clean things up.

## The short version

```
my-app/
  doze.hcl            # you write this  — your declared instances        (commit)
  doze.lock           # doze writes this — resolved versions + checksums  (commit)
  *.doze.hcl          # optional         — extra config, split by topic   (commit)
  .doze/              # doze writes this — runtime manifest               (gitignore)
```

Everything heavy — the actual Postgres/Redis binaries and your databases' data —
lives **outside your project**, in a shared home at `~/.doze`. Your repo stays
tiny.

## What you write

### `doze.hcl`
Your declared instances and root settings. The one file you really author. See the
[configuration reference](../reference/configuration.md).

### `*.doze.hcl` (optional)
You can split instances across sibling `*.doze.hcl` files — doze merges them
automatically. See [breaking config into files](#breaking-config-into-files).

### `doze.lock` — commit it
When doze resolves a version (e.g. `version = 16` → `16.14.0`), it records the
exact version and its per-platform SHA-256 in `doze.lock`, right next to
`doze.hcl`. **Commit it.** It's doze's `package-lock.json`: every teammate and
your CI then download byte-identical engine binaries. See
[Managing binaries](../BINARIES.md).

## What doze manages

### The home — `~/.doze` (shared across all your projects)

One machine-wide directory holds the engine toolchains and per-project state,
laid out like [moonrepo's proto](https://moonrepo.dev/proto):

```
~/.doze/
  postgres/                                  # engine toolchains, downloaded once…
    16.14.0-aarch64-apple-darwin/bin         #   …and shared by every project
    _templates/16.14.0/                      # copy-on-write boot template (initdb once)
  valkey/  kvrocks/  documentdb/             # same idea per engine
  cache/                                     # transient downloads
  tls/                                       # auto-generated self-signed cert (shared)
  projects/                                  # per-project state (see below)
    my-app-1a2b3c4d/
    other-app-9f8e7d6c/
```

Because toolchains live here and are content-addressed, ten projects that all use
Postgres 16.14 share **one** download. Relocate the whole home with `$DOZE_HOME`
or the `home` setting.

### Per-project state — `~/.doze/projects/<slug>/`

Each project gets its own namespaced subdirectory (the slug is your project folder
name plus a short hash of its path, so two `app` folders never collide):

```
~/.doze/projects/my-app-1a2b3c4d/
  clusters/                  # each instance's DATA lives here
    app/                     #   the `app` Postgres data directory
    cache/                   #   the `cache` Valkey data directory
  run/                       # runtime files
    doze.pid                 #   the daemon's pid
    doze.admin.sock          #   the control socket the CLI/TUI talk to
    doze.log                 #   the daemon log (doze logs)
    app/                     #   the `app` backend's private socket
    backend-app.pid          #   for crash/orphan reconciliation
```

Find a project's directory any time:

```sh
doze doctor      # the `project` line prints this path
```

### `.doze/endpoints.yaml` (project-local)

When the daemon is up it writes `.doze/endpoints.yaml` next to your `doze.hcl` —
the current address and connection string for each instance, for other tooling to
read. It's regenerated on demand, so **gitignore it**.

## Putting state somewhere else

By default a project's state lives under the shared home. Two overrides:

```hcl
home     = "~/.doze"         # relocate the shared tool store + cache (or set $DOZE_HOME)
data_dir = "./.doze-state"   # keep THIS project's data inside the repo instead
```

- **`data_dir`** moves clusters/sockets/logs for *this* project. Pointing it at a
  path inside your repo (e.g. `./.doze-state`) keeps everything self-contained —
  just gitignore it. Handy for throwaway sandboxes or fully isolated CI.
- **`home` / `$DOZE_HOME`** moves the shared, cross-project tool store.

## Commit vs ignore

| File | Commit? | Why |
|---|---|---|
| `doze.hcl` | ✅ | your declared environment |
| `doze.lock` | ✅ | byte-identical binaries for the whole team |
| `*.doze.hcl` | ✅ | shared split config |
| `local.doze.hcl` | ❌ | per-developer overrides (see below) |
| `.doze/` | ❌ | regenerated runtime manifest |
| `data_dir` if inside the repo | ❌ | actual database data |

A typical `.gitignore`:

```gitignore
.doze/
local.doze.hcl
.doze-state/        # only if you set data_dir to an in-repo path
```

## Breaking config into files

For anything past a couple of instances, split `doze.hcl` into sibling
`*.doze.hcl` files. doze loads `doze.hcl` first (it holds the root settings), then
merges every `*.doze.hcl` beside it in sorted order — it's all one config. (A plain
`*.hcl` sibling is left alone; only the `*.doze.hcl` suffix is auto-merged.)

```
my-app/
  doze.hcl              # root settings (listen/defaults/tls) + core instances
  databases.doze.hcl    # postgres instances
  cache.doze.hcl        # valkey / kvrocks
  aws.doze.hcl          # s3 / sqs / sns
```

```hcl
# doze.hcl  — root settings live here, and only here
defaults { idle_timeout = "5m" }

postgres "app" {
  version = 16
  role "app" { password = "app" }
}
```

```hcl
# aws.doze.hcl  — just more instances
s3 "media" {
  bucket "uploads" {}
}
sqs "jobs" {
  queue "emails" {}
}
```

```sh
doze status     # shows app (from doze.hcl) and media + jobs (from aws.doze.hcl)
doze doctor     # validates the merged whole
```

**Rules of thumb**

- **Root settings** (`listen`, `home`, `data_dir`, `defaults`, `tls`) belong in
  `doze.hcl`. A duplicate `defaults`/`tls` across files is a (clearly reported)
  error.
- **Instance blocks** can live in any file; names must be unique across all of
  them.
- Or point `--config` at a directory to merge every `*.hcl` in it:
  ```sh
  doze --config ./config status
  ```
- Errors are reported with the **file, line, and a snippet** — including
  cross-file duplicates and unknown blocks (with a "did you mean?" hint).

### Per-developer overrides

Keep shared instances committed in `doze.hcl`, and let each developer add personal
ones in a **gitignored** `local.doze.hcl`:

```hcl
# local.doze.hcl  (gitignored — yours alone)
postgres "scratch" {
  version = 17
  role "me" { password = "me" }
}
```

Everyone shares the same baseline; your scratch instance never touches the team's
config.

## Multiple projects on one machine

Run doze from each project directory and they stay completely independent — own
data, own endpoints, own daemon — while **sharing** the downloaded toolchains
under `~/.doze`. You can have Postgres 14 in one repo and 17 in another with zero
conflict.

## Resetting & cleaning up

```sh
# Put an instance to sleep now (data is kept; next connect re-boots it)
doze stop app

# Wipe one instance's data and start fresh (re-provisions + converges on next boot)
doze stop app
rm -rf "$(doze doctor | awk '/project/{print $3}')/clusters/app"

# Nuke a whole project's state (stop the daemon first)
doze stop --all
rm -rf "$(doze doctor | awk '/project/{print $3}')"

# Reclaim disk from downloads / unused toolchains
rm -rf ~/.doze/cache
rm -rf ~/.doze/postgres/15.18.0-*    # an engine version you no longer use
```

Nothing here touches your `doze.hcl` or `doze.lock` — recreate the data any time
by connecting again.

---

Next: the **[configuration reference](../reference/configuration.md)** for every
field, or **[recipes](../recipes/README.md)** for concrete setups.
