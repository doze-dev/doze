---
title: Your own services
description: Run a program that is not in the registry, such as mailpit, MongoDB or MinIO, as a service in your stack, from your PATH, a container or a download.
---

Run any program your stack needs that the registry does not have, such as a
mail catcher, a search engine or MongoDB itself, as a `service`.

A `service` is a long-running program you didn't write. It works like an
[app](/hcl/apps/): a command, a port, env, readiness, sleep. The difference is
how doze treats it: the app lists it under **Outside**, not with your apps,
and doze can fetch it for you. For Postgres, Valkey, Kafka and the others,
use a [shared service](/hcl/shared-services/) instead: the module sets them
up for you.

## Mailpit, the way Larder runs it

Larder, the made-up grocery shop the docs use as an example, sends its email
to [mailpit](https://mailpit.axllent.org), which catches every message and
shows it in a web page. Mailpit is not in the registry, so Larder downloads it:

```hcl
service "mailpit" {
  download = {
    mac       = "https://github.com/axllent/mailpit/releases/download/v1.21.8/mailpit-darwin-arm64.tar.gz"
    mac_intel = "https://github.com/axllent/mailpit/releases/download/v1.21.8/mailpit-darwin-amd64.tar.gz"
    linux     = "https://github.com/axllent/mailpit/releases/download/v1.21.8/mailpit-linux-amd64.tar.gz"
    linux_arm = "https://github.com/axllent/mailpit/releases/download/v1.21.8/mailpit-linux-arm64.tar.gz"
  }
  run     = "./mailpit --smtp $HOST:$PORT --listen 127.0.0.1:8025 --database $DATA/mail.db"
  port    = 2525
  console = "http://127.0.0.1:8025"
}

app "notifications" {
  dir = "services/notifications"
  run = "bun src/index.ts"
  env = { SMTP_ADDR = service.mailpit.address }
}
```

Line by line:

- `download` is the archive to run from: one URL, or one per platform
  (`mac`, `mac_intel`, `linux`, `linux_arm`). doze fetches the one for this
  machine once, unpacks it, and runs `run` from inside it. That is why the
  command is `./mailpit`.
- `run` uses three variables doze sets: `$HOST` and `$PORT`, the private
  address to listen on, and `$DATA`, a folder of the service's own that
  survives restarts and sleep.
- `port` is the port everyone else uses. doze holds it, so the first
  connection wakes mailpit and it sleeps when no one is connected, like an
  app.
- `console` is a web page the service serves on a second port. The app shows
  it in the service's **Console** tab.
- `service.mailpit.address` is how `notifications` finds it; the reference also
  makes `notifications` wait until mailpit is ready.

## Three ways to run one

`doze add` writes a `service` block for a name that is not a registry module,
and picks the first way that works on your machine:

```sh
doze add mongo
# → mongo is not a doze module — adding it as a service
#   mongod is not installed — the block runs it as a container
#   other ways: --as installed · --as container · --as download
```

Choose one with `--as`, and see the block first with `--dry-run`:

```sh
doze add mongo --as installed --dry-run
doze add mongo --as container --dry-run
doze add mailpit --as download --dry-run
```

The app has no add flow for a stack that already has a doze.hcl; use
`doze add` or edit the file.

### `--as installed`: a program on your PATH

```hcl
service "mongo" {
  run  = "mongod --dbpath $DATA --bind_ip $HOST --port $PORT"
  port = 27017
}
```

The quickest, if it is installed. Each teammate needs it installed too, and
their version is whatever they have.

### `--as container`: a container image

```hcl
service "mongo" {
  # a container: a container is just a command, and `run` stays in the foreground
  run  = "docker run --rm -p $HOST:$PORT:27017 mongo:7"
  port = 27017
}
```

A container is just a command to doze. Keep it in the foreground (no `-d`),
publish the container's port on `$HOST:$PORT`, and doze starts, stops and
sleeps it like anything else. You need Docker or a compatible runtime.

### `--as download`: an archive doze fetches

```hcl
service "mailpit" {
  # fetched once, pinned in doze.lock by its checksum, and run from the archive
  download = "https://example.com/mailpit-darwin-arm64.tar.gz" # fill this in
  run      = "./mailpit --port $PORT"
  port     = 1025
}
```

Fill in the real URL (or a map of them, as Larder does) and the command's
flags. Nothing to install, and every teammate runs the same file.

## Downloads are pinned

The first time a URL is fetched, its sha256 goes into the `downloads` part of
`doze.lock`. Every later fetch, on any machine, must match it:

```text
checksum mismatch for https://…/mailpit-darwin-arm64.tar.gz: doze.lock pins sha256:…, the server sent sha256:… — nothing was unpacked
```

Each URL is pinned when a machine of that platform first fetches it, so the
lock gains the Linux URL's checksum when a Linux machine (or CI) first runs
the stack. Commit `doze.lock` after it changes.

doze unpacks `.tar.gz` and `.zip` archives; any other file is taken as the
program itself and made executable. When an archive holds a single folder,
`run` starts inside that folder. Set `dir` to start somewhere else in the
archive.

## What a service can be referenced by

| Reference | Value |
|---|---|
| `service.<name>.address` | host and port, e.g. `mailpit.larder.doze:2525` |
| `service.<name>.host` | the host alone |
| `service.<name>.port` | the port alone |
| `service.<name>.data` | its data folder |

A service has no `url`: build one around `address` if a client wants a URL.

## Sleep

A service with a `port` sleeps two minutes after its last connection closes,
and wakes on the next one. A service with no port runs until it is stopped.
`sleep` changes either: see [Sleep and wake](/hcl/sleep/).
