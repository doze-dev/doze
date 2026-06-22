# Why doze?

You're building an app. It needs a database. Probably a cache too, maybe a queue,
a bucket, a document store. None of that is your app — it's the *infrastructure
your app talks to* — but you still have to run it on your laptop to get any work
done.

There are three usual ways to do that, and each one taxes you for the privilege.

## The three bad options

### 1. Docker / docker-compose

You write a `docker-compose.yml`, copy it to the next project, tweak it forever.
Then a Linux VM runs on your Mac all day. Docker Desktop reserves **half your
machine's RAM** for that VM by default — 8 GB on a 16 GB laptop — and it keeps
running whether or not you have a single container up.[^docker-mem] On Apple
Silicon, any image without an ARM build runs under **emulation**, which is
slower and spins the fan.[^docker-emu] And it's all-or-nothing: `docker compose
up` starts the *entire* stack — five services — even when you're only touching
one.

It works. It's just heavy, and it's always on.

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

Your app uses S3 and SQS, so you add LocalStack. It's a ~1.2 GB Docker image
running a Python application, and several services delegate to **Java** emulators
under the hood (DynamoDB Local is AWS's JRE-based tool).[^localstack] So now
you're back to option 1 — Docker, plus Python, plus a JVM — just to fake a bucket.

---

Every option forces the same trade: **to run real-enough infrastructure, you pay
for it continuously** — in RAM, in fan noise, in battery, in version chaos, in
ceremony — whether you're using it this second or not.

## What doze does instead

doze keeps the good part of every option and drops the tax.

> **Real engines that sleep.** doze runs the *actual* PostgreSQL, Valkey,
> Kvrocks, and FerretDB binaries — plus pure-Go S3, SQS, and SNS — but only while
> a client is connected. Declare what you need in one file; doze fetches pinned
> binaries, boots each engine on first connect, splices your connection straight
> through, and returns the RAM the moment you walk away.

Concretely:

- **Idle is nearly free.** When nothing is connected, the only thing running is
  one small daemon — about **15 MB of RAM**, with **zero engine processes**. No
  VM, no Docker, no JVM. Your laptop is quiet. (See the
  [measured footprint](resource-footprint.md).)
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
- **FerretDB** gives you a MongoDB-compatible document store on top of Postgres —
  "Mongo" locally without MongoDB's restrictive license.

The full story, with the licensing and cost details, is in **[The
engines](engines.md)**.

## Is doze for you?

**Yes, if** you develop locally, run automated tests, or build CI pipelines and
you want real infrastructure without the always-on weight. If you've ever run
`docker compose up` just to work on one service, doze is for you.

**No, if** you need production infrastructure. doze runs **single** local
instances — no replication, no HA, no failover — tuned toward fast iteration over
durability, and it reaps them when idle. It's not a place to keep data you can't
lose, and its S3/SQS/SNS are dev-grade conveniences, not a substitute for real
AWS. Use managed databases and real AWS in production. (Full rationale in the
[FAQ](faq.md#is-doze-production-ready).)

**Platforms:** macOS and Linux, on Apple Silicon and x86-64. No native Windows
(WSL2 works).

---

**Next:** see the numbers in **[Resource footprint](resource-footprint.md)**, meet
the engines in **[The engines](engines.md)**, or just start building with
**[Getting started](getting-started.md)**.

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
