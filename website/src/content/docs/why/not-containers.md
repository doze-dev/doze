---
title: "Native processes vs containers"
description: Why the services next to your editor should be processes on your machine, not containers in a VM, and what containers are still for.
---

You'll know why doze runs your services as native processes, and when a
container is still the better choice.

Containers are the right way to package software for production. This page is
about a different job: the database, cache, queue and services running next to
your editor while you build. For that job, doze runs them as processes on your
machine, and most of what is painful about local development with containers
comes from them not being that.

On a Mac, a container means a Linux VM: a reserved share of memory, a virtual
disk, a file-sharing layer, and a network boundary between you and every
service. doze has no VM, so none of those exist.

## Debugging

With doze, Postgres is a process, and so is your app. The tools you already
have work on both, with nothing in between:

- **Attach a debugger directly.** `dlv attach`, `lldb -p`, your IDE: onto your
  app while it talks to the real database. Pause a request handler, and in
  another pane `psql "$(doze url postgres)"` looks at the row your paused code
  just wrote. No remote-debug ports, and no path mapping, because the files in
  the debugger are your files.
- **See the engine itself.** `ps` lists it, Activity Monitor shows its real
  memory, `lsof -p` its sockets, and Instruments or `perf` profile it and your
  app in one trace, because there is one kernel.
- **Crashes land on your disk.** Logs, data folders and core dumps are
  ordinary files under `~/.doze`. `doze logs <name>` prints a service's last
  run even after it died, and so does `tail`.
- **One filesystem.** The bug that only happens because a bind mount changed
  permissions, case or timestamps cannot happen.

## Memory and battery

- **No VM.** A stack you are not using is a small daemon and one plugin per
  module type; every service in it can be asleep. See [Memory](/guides/memory/)
  for measured numbers and how they were taken.
- **Services sleep.** Two minutes with no connections and a service stops; its
  data stays. A compose stack runs everything in the file all day, because
  stopping and starting it by hand is a chore.
- **Native on Apple Silicon.** doze's engines are arm64 builds. No emulation
  for images that never got an arm64 build.
- **Waking is starting the engine.** There is no VM to wake and no image to
  pull.

## Fidelity

Postgres, MariaDB, Valkey, Kvrocks, FerretDB and Temporal are their upstream
programs, built from source and pinned by checksum. Every wire feature,
extension and client behaviour is the real one, because it is the same
software.

Kafka and AWS are different: they are doze's own servers,
[doze Kafka](https://doze-dev.com/docs/kafka/) and
[doze AWS](https://doze-dev.com/docs/aws/), written to speak the wire
protocols so unmodified clients work. Each says exactly what it supports and
what it does not, operation by operation; see doze AWS's
[API support](https://doze-dev.com/docs/aws/support/).

## What containers are still for

- **Production packaging.** doze is not for production: one copy of each
  service, no replication, and services that sleep.
- **An app that must match a Linux userland exactly.** If a bug only shows on
  Linux, run the app in a container or in CI. The two mix: a container can
  reach doze's services at their addresses.
- **Software that is only shipped as an image.** Run it as your own `service`
  with `doze add <name> --as container`; doze starts and stops the container
  like any other service. Anything with a binary runs natively the same way.
  See [Your own services](/hcl/your-own-services/).
- **Teams whose inner loop is Kubernetes.** If you already develop in a
  cluster, doze solves a problem you have chosen to solve differently.

[Alternatives](/why/alternatives/) goes tool by tool.
