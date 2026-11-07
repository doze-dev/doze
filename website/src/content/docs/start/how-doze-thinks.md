---
title: How doze thinks
description: The four kinds of block, sleep and wake, names, and start order, in one page.
redirect_from: ["/start/concepts/"]
---

After this page you can read any `doze.hcl` and guess what doze will do with it.

doze starts a service when something connects to it, and puts it to sleep when
nothing does. Everything else follows from that, and from one file,
`doze.hcl`, which says what a project runs. A project with a `doze.hcl` is a
**stack**.

## Four kinds of block

```hcl
stack = "larder"

app "catalog" {                        # your code
  run   = "go run ./services/catalog"
  port  = 8002
  after = [task.migrate-catalog]
  env   = { DATABASE_URL = module.postgres.url }
}

module postgres "postgres" {           # from the doze registry
  version = 18
}

service "mailpit" {                    # anything else
  run  = "mailpit --smtp $HOST:$PORT"
  port = 2525
}

task "migrate-catalog" {               # runs once, then gets out of the way
  run    = "go run ./platform/cmd/migrate services/catalog/migrations"
  inputs = ["services/catalog/migrations/*.sql"]
}
```

These are cut down from Larder, the made-up grocery service the docs use.

- **`app`** is your own code: the command that runs it, and its port. doze runs
  it as a plain process on your machine, so your debugger attaches as usual.
- **`module`** is software from the [doze registry](https://registry.doze-dev.com):
  Postgres, Valkey, Kvrocks, FerretDB, MariaDB, Temporal, Kafka, and AWS. The
  `aws` module is one service with S3, SQS, SNS, DynamoDB, Lambda, EventBridge,
  KMS, SSM, Secrets Manager, STS and Step Functions. `version` names the
  engine; doze fetches the real binaries, checks them and pins them in
  `doze.lock`.
- **`service`** is anything the registry does not have: a binary on your
  `PATH`, a download, or a container. See [Your own services](/hcl/your-own-services/).
- **`task`** runs to the end and stops: migrations, installs, seeds. With
  `inputs`, it runs again only when those files change; with `manual = true`,
  only when you ask. See [Tasks](/hcl/tasks/).

## Sleep and wake

A module or a service with a port sleeps when nothing has been connected for
two minutes. Its process stops; its data stays on disk. The next connection
wakes it, and the connection waits until it is ready.

An app with a port sleeps the same way, and a request wakes it. An app with no
port, such as a worker, runs until you stop it, since no request could wake
it. `sleep = "10m"` on a block changes its time; `sleep = "never"` keeps it up.
See [Sleep and wake](/hcl/sleep/).

What doze counts is connections, not queries. A connection pool that holds an
idle connection open keeps its service awake.

Each service is in one of these states:

| State | Means |
|---|---|
| awake | running, and something used it lately |
| dozing | running and quiet; it sleeps when its time runs out |
| waking | starting, because something asked for it |
| asleep | not running, data kept; the next connection wakes it |
| crashing | exits soon after starting; doze tries again, a few times |
| failed | doze stopped trying; it waits for you |
| stopped | the whole stack is down |

## Names

With `defaults { names = true }`, every service gets a name of its own:
`<name>.<stack>.doze`. Larder's Postgres is `postgres.larder.doze:5432`, and its
catalog app is `catalog.larder.doze:8002`. `stack` sets the middle part; it is
the folder's name if you leave it out.

Each name has its own loopback address, so two stacks can both have a Postgres
on 5432 at once. Names need the one-time setup from
[Install the app](/start/install/#first-launch). See
[Names and addresses](/hcl/names/).

## Order

A reference to another block is also an order. `env = { DATABASE_URL =
module.db.url }` means: start `db` first, wait until it is ready, then pass its
URL. When there is no value to pass, say the order with `after`:

```hcl
app "orders" {
  run   = "go run ./services/orders"
  after = [task.migrate-orders]
}
```

A bare reference in `after` waits until that block is ready (or, for a task,
has run). `started(app.x)` waits only until it runs, and `lazy(module.x)` orders
without starting it. See [Start order and readiness](/hcl/start-order/).

## The app and the command line

The app and the `doze` command are one binary and see the same stacks. Every
switch in the app is a command: the switch on a service is `doze wake` and
`doze sleep`, the stack's switch is `doze up` and `doze down`. Quitting the app
leaves every stack running.

Next: [Library and stacks](/app/library/).
