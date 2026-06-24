# Resource footprint

doze's whole pitch is that it's light. This page backs that up with numbers — what
doze actually uses, measured, and how that compares to the alternatives.

## The short version

- **Idle, doze is ~15 MB of RAM and zero engine processes.** When nothing is
  connected, the only thing running is one small daemon.
- **Active, you pay for one engine at a time.** A booted Postgres is a single
  native process of a few megabytes to a few tens of megabytes; an idle-but-booted
  Valkey is ~4 MB. Nothing else runs unless you connect to it.
- **Reaped, it's gone.** When an instance goes idle, its process exits and the
  kernel reclaims every byte. Back to ~15 MB.

There is no VM, no container runtime, no JVM, and no per-service "always-on"
overhead anywhere in that picture.

## Measured

The figures below were measured with `ps` on Apple Silicon (macOS, arm64), using
the real engine binaries doze downloads. They'll vary with your hardware, the
engine version, and load — see [how we measured](#how-we-measured) — but the
*shape* (one tiny daemon idle; one native process per active engine; zero when
reaped) is structural, not machine-specific.

| State | What's running | Resident memory |
|---|---|---|
| doze idle | the daemon only (listeners + reaper) | **~15 MB** |
| Postgres booted | daemon + 1 native `postgres` | ~15 MB + **~5 MB** |
| Valkey booted | daemon + 1 native `valkey-server` | ~15 MB + **~4 MB** |
| any engine reaped | daemon only | back to **~15 MB** |

A booted Postgres grows under load toward its `shared_buffers` (16 MB in doze's
dev profile by default), and adds a few small background workers — so call it
"single-digit to low-tens of MB while you're using it, zero when you're not."

**On disk:** engine toolchains are downloaded once and shared across every project
under `~/.doze` (deduplicated). PostgreSQL is ~77 MB of binaries plus a ~39 MB
copy-on-write boot template (created once per version); Valkey and the others are
smaller. You download an engine the first time any project uses it, never again.

## Versus the alternatives

The comparison that matters isn't peak memory under load — every real database
uses memory when it's working. It's **what runs when you're *not* actively using
a service**, which on a dev machine is most of the time.

| | What sits idle in the background |
|---|---|
| **Docker Desktop** | A Linux VM allocated **50% of host RAM** by default (e.g. 8 GB on a 16 GB Mac), running whether or not any container is up.[^docker] |
| **A docker-compose stack** | Every service in the file, up the whole time you've run `docker compose up` — plus the VM above. |
| **`brew services`** | Each installed engine, started at login and idling until you remember to stop it. |
| **LocalStack** | A ~1.2 GB Docker image running a Python app (and a JVM for some services), on top of the Docker VM.[^localstack] |
| **doze** | **One ~15 MB daemon. Zero engine processes.** |

The difference is categorical. Docker reserves **gigabytes** for a VM that's up
all day; doze's idle cost is a **single process smaller than a browser tab**.
When you actually need a database, doze runs *one real native engine* and reaps
it when you're done — so even "active" doze is a handful of megabytes against
Docker's always-resident VM.

A worked example — a typical app needing Postgres + a cache + a bucket:

- **docker-compose + LocalStack:** the Docker VM (gigabytes, always) + 3 service
  containers + the LocalStack image, all running for as long as you're "working
  on the app," even while you read code or eat lunch.
- **doze:** ~15 MB at rest. Open a `psql` shell and one ~5 MB Postgres boots. Run
  your test suite and the cache and bucket boot for the seconds they're used,
  then sleep. Close your laptop and everything is back to ~15 MB.

## Why it's this light

The footprint isn't a tuning trick — it falls out of the architecture
([concepts](concepts.md)):

- **Lazy boot.** Engines don't run until a connection arrives, so declaring ten
  services costs nothing until you use one.
- **One real process per active instance.** doze spawns the genuine engine binary
  as a child process and splices your connection to it — no supervisor pool, no
  per-connection backends, no sidecars.
- **Idle reap.** Zero connections for a few minutes and the process exits; the OS
  returns all of its RAM, file descriptors, and sockets.
- **Pure-Go AWS.** S3/SQS/SNS are compiled into the doze binary and run as
  short-lived child processes — no Docker image, no Python, no JVM.

## How we measured

So you can reproduce or sanity-check the numbers:

```sh
# idle daemon RSS (KB) — nothing connected
doze start
ps -o rss= -p "$(pgrep -f 'doze start --foreground')"

# a booted engine's RSS — boot it, then read its process
doze start app                 # or: doze shell app
doze status                 # the RAM column reports the same figure
ps -Ao rss,command | grep '/bin/postgres'
```

Numbers depend on: CPU architecture and OS, the engine version, your config
(`shared_buffers`, `maxmemory`, etc.), and whether the instance is freshly booted
or under load. The external figures (Docker, LocalStack) are vendor-documented
defaults, linked below — your settings may differ, but the order of magnitude
holds.

---

**Next:** **[The engines](engines.md)** — why doze runs Valkey, Kvrocks, and
FerretDB — or **[Why doze](why-doze.md)** for the full case.

[^docker]: Docker Desktop allocates 50% of host memory to its Linux VM by
    default, and that VM runs even with no containers (the "Resource Saver"
    feature exists to reclaim the idle memory). See
    [Docker Desktop settings](https://docs.docker.com/desktop/settings-and-maintenance/settings/).

[^localstack]: LocalStack is a Python application shipped as a Docker image
    (~1.2 GB); some services use Java tooling (DynamoDB Local requires a JRE).
    See [LocalStack](https://github.com/localstack/localstack) and
    [DynamoDB Local](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/DynamoDBLocal.html).
