---
title: "Why doze?"
---


You're building an app. It needs a database. Probably a cache too, maybe a queue,
a bucket, a document store. None of that is your app — it's the *infrastructure
your app talks to* — but you still have to run it on your laptop to get any work
done.

There are three usual ways to do that, and all three are genuinely good tools
that solved real problems. It's worth being clear about that up front, because
this page is going to point at their costs — and the costs are real — but none
of these are mistakes. They're just carrying weight your laptop doesn't need to
carry for *local development* specifically.

## The three familiar taxes

### 1. Docker / docker-compose

docker-compose is, deservedly, how most of us have done this for years — one
file, `docker compose up`, works anywhere Docker runs. It's a fantastic answer
to "package and ship this stack." As a *development* environment on a Mac or
Windows machine, though, it asks for a Linux VM that runs all day. Docker
Desktop reserves a big slice of your RAM for that VM by default — often several
gigabytes — whether or not a single container is up.[^docker-mem] On Apple
Silicon, any image without an ARM build runs under **emulation**, which is
slower and spins the fan.[^docker-emu] And it's all-or-nothing: `docker compose
up` starts the *entire* stack even when you're only touching one service.

None of that is a flaw in compose. It's the price of virtualization, paid all
day, for a job — local dev — that maybe doesn't need the virtualization at all.

### 2. `brew install` everything

So you skip Docker and install Postgres and Redis natively. Now they're fast —
but you've traded one problem for three:

- **Version drift.** Your laptop has Postgres 16; your teammate has 15; CI has 14.
  "Works on my machine" is now a daily occurrence.
- **Port conflicts.** Which `postgres` is on 5432 right now? The one for *this*
  project, or the one you forgot to stop last week?
- **Always running.** `brew services` starts them at login and they idle in the
  background forever, because turning them on and off by hand is a chore.

There's no per-project isolation and no way to pin the *exact* versions everyone
should run.

### 3. LocalStack for the AWS bits

Your app uses S3 and SQS, so you add LocalStack — which, credit where it's due,
gives you an astonishing amount of the AWS surface in one place, and is the right
call when you need the breadth. For a bucket and a queue, though, it's a ~1.2 GB
Docker image running Python, with some services delegating to **Java** emulators
under the hood.[^localstack] So you're back to option 1 — Docker, plus Python,
plus a JVM — to stand in for a bucket.

---

There's a common thread, and it isn't that these tools are bad — they're not.
It's that **to run real-enough infrastructure, each asks you to pay for it
continuously** — in RAM, in fan noise, in battery, in version drift, in
ceremony — whether you're using it this second or not. Somewhere along the way,
"convenient" quietly came to mean "always running, always costing." doze is an
attempt to question that.

## What doze does instead

doze keeps the good part of every option and drops the tax.

> **Real engines that sleep.** doze runs the *actual* PostgreSQL, Valkey,
> Kvrocks, and DocumentDB binaries — plus pure-Go S3, SQS, and SNS — but only while
> a client is connected. Declare what you need in one file; doze fetches pinned
> binaries, boots each engine on first connect, splices your connection straight
> through, and returns the RAM the moment you walk away.

Concretely:

- **Idle is nearly free.** When nothing is connected, the only thing running is
  one small daemon — about **15 MB of RAM**, with **zero engine processes**. No
  VM, no Docker, no JVM. Your laptop is quiet. (See the
  [measured footprint](/guides/resource-footprint/).)
- **You pay only for what you touch.** Connect to your database and doze boots
  *that one engine* — a real, native ~5 MB Postgres — and nothing else. Stop, and
  it reaps back to zero a few minutes later.
- **It's real, not emulated.** doze doesn't reimplement Postgres or fake the wire
  protocol; it runs the genuine binary and gets out of the way. Every client,
  extension, and protocol feature behaves exactly like production.
- **It's reproducible.** Exact versions are pinned in a committed `doze.lock`, so
  your machine, your teammate's, and CI all download byte-identical software.
- **One file, no orchestration.** `doze.hcl` describes the end state. There's no
  compose file to babysit, no ports to assign, no `services:` to start and stop.

| | Docker / compose | `brew install` | LocalStack | **doze** |
|---|---|---|---|---|
| Setup | a compose file per project | manual, per-machine | a container | **one `doze.hcl`** |
| Idle cost | a VM, ~½ your RAM | services run at login | Docker + Python + JVM | **~15 MB, engines asleep** |
| Only run what you use | no — whole stack | no — all or nothing | no | **yes — boot on connect** |
| Pinned versions | image tags | none | image tag | **`doze.lock`, exact** |
| Apple Silicon | often emulated | native | emulated | **native** |
| Local AWS | extra tooling | n/a | yes (heavy) | **built in, pure Go** |

## The engines are part of the point

doze doesn't just run things lightly — it runs the *right* things. The non-Postgres
engines are deliberately chosen to be **cheap, real, license-clean local
stand-ins** for software that's otherwise heavy or encumbered:

- **Valkey** is the open-source continuation of Redis (Redis itself left open
  source in 2024) — a drop-in cache your existing Redis code talks to unchanged.
- **Kvrocks** speaks the same Redis protocol but stores data on disk via RocksDB,
  so it's a low-RAM stand-in when you have more keys than you want resident in
  memory.
- **DocumentDB** gives you a MongoDB-compatible document store on top of Postgres —
  "Mongo" locally without MongoDB's restrictive license.

The full story, with the licensing and cost details, is in **[The
engines](/guides/engines/)**.

## Is doze for you?

**Yes, if** you develop locally, run automated tests, or build CI pipelines and
you want real infrastructure without the always-on weight. If you've ever run
`docker compose up` just to work on one service, doze is for you.

**No, if** you need production infrastructure. doze runs **single** local
instances — no replication, no HA, no failover — tuned toward fast iteration over
durability, and it reaps them when idle. It's not a place to keep data you can't
lose, and its S3/SQS/SNS are dev-grade conveniences, not a substitute for real
AWS. Use managed databases and real AWS in production. (Full rationale in the
[FAQ](/guides/faq/#is-doze-production-ready).)

**Platforms:** macOS and Linux, on Apple Silicon and x86-64. No native Windows
(WSL2 works).

---

**Next:** see the numbers in **[Resource footprint](/guides/resource-footprint/)**, meet
the engines in **[The engines](/guides/engines/)**, or just start building with
**[Getting started](/start/getting-started/)**.

[^docker-mem]: Docker Desktop allocates 50% of host memory to its Linux VM by
    default, and the VM runs even with no containers; the "Resource Saver"
    feature was added specifically to reclaim that idle memory. See
    [Docker Desktop settings](https://docs.docker.com/desktop/settings-and-maintenance/settings/).

[^docker-emu]: x86/amd64-only images run under emulation (QEMU, or Rosetta 2 when
    enabled) on Apple Silicon, which is slower than native arm64. See
    [Docker Desktop known issues](https://docs.docker.com/desktop/troubleshoot-and-support/troubleshoot/known-issues/).

[^localstack]: LocalStack is a Python application distributed as a Docker image
    (~1.2 GB); some services rely on Java tooling — e.g. DynamoDB is "powered by
    DynamoDB Local," AWS's emulator, which requires a Java Runtime Environment.
    See [LocalStack](https://github.com/localstack/localstack) and
    [DynamoDB Local](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/DynamoDBLocal.html).
