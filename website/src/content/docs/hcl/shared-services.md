---
title: Shared services
description: Add Postgres, Valkey, Kafka, AWS and the other registry modules to a stack, choose their versions, and keep them pinned.
redirect_from:
  - /guides/modules/
  - /guides/engines/
---

Add a database, a cache, a broker or local AWS to your stack as a `module`,
choose its version, and keep every teammate on the same build.

## Add one

```hcl
module postgres "db" {
  version = 18
}
```

The first word after `module` is the module, from the doze registry; the
label is your name for this one. A stack can have several of the same module
(`module postgres "db"` and `module postgres "legacy"`), each its own server
with its own data.

`doze add` writes the block for you, beside what is already in doze.hcl, and
prints the line that wires it into an app:

```sh
doze add postgres
# → module postgres "db"
#   module postgres "db" {
#     version = 18
#     port    = 5432
#   }
#
#   use it from an app:  DATABASE_URL = module.db.url
doze add valkey cache --dry-run     # show the block, write nothing
```

The app offers the same for a folder that has no doze.hcl yet ("Add one
thing"); in a stack that has one, add blocks in the file or with `doze add`.

The first `doze up` after you add a module fetches it, checks its signature,
and pins it in `doze.lock`.

## What the registry has

| Module | Speaks | Use it as | `version =` |
|---|---|---|---|
| [`postgres`](/services/postgres/) | Postgres | your SQL database | 14 – 18 |
| [`valkey`](/services/valkey/) | Redis | an in-memory cache | 8 – 9 |
| [`kvrocks`](/services/kvrocks/) | Redis | a key-value store on disk | 2 |
| [`ferret`](/services/ferretdb/) | MongoDB | a document database | 2 |
| [`mariadb`](/services/mariadb/) | MySQL | a MySQL-compatible database | 11 |
| [`temporal`](/services/temporal/) | Temporal gRPC | a workflow engine | 1.1 |
| [`kafka`](/services/kafka/) | Kafka | an event log | 1 – 4 (protocol) |
| [`aws`](/services/aws/) | the AWS APIs | S3, SQS, SNS, DynamoDB, Lambda and more, as one service | none |

Each page has the module's every field, the exact builds you can declare, and
recipes. Each one runs on macOS (Apple Silicon) and Linux (x86_64 and arm64).

Several are chosen as open-source stand-ins for software whose licence or
weight makes it awkward to run yourself: Valkey for Redis, FerretDB for
MongoDB, MariaDB for MySQL, and Go implementations of Kafka and AWS instead of
a JVM or a Docker image. Each page says why.

Something not in the list runs as [your own service](/hcl/your-own-services/):
any program, from your PATH, a download or a container. To get a module into
the registry, [propose it](/contribute/propose/).

## The one version that is yours

`version` is the engine's own version: the Postgres major, the Valkey major,
the Kafka protocol level.

```hcl
module postgres "db" { version = 18 }
module postgres "pinned" { version = "18.6" }
```

A major (`18`) gets the newest build of it the first time, and `doze.lock`
keeps that build until you change it. A quoted exact version (`"18.6"`) asks
for that build.

Behind the engine sits the module: the plugin that runs it, with its own
release number (`0.2.4`). doze picks the newest release that supports your
doze and the versions you declared, pins it in `doze.lock`, and never moves it
on its own. You never write a module release.

## doze.lock

`doze.lock` sits beside doze.hcl. Commit it. It holds:

- `engines`: each engine version you declared, the exact build it resolved to,
  and its checksum per platform;
- `modules`: each module's pinned release and its checksums;
- `downloads`: the checksum of each file a [`service`](/hcl/your-own-services/)
  downloads;
- a `doze:` line with the newest doze that wrote it.

A teammate who clones the repo gets the same builds, checked against the same
checksums. With everything cached, nothing touches the network.

## Upgrading

```sh
doze modules upgrade --check   # anything newer that fits? exit 1 if so
doze modules upgrade           # move the pins; commit doze.lock
doze modules upgrade postgres  # just one
```

doze tells you when you need it. Declare a major the pinned module does not
support and the error names the command:

```text
postgres 19 needs a newer doze/postgres module: pinned 0.2.1 supports
14, 15, 16, 17, 18 — run 'doze modules upgrade postgres'
```

Moving a Postgres server's data to a new major is a separate step:
[Postgres major upgrades](/guides/postgres-upgrades/).

## Where modules come from

Every module comes from the doze registry, signed. The index that picks a
release is signed with ed25519, so is every archive's checksum, and the key
they are checked against is built into doze. A tampered or unsigned module is
refused. There is no setting that points doze at another registry. The full
story is in [The trust model](/why/trust/).

`DOZE_MODULES=off` turns fetching off, for a machine with no network: apps
and services still run.

## Settings every module takes

Besides its own fields, every `module` block takes doze's: `port`, `listen`,
`sleep`, `after`, `enabled`, `routes`, `count` and `for_each`. A module listens
on its engine's usual port (5432, 6379, …) unless you set `port`.
