---
title: "Why doze"
description: The problem doze solves, what it does instead of a VM or always-on services, and when it is the wrong tool.
---

You'll know what doze is for, what it costs, and when to use something else.

## The problem

Your app needs a database, probably a cache, maybe a queue, a bucket, a
workflow engine. None of that is your app, but you have to run it on your
machine to get work done. And your app is rarely one process: it is an API, a
worker, a front end, and a few services you inherited, which must start in the
right order and find each other.

There are three usual ways, and each is a good tool with a cost for this job:

- **docker-compose.** One file, works anywhere Docker runs. On a Mac it needs
  a Linux VM that holds a share of your memory all day, whether or not a
  container is up.[^docker-mem] It starts the whole file even when you work on
  one service, and your own code runs across a VM boundary from your debugger.
- **Installing each engine.** Fast and native, but each project wants a
  different version, two projects fight over 5432, and `brew services` keeps
  everything running from login until you remember to stop it.
- **LocalStack for AWS.** A lot of AWS in one place, as a large Docker image,
  so you are back to the VM.[^localstack]

The cost in each case is the same: you pay for infrastructure all day, in
memory, battery and setup, whether you are using it this minute or not.

## What doze does instead

doze runs each service as a native process, and only while something uses it.

- **One file.** `doze.hcl` lists your apps, the shared services they use
  (Postgres, MariaDB, Valkey, Kvrocks, FerretDB, Temporal, Kafka, AWS), any
  other software you run as your own `service`, and tasks such as migrations.
  References between blocks are the start order.
- **Asleep until used.** A service starts on its first connection and goes to
  sleep after two minutes with none. Its data stays on disk. Declaring ten
  services costs little until you touch one.
- **Real software.** Postgres, MariaDB, Valkey, Kvrocks, FerretDB and Temporal
  are their upstream programs, built from source and pinned by checksum. Kafka
  and AWS are doze's own servers, [doze Kafka](https://doze-dev.com/docs/kafka/)
  and [doze AWS](https://doze-dev.com/docs/aws/): native programs that speak
  the wire protocols, so unmodified clients work.
- **Pinned.** `doze.lock` pins every binary, so your machine, your teammate's
  and CI run the same bytes.
- **Names.** Each service gets `<name>.<stack>.doze`, and on a Mac an address of its own, so two
  stacks can both have a Postgres on 5432.
- **An app to see it all.** Every stack on the machine in one window, with a
  switch per service, logs, a map of who calls whom, and a bat in the menu
  bar. Every action is also a command, for scripts and CI.

| | docker-compose | installed engines | LocalStack | doze |
|---|---|---|---|---|
| Setup | a compose file | by hand, per machine | a container | one `doze.hcl` |
| While idle | a VM and every container | every engine, from login | Docker and the image | a daemon and one plugin per module type; services asleep |
| Only what you use runs | no | no | no | yes: a service starts on connect |
| Pinned versions | image tags | none | an image tag | `doze.lock`, exact builds |
| On Apple Silicon | images may be emulated | native | in the VM | native |

What a stack costs, measured: [Memory](/guides/memory/).

## Your own services too

doze is not only for databases. Larder, the made-up grocery service in these
docs, is ten services in Go, Python and TypeScript, started in order after
their migrations, beside Postgres, Valkey, Kafka and AWS. Because they are
native processes, your debugger attaches, your file watcher reloads, and their
logs are files on your disk. See [Microservices](/guides/microservices/).

## When doze is the wrong tool

- **Production.** One copy of each service, no replication or failover, and
  services that sleep. Use managed databases and real AWS.
- **Your app must match a Linux production userland exactly.** Run the app in
  a container or in CI; doze can still run what it talks to.
- **Your team's inner loop is Kubernetes.** doze does not try to replace it.
- **Windows or an Intel Mac.** There is no build for them.

More in [Native processes vs containers](/why/not-containers/) and
[Alternatives](/why/alternatives/). To start, see
[Install the app](/start/install/).

[^docker-mem]: Docker Desktop gives its Linux VM a share of host memory by
    default, and the VM runs with no containers up; the "Resource Saver"
    feature exists to reclaim that idle memory. See
    [Docker Desktop settings](https://docs.docker.com/desktop/settings-and-maintenance/settings/).

[^localstack]: LocalStack is a Python application shipped as a Docker image;
    some services use Java tooling (DynamoDB Local needs a Java runtime). See
    [LocalStack](https://github.com/localstack/localstack) and
    [DynamoDB Local](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/DynamoDBLocal.html).
