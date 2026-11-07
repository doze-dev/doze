---
title: "FAQ"
description: Short answers to the questions people ask about doze, with links to the pages that go further.
---

Short answers, each with the page that says more.

## Is doze for production?

No. doze is for your machine and for CI. It runs one copy of each service, with
no replication or failover, settings chosen for quick starts, and it puts
services to sleep when nobody uses them. The local AWS is for development too.
In production, use managed databases and real AWS.

## Which machines does it run on?

Macs with Apple Silicon, and Linux on x86-64 and arm64. There is no build for
Intel Macs or for Windows. See [Install the app](/start/install/) and
[Linux](/guides/linux/).

## Do I need Docker?

No. doze runs each engine as a native process from binaries it downloads and
checks. A service that is only shipped as a container image can still run in a
container, as your own `service`; see
[Your own services](/hcl/your-own-services/).

## Can I use it in CI?

Yes. Wrap your test command, or bring the stack up once and run several steps
against it:

```sh
doze run --env -- go test ./...
```

```sh
doze up --all
./integration-tests
doze down
```

Commit `doze.lock`, so CI downloads the same binaries you run. In CI you
usually want names off (`DOZE_ZONE=off`) and services on `127.0.0.1` at their
ports. See [Teams and CI](/hcl/teams-and-ci/).

## Will it fight with a Postgres or Redis I already have?

doze runs its own binaries and keeps its data under `~/.doze`; it never
touches a system install. The one thing two programs can share is a port. If
something else holds the port a service wants, the app names the process and
offers to stop it or to use another port. See
[Troubleshooting](/guides/troubleshooting/#a-port-is-taken).

## Why did my database go to sleep?

Nothing was connected to it for two minutes. Its data stays on disk, and the
next connection wakes it. Set `sleep` on its block for a longer time or
`"never"`, or pin it for now. See [Sleep and wake](/hcl/sleep/).

## What is in doze.lock? Should I commit it?

Commit it. It pins the exact build of each engine version, the release of each
module, and the checksum of anything a `service` downloads, so every teammate
and CI run the same bytes. A `doze:` line at the top records the newest doze
that wrote it. See [Files and storage](/guides/files-and-storage/#dozelock).

## What is the difference between `version = 18` and a module version?

`version = 18` is the engine: the Postgres your app talks to. It is the only
version you write. The module is the plugin that runs that engine; it has its
own releases, which doze picks for you and pins in `doze.lock`.

## Will a module update change things under me?

No. The lock wins, offline too. Pins move only when you run
`doze modules upgrade`, and the new lock goes through review like any change.
If your config needs a newer module (an engine version or an argument your
pinned release does not know), the error says which command to run.

## Where do modules come from?

From the doze registry only, signed with a key compiled into doze. Software
that is not in the registry runs as your own `service`. To add a module to the
registry, see [Propose a module](/contribute/propose/). How the signing works
is in [The trust model](/why/trust/).

## How do I share a setup with my team?

Commit `doze.hcl` and `doze.lock`. A teammate clones the repository and brings
the stack up; they get the same engines, versions, databases, roles, buckets,
queues and topics. Personal changes go in a gitignored override file; see
[Splitting files](/hcl/splitting-files/).

## How is it different from docker-compose, Testcontainers or LocalStack?

- **docker-compose** runs every service in the file, all the time, inside a
  Linux VM on a Mac. doze runs native processes and starts each one when
  something connects.
- **Testcontainers** starts containers from inside each test run. doze keeps
  one declared stack that your editor, your tests and your teammates share;
  `doze reset <name>` gives a test run a clean database.
- **LocalStack** is a large set of AWS services in a Docker image. doze's `aws`
  module is one native process with S3, SQS, SNS, DynamoDB, Lambda,
  EventBridge, KMS, SSM, Secrets Manager, STS and Step Functions. It does not
  cover everything LocalStack does; see doze AWS's
  [API support](https://doze-dev.com/docs/aws/support/).

More in [Alternatives](/why/alternatives/).

## Can I safely delete `~/.doze`?

Yes, but it holds your local databases, so their data goes with it. Engines
are downloaded again on next use, and your `doze.hcl` and `doze.lock` are not
touched. To free space more carefully, see
[Files and storage](/guides/files-and-storage/#freeing-space).

## How do I see what doze is doing?

The app: each stack's page, a service's page with why it is awake, and
Activity, which shows the last hour, day or week from the stack's journal. In
a terminal: `doze status` (`-w` to keep it on screen), `doze logs`, and
`doze up -f` to follow the logs as the stack comes up.

## How do I uninstall doze?

Undo the machine setup, remove the app, then delete its folder:

```sh
doze trust --remove        # in each project that used domains
doze dns-setup --uninstall
brew uninstall --cask doze # or: sudo apt remove doze, sudo dnf remove doze
rm -rf ~/.doze
```

`doze dns-setup --uninstall` removes the resolver route, the network daemon and
doze's certificate authority. See [Files and storage](/guides/files-and-storage/#removing-everything).
