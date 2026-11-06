---
title: "Alternatives, honestly"
description: What docker-compose, Testcontainers, LocalStack, Nix, and the rest are genuinely good at — and when to pick them over doze.
---

Tools earn trust by being honest about their neighbors. Here's the landscape as
we see it — including when you should **not** pick doze.

| | Best at | Reach for it when | doze's edge over it |
|---|---|---|---|
| **docker-compose** | Declaring multi-service stacks portably | Your stack must mirror production containers exactly | Native processes (debuggers, no VM tax), lazy boot/sleep, pinned real binaries, typed config |
| **Testcontainers** | Ephemeral per-test services in code | Tests must own service lifecycle programmatically, per language | One shared declarative stack for dev *and* tests; instant warm boots; no Docker daemon required |
| **LocalStack** | Broad AWS API surface in one box | You need many AWS services (Lambda, DynamoDB, …) | Real engines for data stores; S3/SQS/SNS in pure Go, ~instant, no Docker/JVM/Python |
| **Nix / devenv / Flox** | Reproducible toolchains & environments | You want the *whole* environment (compilers, tools) pinned | Radically smaller learning curve; services that sleep; one lockfile scoped to backing services |
| **Tilt / Skaffold** | Dev loops on Kubernetes | Your inner loop is genuinely k8s | Not being Kubernetes for a laptop CRUD app |
| **brew services / systemd** | System-global daemons | One global Postgres forever is fine for you | Per-project versions & data, no port fights, no always-on drain, a lockfile |

## The longer takes

**docker-compose** is the incumbent and a fine one: one file, `docker compose
up`, portable anywhere Docker runs. Choose it when dev/prod container parity is
the priority — when the container *is* the artifact you ship. Its costs are the
VM on Mac/Windows (RAM held all day, bind-mount I/O, emulation for stray
amd64-only images), the always-on whole-stack model, and YAML with string
conventions where references should be. If your compose file is mostly "a
postgres, a redis, a bucket" — the common case — doze replaces it with less
machinery than it took to keep Docker Desktop updated. There's a
[mental-model mapping](/start/from-docker-compose/) for making the move.

**Testcontainers** is excellent at what it actually is: a *testing* library
that gives each suite programmatic, throwaway containers. Choose it when tests
must create bespoke topologies at runtime. As a *dev environment* it's the
wrong shape — nothing to `psql` into between runs, per-language APIs, container
startup in every suite. doze inverts it: one declared stack, always a
connection away, warm across runs (`doze run -- go test ./...`), shared with
your editor and your teammates via two committed files. Many teams run doze for
dev + CI services and keep Testcontainers for the exotic per-test cases.

**LocalStack** has admirable breadth — dozens of AWS APIs, one endpoint. Choose
it when your app leans on services doze doesn't provide (DynamoDB, Lambda,
Step Functions). Its weight is the tradeoff: a ~1.2 GB image, Python + JVM
inside Docker, and *reimplemented* APIs whose fidelity you verify against a
coverage matrix. doze covers the three services most local dev actually
touches — S3, SQS, SNS — as built-in pure-Go engines that boot in milliseconds,
and runs real binaries for everything stateful.

**Nix (and devenv, Flox)** solves a bigger problem than doze does: the entire
environment, hermetically. If your team already speaks Nix, `services.postgres`
in devenv is genuinely good, and doze won't out-reproduce it. The honest
differences: the learning cliff (Nix is a language, an ecosystem, and a
worldview), and services that are supervised-while-the-shell-lives rather than
doze's boot-on-connect/sleep-when-idle. doze is the 90% of the value for 2% of
the onboarding.

**Tilt/Skaffold** answer "how do I develop *on Kubernetes*" — if that's your
question, doze isn't. doze's bet is that for most application work, Kubernetes
on a laptop is complexity imported from production for no local benefit.

**brew services** (or systemd units) is the zero-tooling answer and the one
doze most sympathizes with — it's also native processes! What it lacks is
everything around the process: per-project versions (one global 5432), pinning
(whatever brew last upgraded to), scoped data dirs, idle shutdown, and a
declarative file your teammate can clone. doze is brew-services with the
missing management layer.

## When not to use doze

Plainly: **production** (single instances, no HA, idle sleep — by design);
**Windows without WSL2**; **apps needing AWS beyond S3/SQS/SNS** (pair doze
with LocalStack, or wait for modules); **teams whose dev loop is contractually
k8s**. And if your whole company happily runs Nix — carry on; you have our
respect.
