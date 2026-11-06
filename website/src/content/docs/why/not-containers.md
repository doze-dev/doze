---
title: "Native processes vs containers, for development"
description: Why your dev backend should be a process you can see, debug, and profile — not a VM you tunnel into.
---

Containers won production packaging, and deservedly. This page is about a
different job: the database, cache, and queue running next to your editor while
you build. For that job, doze's position is simple — **your backing services
should be native processes on your machine**, and almost everything painful
about local development with containers follows from them not being that.

On macOS and Windows, "a container" means a Linux VM. Docker Desktop, Colima,
OrbStack — better and worse VMs, but VMs: a reserved slab of RAM, a virtual
disk, a file-sharing layer, and a network boundary between you and every
service you run. doze removes the boundary instead of optimizing it.

## The debugger story

This is the centerpiece, because it's where the boundary costs you the most and
gets discussed the least.

With doze, Postgres is a **process**. Your app is a process. The whole
observability toolbox you already own works on both, with nothing in between:

- **Attach anything, directly.** `dlv attach`, `lldb -p`, an IDE debugger —
  onto your app while it talks to the real database. Breakpoint a request
  handler, step into the query call, and in another pane `psql "$(doze url app)"` is a
  real `psql` inside the same server, inspecting the row your paused code just
  wrote. No remote-debug ports, no "attach to process in container", no
  source-path mapping because the filesystem in the debugger *is* your
  filesystem.
- **See the engine itself.** `ps aux | grep postgres` shows it. Activity
  Monitor shows its real memory. `lsof -p` shows its sockets and files.
  `strace`/`dtruss` show its actual syscalls. `perf` / Instruments profile it —
  and your app — in one trace, because there's one kernel. Inside a VM, every
  one of those tools either lies (it sees the VM) or requires tunneling into a
  minimal container image that doesn't have them.
- **Crashes land on your desk.** Core dumps, engine logs, data directories —
  ordinary paths under your project's doze home (`doze logs app` prints them,
  but so does `tail`). Not volumes you have to `docker cp` out of a stopped
  container before it's garbage-collected.
- **No filesystem skew.** The classic container heisenbug — works locally,
  fails in the container, because the bind mount changed permissions, case
  sensitivity, or mtimes — cannot happen. There is no second filesystem.

None of this is exotic. It's how development worked before we put a VM in the
middle — doze just brings it back without the old costs (version drift, port
conflicts, always-on services).

## The resource story

Numbers, not vibes — [measured here](/guides/resource-footprint/):

- **At rest, doze is one daemon, ~15 MB RSS.** Not "small VM": no VM. A Docker
  Desktop installation idles at hundreds of MB to several GB reserved, running
  or not, because the VM holds its allocation.
- **Engines sleep.** Zero connections for a few minutes and the instance is
  asleep. The whole stack exists only while something talks to it —
  compose stacks run everything you defined, all day, because stopping and
  starting them is annoying enough that nobody does.
- **Apple Silicon runs native binaries.** doze's engines are arm64 builds. No
  Rosetta, no qemu emulation for the images that never got multi-arch builds.
- **Boot is engine boot.** First connection cold-boots Postgres in well under a
  second (after one-time provisioning) because there's no VM to wake and no
  image to pull.

## The fidelity story

doze runs **real, unmodified upstream binaries** — the actual Postgres 18, the
actual Valkey — built from source and checksum-pinned. Every wire feature,
extension, collation quirk, and client behavior is exactly production's,
because it's the same software. Where doze provides AWS services (S3/SQS/SNS),
they're honest local stand-ins and [documented as such](/guides/engines/) — but
your *database* is never an approximation.

Contrast the emulation end of the spectrum: LocalStack reimplements AWS APIs in
Python (~1.2 GB image, JVM for some services), and its coverage matrix is a
thing you check. Reimplementations drift; upstream binaries can't.

## What containers are still for

An honest list, because pretending otherwise would cost this page its
credibility:

- **Production packaging and orchestration.** doze is explicitly not for
  production — no HA, no replication, sleeps when idle. Ship containers.
- **Your app's own runtime, when it must match Linux exactly.** doze manages
  backing services; if your bug only reproduces on a Linux userland, you want
  a container (or CI) for the *app*. The two compose fine — doze's services
  listen on ordinary localhost ports that a container can reach.
- **Services doze has no module for (yet).** Some vendor box with a bespoke
  daemon? Run it however you can — or [write the module](/modules/overview/);
  it's an afternoon.
- **Teams standardized on dev-in-k8s.** If your inner loop already runs in a
  cluster (Tilt et al.), doze solves a problem you've chosen to solve
  differently.

The [alternatives page](/why/alternatives/) goes tool by tool.
