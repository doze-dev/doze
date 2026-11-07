---
title: "Files and storage"
description: Where doze keeps binaries, data, logs and state, what to commit, and how to clean up.
---

You'll be able to find any file doze writes, know which ones to commit, and
free disk space safely.

There are two places: a little in your project, and the rest in one folder per
user, `~/.doze`. To split `doze.hcl` itself into several files, see
[Splitting files](/hcl/splitting-files/).

## In your project

```text
larder/
  doze.hcl       you write it                           commit
  *.doze.hcl     more of the same, if you split it      commit
  doze.lock      doze writes it: what each version is    commit
  .doze/         doze writes it: this machine's state    ignored by itself
```

- **`doze.lock`** pins what doze downloads, so every machine runs the same
  bytes. Commit it. More [below](#dozelock).
- **`.doze/`** holds `endpoints.yaml` (each service's address and connection
  string, for scripts that want to read them) and `state.json`. doze writes a
  `.gitignore` inside the folder, so you don't need to add it to yours.

Your data is not in the project. That keeps the repository small, and a
`git clean` cannot delete a database.

## In `~/.doze`

```text
~/.doze/
  postgres/                      engine binaries, one folder per version and platform
    18.6.0-aarch64-apple-darwin/
    _templates/                  a ready-made empty cluster per version, copied on first wake
  valkey/  kafka/  …             the same for each engine
  modules/                       the module plugins doze fetched from the registry
  downloads/                     what a `service` with `download` fetched, by checksum
  cache/                         downloads in progress and registry metadata
  ca/                            the local certificate authority, after `doze trust`
  desk/                          the app: prefs.json and app.log
  names.json  routes.json  …     the machine's names and routes, shared by every stack
  projects/
    larder-3d586471/             one folder per project
      project.json               where its doze.hcl is, when it was last up, what was started
      clusters/<service>/        each service's data
      logs/                      what each service printed, per run
      run/
        doze.log                 the stack's daemon log
        journal.jsonl            every wake, sleep, crash and task run, for Activity
        …                        sockets and the daemon's pid
```

The examples use **Larder**, a made-up grocery service.

- **Binaries are shared.** Ten projects on Postgres 18.6 use one download.
  You download a version the first time any project needs it.
- **A project's folder** is named after its folder plus a short hash of its
  path, so two projects both called `api` never meet. `doze doctor` prints it
  on its `project` line.
- **Logs** are kept per run on disk, so `doze logs` and the app's Logs tab work
  for a service that is asleep or crashed. `log_runs` and `log_size` in
  `defaults` set how much is kept.
- **The journal** is what the app's Activity view reads: how long each service
  slept, what woke it, what crashed.
- **`ca/`** exists only after `doze trust`, for apps with `domains`. Its key is
  readable by you alone. See [Domains and HTTPS](/app/domains/).

## doze.lock

`doze.lock` has a `doze:` line and three layers:

```yaml
doze: 0.2.0          # the newest doze that wrote this file
engines:             # each engine version you declared, resolved to a build
  postgres:
    "18":
      resolved: 18.6.0
      source: mirror
      hashes:
        aarch64-apple-darwin: sha256:…
modules:             # the module release that runs each engine
  doze/postgres:
    version: …
    protocol: 1
    hashes:
      aarch64-apple-darwin: sha256:…
downloads:           # each URL a `service` downloads, and what it served the first time
  https://github.com/axllent/mailpit/releases/download/v1.21.8/mailpit-darwin-arm64.tar.gz:
    sha256: sha256:…
```

- **`engines`**: `version = 18` becomes an exact build, with a checksum for
  each platform.
- **`modules`**: the plugin that runs the engine, with its release and
  checksums. Pins move only when you run `doze modules upgrade`.
- **`downloads`**: a download that later serves different bytes is refused.
- **`doze:`**: an older doze reading the file says it is older, since a newer
  one may have pinned something it cannot run.

How the registry signs what doze fetches is in
[The trust model](/why/trust/). The fields are in the
[doze.lock reference](/reference/lockfile/).

## Moving it

Two root settings in `doze.hcl` move the folders:

```hcl
home     = "/Volumes/work/doze" # the whole ~/.doze, for this project (or set DOZE_HOME)
data_dir = "./.doze-data" # only this project's folder, here inside the repo
```

`DOZE_HOME` in the environment moves `~/.doze` for every project.
`data_dir` inside the repository keeps a throwaway stack self-contained; add
it to `.gitignore`, since it holds real database files.

## Freeing space

| To | Do |
|---|---|
| Put a service to sleep, keeping its data | `doze sleep postgres` |
| Throw away one service's data | `doze reset postgres` (it asks; the next wake starts fresh) |
| Throw away a whole project's data | `doze down`, then delete its folder under `~/.doze/projects/` |
| Drop an engine version no project uses | delete its folder, such as `~/.doze/postgres/16.15.0-aarch64-apple-darwin` |
| Drop old Postgres data kept by `doze migrate` | delete `clusters/<name>.pg16-…` and the `-dump.sql` beside it |

Anything doze downloaded comes back on the next use, checked against
`doze.lock`. Your `doze.hcl` and `doze.lock` are never touched.

The app's **Settings › Machine** has a **Left behind** line: processes, folders
and routes that belong to no stack on Home.

## Removing everything

To remove doze from a machine, undo the machine setup first, then delete the
folder:

```sh
doze trust --remove        # in each project that used domains
doze dns-setup --uninstall
rm -rf ~/.doze
```

Then remove the app the way you installed it. On Linux, see
[Linux](/guides/linux/#removing-it).
