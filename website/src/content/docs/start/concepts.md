---
title: "Core concepts"
---


Here's a promise: there are only a handful of ideas in doze, and once they click,
the whole tool stops needing explanation — you'll be able to guess how it behaves
because it all follows from the same few principles. So let's go through them
unhurried. (Want the *why* before the *how*? [Why doze](/why/doze/). Just here for
the engines? [The engines](/guides/engines/).)

**A note on words.** Throughout the docs:

- **engine** — the software (PostgreSQL, Valkey, S3, …).
- **instance** — one thing you declare (`postgres "app" {}` is the `app`
  instance); each has its own data, endpoint, and lifecycle.
- **backend** — the actual process running an instance once it's booted.
- **daemon** — the long-running daemon that fronts every instance.

## The daemon and per-instance endpoints

doze runs a small background **daemon** (started for you automatically by most
commands). For every instance you declare, the daemon opens **one listener** at
its own address — `app` on `127.0.0.1:6432`, `cache` on `:6433`, and so on.

Because each listener belongs to exactly one instance, doze knows what you want
the moment you connect — there's nothing to route or parse. Your client talks to
the listener; doze stands in front of the real engine.

## Lazy boot

When the first connection arrives on an instance's listener, doze:

1. resolves and (once) downloads the engine binaries,
2. provisions a data directory if this is a cold start,
3. starts the real backend on a private socket,
4. waits until it actually accepts connections, then
5. **splices** your connection to it — copying bytes in both directions, with no
   protocol emulation in the middle.

After that first boot, doze is just a thin pipe. The engine is real and behaves
exactly like the same version would in production.

If ten connections race in at once on a cold instance, doze coalesces them: one
boot happens, and everyone attaches to it (this is the "singleflight" in the
internals).

## Idle reap

doze reaps an instance when it has had **zero live connections** for the
`idle_timeout` (5 minutes by default). The key word is *connections*, not
*queries*:

> doze reaps on connection count, **never** on query inactivity. A connection
> pool that holds idle connections open keeps its backend alive — doze will never
> pull the rug out from under your app.

When you stop touching an instance and your clients disconnect, it sleeps. RAM
returns to your machine. The next connection boots it again. This is why a
laptop running doze is quiet: nothing runs unless something is using it — at rest
it's one ~15 MB daemon and zero engine processes (see
[Resource footprint](/guides/resource-footprint/)).

You can reap on demand (`doze sleep <name>`), boot eagerly (`doze wake <name>`), or just let
it happen.

## Waking back up

If reaping shut the engine down, isn't reconnecting slow? No — and this is the
point that makes the whole sleep/wake cycle worth it.

**A reap stops the process but keeps the data directory.** So waking an instance
isn't a from-scratch boot: doze just restarts the backend on the files that are
already there and runs the (idempotent, no-op) convergence. There's no
re-`initdb` and no template clone. In practice that's **sub-second** — your next
connection cold-boots the engine and runs its first query in a fraction of a
second, then everything after is a thin pipe.

The slower paths are one-offs you pay once, not on every wake:

| Event | Cost | How often |
|---|---|---|
| **Wake a reaped instance** | sub-second (just restart the backend) | every idle → reconnect |
| **First boot of a new instance** | a few seconds (clone the [template](#real-engines-pinned-for-reproducibility), provision roles/db) | once per instance, ever |
| **First use of an engine version** | + a one-time binary download | once per version, ever |

So from your app's side there's simply "a Postgres at this address." doze drops
it when nobody's connected and brings it back the instant someone knocks — and
because the data and provisioning survive, knocking is cheap.

## Convergence: structure, not data

When an instance boots fresh (and whenever you run `doze sync`), doze **converges**
it to the shape you declared:

- Postgres → databases, roles, schemas, grants, extensions
- S3 → buckets
- SQS → queues and redrive policies
- SNS → topics and subscriptions

Convergence is **idempotent** — running it again is a no-op. And it deliberately
stops at *structure*: doze never seeds rows or runs your migrations. Your
application owns its data; doze owns the scaffolding so the data has somewhere to
live.

## Endpoints and connection strings

Apps shouldn't have to guess ports — and they don't have to. Every instance
listens on the explicit `port` you declared in `doze.hcl`, so its address and
connection string are **stable and deterministic**. There are three honest ways
to get a URL into your app:

1. **Write the stable URL directly** in your app config or `.env` —
   `postgresql://app:app@127.0.0.1:5432/app`, `redis://127.0.0.1:6379`,
   `mongodb://127.0.0.1:27017/`. Connecting cold-boots the instance.
2. **Declare your app as a `process` block** — doze injects each declared
   dependency's `env_var` → its URL automatically when it runs the process.
3. **Read `.doze/endpoints.yaml`** — the daemon writes the current address and
   connection string for every instance there, for other tooling to consume.

## Real engines, pinned for reproducibility

doze never runs a system install or an emulation. It resolves each
`(engine, version)` cheapest-first:

1. **`DOZE_<ENGINE>_BINDIR`** — an explicit bin directory you point at (CI, local builds).
2. A **content-addressed cache** under `~/.doze/<engine>/<version>-<platform>/bin`.
3. A **verified download** from the [doze-binaries](https://github.com/doze-dev/doze-binaries) mirror, SHA-256 checked.

The exact version each instance resolved to, and its checksum, are recorded in a
committed **`doze.lock`** — so a teammate's clone and your CI run byte-identical
software. A bare major (`version = 16`) resolves to the newest minor and pins it;
a dotted string (`version = "16.14"`) pins exactly. See
[Managing binaries](/reference/binaries/).

## Engines are modules

The doze binary you install contains no databases — every engine (except the
`process` primitive) is a **module**: a separately-versioned plugin fetched from
the signed registry the first time your config names its type. Think Terraform
providers. What keeps this invisible day to day:

- **You never pick a module version.** `version = 18` names the *engine*; doze
  selects the newest module release that supports it and speaks this doze's
  plugin protocol, then pins the choice — release, supported engine versions,
  per-platform checksums — in `doze.lock`.
- **Nothing runs unverified.** The registry index carrying each module's
  compatibility metadata is ed25519-signed; each archive's checksum is signed;
  the publisher's key is pinned on first use in your lock. A tampered index, a
  swapped archive, or a rotated key is a hard error, not a warning.
- **Pins move only when you say so.** `doze modules upgrade --check` reports
  waiting updates (exit 1 in CI); `doze modules upgrade` moves the pins. When a
  config needs a newer module — you declared `version = 19`, or used an
  argument added in a later release — the error names that exact command.
- **Everything is inspectable.** The [registry](https://doze.nerdmenot.in/registry/)
  shows every module's engine versions, platforms, config reference, and release
  history — generated from the module itself; `doze.lock` shows exactly what
  this project runs and the signatures it pinned.

Offline behavior matches the lockfile promise: a pinned, cached module resolves
with zero network. Writing your own engine? Start at
[module-template](https://github.com/doze-dev/module-template).

## Per-instance isolation

Each instance is its own real server with its own data directory, namespaced
under your project. Two projects, or two `postgres` blocks in one project, never
collide — different data dirs, different endpoints, independent lifecycles. You
can run Postgres 14 and 17 side by side without a second thought.

## Instance dependencies

Some instances need another to function — **SNS** fans out to an **SQS** queue.
You express it with a typed reference, and doze derives the dependency and handles
the lifecycle:

```hcl
sqs "jobs" {
  queue "emails" {}
}
sns "events" {
  sqs = sqs.jobs.name        # reference → events depends on jobs
  topic "signups" {}
}
```

Booting `events` boots `jobs` first, injects its connection info, and **holds it
running** for as long as `events` runs (so the reaper won't take the backend out
from under it). Stopping `events` releases it. The ordering falls out of the
reference — there's nothing extra to declare.

(DocumentDB *looks* like it needs a Postgres backend, but it's self-contained:
doze runs that Postgres privately and exposes only the Mongo wire.)

## Local AWS, built in

S3, SQS, and SNS aren't downloaded binaries — they're implemented in pure Go and
ship *inside* the doze binary. doze runs each as a managed child process behind
the same proxy, so they cold-boot, persist to disk, and reap just like the
databases. That's how doze offers "local AWS" with no Docker, no JVM, and no
LocalStack. (S3 embeds [gofakes3](https://github.com/johannesboyne/gofakes3);
SQS and SNS are built from scratch.)

## Built to be unsupervised

doze is meant to fade into the background, so it heals itself:

- A backend that crashes is detected and marked reaped, so your next connection
  cleanly re-boots it instead of hitting a dead socket.
- Boot and convergence failures are recorded and surfaced in `doze status` and
  `doze doctor` — not swallowed.
- Daemon shutdown is bounded so it can't hang, and on startup the daemon reclaims
  any backends orphaned by a previous crash.

## Storage layout

Everything lives under `$DOZE_HOME` (default `~/.doze`), laid out like
[moonrepo's proto](https://moonrepo.dev/proto): a shared, deduplicated tool store
plus per-project state.

```
~/.doze/
  postgres/  valkey/  kvrocks/  documentdb/      # shared engine toolchains (cached once)
  postgres/_templates/16.14.0/                   # copy-on-write boot template
  projects/myapp-1a2b3c4d/                       # this project's data dirs, sockets, logs
```

→ **[Files & storage](/guides/files-and-storage/)** covers the full layout, what to
commit vs ignore, relocating state, and cleaning up.

Where to next: **[Recipes](/guides/engines/)** for concrete patterns, or the
**[Configuration reference](/reference/configuration/)** for every field.
