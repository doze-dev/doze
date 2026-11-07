---
title: "Memory"
description: What doze holds in memory when a stack is idle and while you work, measured, with how to measure it yourself.
redirect_from: ["/guides/resource-footprint/"]
---

You'll be able to say what a doze stack costs in memory, from numbers that say
how they were taken, and check them on your own machine.

## What runs, and when

- **One daemon per stack that is up.** It holds each service's port, wakes a
  service when something connects, and puts it to sleep after two minutes with
  no connections. `doze down` stops it.
- **One small plugin per module type**, which the daemon keeps running while
  the stack is up (one for Postgres, one for Valkey, and so on).
- **The services themselves, only while awake.** An asleep Postgres is no
  process at all; its data stays on disk.
- **Apps with no port, and services set to `sleep = "never"`,** run the whole
  time the stack is up.

So the cost of a stack you are not using is the daemon and its plugins. The
cost while you work is that plus each awake service, which is the engine's
own memory: doze runs the real binary, with no VM or container around it.

## Measured

### A stack with everything asleep

Measured on 2026-11-07 on an Apple Silicon Mac (macOS 27), with `ps -o rss=`,
on doze's own test stack (`examples/shop`: Postgres, Valkey, Kafka and AWS
modules, three apps, a service and two tasks). The stack had been up for three
hours, and every service was asleep.

| Process | Resident memory |
|---|---|
| the stack's daemon | 31.6 MB |
| postgres plugin | 20.1 MB |
| valkey plugin | 19.4 MB |
| kafka plugin | 21.6 MB |
| aws plugin | 20.6 MB |
| **total** | **113.2 MB** |

A stack with fewer module types has fewer plugins.

### The daemon does not grow with logs

`make bench` starts a stack with two apps, one silent and one that prints
100,000 lines, and reads the daemon's memory before and after. Logs go to disk,
so the daemon should not grow. A run on 2026-11-07, on the same Mac:

| Before | After 100,000 lines | Growth |
|---|---|---|
| 26.8 MB | 28.6 MB | +1.8 MB |

An earlier run on an Apple Silicon Mac read 27.1 MB before and +1.3 MB after.
The growth is the Go runtime's own noise; the test fails above 3 MB.

### Held to a budget on every change

The same `make bench` runs on every pull request, on macOS and Linux, against
the `examples/shop` stack with everything asleep. It fails if the daemon or the
daemon and its plugins go over these ceilings (`test/bench/budget.json`):

| | macOS | Linux |
|---|---|---|
| the daemon | 34 MB | 40 MB |
| the daemon and its plugins | 250 MB | 230 MB |

These are limits, not measurements: the real figures sit under them, as in the
first table.

## Not counted above

- **The app.** The window and the menu bar are a separate process. `make
  deskbench` measures it (menu bar alone, window open, a minute after close,
  CPU at rest) against `test/deskbench/budget.json`; it does not run on every
  change yet, and this page has no figure from it.
- **The network daemon** that answers `.doze` names when no stack runs. It is
  one process per machine, not per stack. Measured the same way as above,
  after a day and a half of running: 18.6 MB.
- **Awake services.** Each is the engine's own process. Its size depends on
  the engine, its settings (`shared_buffers`, `maxmemory`) and your data, so
  look at your own: the app shows each service's memory on its card and inside
  the service, and `doze status` has a memory column.

## Measure it yourself

```sh
doze status                            # memory per service, for the stack in this folder
ps -o rss=,command= -p <pid>           # one process, in KB
make bench                             # in a doze checkout: the log test, and the
                                       # full stack test when its ports are free
```

`make bench` downloads real engines into a private folder the first time. The
full-stack test needs ports 5432, 6379, 9092, 4566, 8080 and 8090 free and
skips when they are not.

## Disk

Engine binaries are downloaded once per version and shared by every project;
data lives per project. Both are under `~/.doze`. See
[Files and storage](/guides/files-and-storage/).
