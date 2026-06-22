# FAQ

## Is doze production-ready?

No — and it isn't meant to be. doze is for **local development and CI**. Concretely:

- It runs **single instances** — no replication, no high availability, no
  failover.
- Engines are tuned for fast local iteration (e.g. Postgres defaults toward
  speed over crash-safety), and instances **reap when idle**.
- The built-in **S3/SQS/SNS are dev-grade** conveniences, not a substitute for
  real AWS (limited versioning/IAM; single-node semantics).

Use doze to build and test against realistic services locally; use managed
Postgres/Redis and real AWS in production.

## Can I use doze in CI?

Yes — it's a great fit. Either wrap your test command:

```sh
doze run -- go test ./...
```

or start once and reuse across steps:

```sh
doze start && eval "$(doze env)"
./migrate && ./integration-tests
```

Commit `doze.lock` so CI downloads byte-identical binaries, or set
`DOZE_<ENGINE>_BINDIR` to a preinstalled binary to skip downloads entirely.

## Does it run on Windows? In Docker? WSL?

- **macOS and Linux**, on Apple Silicon and x86-64 — natively.
- **WSL2** works (it's Linux).
- **No native Windows.**
- You *can* run doze inside a Linux container, but that somewhat defeats the
  purpose — doze exists so you don't need Docker for local services.

## Will doze conflict with a Postgres/Redis I already have installed?

No. doze runs **its own** downloaded binaries on **its own** ports (the default
base is `127.0.0.1:6432`, not Postgres's `5432`), with data under `~/.doze`. It
never touches a system install. You can run a brew Postgres and doze side by side.

## Can I safely delete `~/.doze`?

Yes. It holds downloaded engine toolchains (re-downloaded on next use) and
per-project data. Deleting it loses local **data** for your doze projects, but
**not** your `doze.hcl` or `doze.lock` — reconnect and doze rebuilds everything.
To reclaim space more surgically, see
[Resetting & cleaning up](files-and-storage.md#resetting--cleaning-up).

## What is `doze.lock` for? Should I commit it?

Yes, commit it. It's doze's `package-lock.json`: it records the exact engine
version each instance resolved to and its per-platform SHA-256, so every
teammate and your CI download **byte-identical** binaries. See
[Managing binaries](../BINARIES.md).

## How do I share a setup with my team?

Commit `doze.hcl` and `doze.lock`. A teammate clones the repo and runs
`doze run -- …` (or `doze up`) — they get the same engines, versions, databases,
roles, buckets, and queues, with no manual setup. Personal tweaks go in a
gitignored `doze.d/local.hcl` (see
[Files & storage](files-and-storage.md#per-developer-overrides)).

- **docker-compose** runs a heavyweight, always-on stack in Docker. doze runs
  native binaries that sleep when idle — far less RAM, no Docker daemon, instant
  for the service you're actually using.
- **Testcontainers** spins up containers per test run (great for CI, still needs
  Docker). doze's `ephemeral` gives you an isolated, real database per run with no
  container runtime; and doze also serves your everyday dev loop.
- **LocalStack** emulates AWS via Python + a JVM + Docker. doze's S3/SQS/SNS are
  built into one Go binary — no Docker, no JVM.

For the full argument and **measured** numbers, see [Why doze](why-doze.md) and
[Resource footprint](resource-footprint.md).

## How do I uninstall doze?

```sh
rm "$(command -v doze)"     # remove the binary
rm -rf ~/.doze              # remove all cached engines + local data
```

That's it — doze installs nothing else system-wide.

## Where can I see what doze is doing?

`doze status` (snapshot), `doze dash` (live, interactive), and `doze logs`
(daemon) / `doze logs <instance>` (a backend). Run `doze serve` in the foreground
to watch boot and convergence in real time.
