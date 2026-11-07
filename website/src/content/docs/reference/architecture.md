---
title: "Architecture"
description: How doze is built (the binary, the daemon, modules, names and the app) and which repo holds each part.
---

This page explains how doze is put together, so you know which part does what when you read its code or a log.

doze is one Go program. The same binary is the CLI, the background daemon and
the desktop app. The daemon is a thin proxy in front of real services, one per
declared block: it starts each on its first connection and puts it to sleep
when nothing is using it.

```text
      ┌──────────────────────────── doze daemon (one per stack) ─────────────────────────┐
apps  │  proxy: one listener per service                                                  │
 and  │     accept ─▶ runtime: wake (deps first) ─▶ module: resolve, provision, run, sync │
tools ─▶    splice bytes ─▶ the real engine, on a local socket or private port            │
      │  sleep timer (by open connections)   supervisor   logs and journal   control IPC  │
      └──────────┬───────────────────────────────────────────────────────────────┬──────┘
                 │ gRPC (one plugin process per engine type)                       │ control socket
         ┌───────▼────────┐                                              ┌─────────▼────────┐
         │ modules        │  signed, from the doze registry              │ the app, the CLI │
         └────────────────┘                                              └──────────────────┘
```

## The parts

**The CLI.** Every `doze` command. It reads `doze.hcl`, starts the daemon
when needed, and talks to it over a control socket.

**The daemon.** One per stack, started by the CLI or the app and left
running in the background. It holds every declared port, wakes services, puts
them to sleep after the `sleep` time at zero connections, restarts what
crashes, keeps logs on disk per run, and records what it did in a journal. It
never cuts a connection to put something to sleep: a pool holding idle
connections keeps its service awake.

**Modules.** Every `module` block runs a module: a separate plugin program
from the [doze registry](/contribute/registry/), signed, pinned in
[`doze.lock`](/reference/lockfile/), and kept running for the daemon's
lifetime. A module knows its engine: which binary to run, how to set up a data
folder, how to converge declared structure (roles, schemas, buckets), and what
its console shows. doze itself knows no engine. Apps, services and tasks run
through the one driver built into doze, `process`.

**Engine binaries.** A module asks doze for its engine. doze downloads it from
[doze-binaries](/reference/binaries/), checks it, and caches it under
`~/.doze`.

**Names.** With names on, each service gets `<name>.<stack>.doze` on an
address of its own, so every Postgres can sit on 5432. A resolver answers the
`.doze` zone, and a front door serves apps at `http://<name>.<stack>.doze` on
:80 and their `domains` on :443. The zone, the front door and the addresses are
shared by every doze program on the machine (doze, doze AWS, doze Kafka):
whichever starts first answers for all of them. `doze dns-setup` installs a
small network daemon that holds the zone when no doze program runs and, on
macOS, gives services their addresses. See [Names and addresses](/hcl/names/).

**The certificate authority.** For an app's `domains`, doze mints
certificates from a local authority in `~/.doze/ca`, trusted on this machine
by `doze trust`.

**The app.** The desktop face of doze: one window for every stack on the
machine and a bat in the menu bar. It reads each stack's daemon over the same
control socket as the CLI, and runs doze commands for anything that changes
the machine. Quitting it leaves stacks running.

## A connection, step by step

1. The **proxy** accepts a connection on a service's listener. The listener
   says which service it is for, so nothing is parsed. An engine that needs a
   wire filter, as Postgres does for TLS, startup and cancel, runs it inside its module: doze hands
   it the connection's file descriptor.
2. It asks the **runtime** to wake the service. Wakes of the same service
   happen once. What it depends on wakes first and is held awake.
3. A cold wake runs the module: resolve the engine binary (cache or verified
   download, pinned), set up the data folder, start the engine under doze's
   supervisor and wait for its probe, and on first start converge the declared
   structure.
4. The proxy connects to the engine and copies bytes both ways. The open
   connection holds the service awake.
5. When the last connection closes, the **sleep timer** starts. After the
   `sleep` time (2 minutes by default) the service sleeps.

## Two versions, one lockfile

The **engine version** is what you write (`version = 18`), resolved against
doze-binaries. The **module version** is the plugin's own release. doze picks
it: the newest release in the module's signed index that speaks this doze's
plugin protocol and supports every engine major you declared. Both pin into
`doze.lock`. Pinned resolution needs no network, and pins move only with
`doze modules upgrade`.

## The repos

| Repo | What it holds |
|---|---|
| [doze](https://github.com/doze-dev/doze) | The CLI, daemon, config loader, proxy, runtime, supervisor, module fetcher and the app. Knows no engine. |
| [doze-sdk](https://github.com/doze-dev/doze-sdk) | The contract between doze and a module: the driver interfaces, the gRPC plugin transport, the signed-index format, and `enginetest`, the harness modules are tested with. |
| [doze-modules](https://github.com/doze-dev/doze-modules) | The modules for postgres, valkey, kvrocks, ferret, mariadb and temporal. New modules are [contributed here](/contribute/write/). |
| [doze-binaries](https://github.com/doze-dev/doze-binaries) | The engine binaries, built from source in CI and published append-only. |
| [doze-registry](https://github.com/doze-dev/doze-registry) | The registry at registry.doze-dev.com: each module's signed index and docs. Signing happens in its CI. |
| [doze-names](https://github.com/doze-dev/doze-names) | The `.doze` zone shared by every doze program: resolver, addresses, front door, certificate authority, and the network daemon. |
| [doze-aws](https://github.com/doze-dev/doze-aws) | Local AWS, standalone or as the `aws` module: S3, SQS, SNS, DynamoDB, Lambda, EventBridge, KMS, SSM, Secrets Manager, STS and Step Functions in one service. |
| [doze-kafka](https://github.com/doze-dev/doze-kafka) | A local Kafka, standalone or as the `kafka` module. |

The `aws` and `kafka` modules are built from their own repos and published to
the same registry.

## Packages in doze

| Package | What it does |
|---|---|
| `cmd/doze` | The commands. |
| `engine/process` | The one built-in driver: apps, services and tasks. |
| `internal/config` | `doze.hcl` to a checked model: variables, `for_each` and `count`, references and the dependency graph, files merged, errors with positions. |
| `internal/modules` | Fetches and checks modules from the registry, picks releases, writes module pins. |
| `internal/binaries` | Fetches and checks engine binaries and downloads; `doze.lock`. |
| `internal/runtime` | Wakes, holds and puts services to sleep, in dependency order. |
| `internal/proxy` | One listener per service; copies bytes; hands Postgres connections to its module. |
| `internal/supervisor` | Starts and stops one process: clean stop, restart, cleanup if doze dies. |
| `internal/daemon` | Wires the runtime, listeners, sleep timer, front door and control socket together. |
| `internal/control` | The control socket the CLI and the app talk to the daemon over. |
| `internal/logstore`, `internal/journal` | Logs per run on disk; the record of what the daemon did. |
| `internal/endpoints` | Each service's address and connection string, and `.doze/endpoints.yaml`. |
| `internal/detect` | What `doze init` reads in a project. |
| `internal/desk` | The app: window, menu bar, notifications, and its React page. |
| `internal/dash` | The terminal dash, for machines with no screen. |
