---
title: "Alternatives"
description: What docker-compose, Testcontainers, LocalStack, Nix, Tilt and brew services are good at, and when to pick them over doze.
---

You'll know which tool fits your situation, including when it is not doze.

| | Best at | Pick it when | Where doze differs |
|---|---|---|---|
| **docker-compose** | Declaring a multi-service stack that runs anywhere Docker does | Your local stack must match production containers exactly | Native processes (debuggers attach, no VM), services that start on connect and sleep, pinned binaries, typed config |
| **Testcontainers** | Throwaway services created from test code | Tests must build their own topology while they run | One declared stack shared by your editor, tests and team; no container runtime |
| **LocalStack** | A very wide AWS surface in one place | You need AWS services or features doze AWS does not have | One native process for S3, SQS, SNS, DynamoDB, Lambda, EventBridge, KMS, SSM, Secrets Manager, STS and Step Functions; no Docker |
| **Nix, devenv, Flox** | Reproducible toolchains and whole environments | You want compilers and tools pinned too | Far less to learn; services that sleep; a lockfile for what the stack runs |
| **Tilt, Skaffold** | Development loops on Kubernetes | Your inner loop is Kubernetes | Not Kubernetes |
| **brew services, systemd** | One system-wide daemon per engine | One global Postgres forever suits you | A version and data per project, no port fights, nothing running when idle, a lockfile |

## docker-compose

The incumbent, and a good one: one file, `docker compose up`, works anywhere
Docker runs. Choose it when the container is what you ship and the local stack
must match it. Its costs on a Mac are the VM (memory held all day, slower file
sharing, emulation for images without an arm64 build), a stack that runs
whole the entire time, and YAML where references should be values. If your
compose file is mostly a Postgres, a Redis and a bucket, doze does that with
less machinery. See [Bring your own repo](/start/your-repo/) for moving
across.

## Testcontainers

A testing library that gives each suite containers it creates and throws away.
Choose it when tests must build unusual topologies at run time. As a
development environment it is the wrong shape: nothing to connect to between
runs, an API per language, and container start-up in every suite. doze keeps
one declared stack, a connection away and warm across runs
(`doze run --env -- go test ./...`), with `doze reset <name>` for a clean
database. Many teams use doze for development and CI services and keep
Testcontainers for the odd test that needs its own.

## LocalStack

LocalStack covers a very wide range of AWS services and features, as a large
Docker image running a Python application, with some services using Java
tooling.[^localstack] Choose it when your app needs an AWS service or an
operation that doze AWS does not have.

doze's `aws` module runs [doze AWS](https://doze-dev.com/docs/aws/), one
native process behind one endpoint. Inside a doze stack you declare buckets,
queues, topics, DynamoDB tables, Lambda functions, EventBridge rules, KMS keys,
secrets and SSM parameters as blocks, and it also serves STS and Step
Functions. It is not everything AWS or LocalStack offers: each service lists
which operations work, which are accepted without effect, and which are
refused, in doze AWS's [API support](https://doze-dev.com/docs/aws/support/).
Check that list for the calls your app makes.

## Nix, devenv and Flox

They solve a bigger problem than doze: the whole environment, hermetically. If
your team already uses Nix, `services.postgres` in devenv is good, and doze will
not out-reproduce it. The differences: Nix is a language and an ecosystem to
learn, and its services run while the shell lives rather than starting on a
connection and sleeping when idle.

## Tilt and Skaffold

They answer "how do I develop on Kubernetes". If that is your question, doze is
not the answer. doze's bet is that for most application work, Kubernetes on a
laptop is production complexity with no local benefit.

## brew services and systemd

The no-tooling answer, and the closest to doze: native processes too. What it
lacks is everything around the process: a version per project (one global
5432), pinning (whatever the last upgrade installed), data per project,
sleeping when idle, and a file a teammate can clone.

## When not to use doze

- **Production.** One copy of each service, no failover, services that sleep.
- **Windows or an Intel Mac.** There is no build.
- **AWS beyond what doze AWS supports.** Check its
  [API support](https://doze-dev.com/docs/aws/support/) first.
- **A team whose development loop is Kubernetes.**

And if your whole company already runs on Nix, carry on.

[^localstack]: See [LocalStack](https://github.com/localstack/localstack) and
    [DynamoDB Local](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/DynamoDBLocal.html),
    which needs a Java runtime.
