# Getting started

From zero to a running Postgres (plus a cache) in about five minutes. Every
command below is real — copy them as-is.

> **Platforms:** macOS (Apple Silicon) and Linux (arm64 / x86-64). No native
> Windows yet (use WSL2).

## 1. Install

```sh
brew install doze-dev/tap/doze
# or: mise use -g ubi:doze-dev/doze
# or: grab a binary from the releases page and put it on your PATH
```

Check it:

```sh
doze version
```

## 2. Describe what you need

```sh
doze init
```

`doze init` scaffolds a `doze.hcl` in the current directory (interactively when
run in a terminal; a starter file otherwise). A minimal one looks like this —
each block is one instance, and the `port` is the stable address your app
connects to:

```hcl
postgres "app" {
  port     = 5432
  database = "app"
  user     = "app"
  password = "app"
}

valkey "cache" {
  port = 6379
}
```

You never start these by hand — doze boots each engine the first time something
connects, and reaps it back to zero after a few idle minutes.

## 3. Bring it up

```sh
doze up          # auto-starts the daemon; engines stay cold until first use
doze status      # what's declared, what's awake, and each endpoint
```

`doze status` shows the whole stack at a glance:

```
  NAME      ENGINE        STATE    ENDPOINT         CONNS   MEM     CPU
  ● app     postgres 16   asleep   127.0.0.1:5432   -       -       -
  ○ cache   valkey 9      asleep   127.0.0.1:6379   -       -       -
```

Both are declared and addressable, but nothing is running yet — that's the
point.

## 4. Connect

The ports you declared are stable, so you can connect straight away — the first
connection boots the engine and converges it to your declared shape (database,
roles, schemas, extensions):

```sh
eval "$(doze env)"       # export DATABASE_URL, REDIS_URL, AWS_ENDPOINT_URL_*, …
psql "$DATABASE_URL"     # cold-boots `app`; the next connection is instant
```

Or point your app's config at the stable URL directly
(`postgresql://app:app@127.0.0.1:5432/app`) and just run it:

```sh
doze run -- <your app command>   # ensures the backends are up, then runs it
```

## The daily loop

- **`doze up` / `doze down`** — bring the stack up (daemon + declared instances) or
  put it all to sleep and stop the daemon.
- **`doze status`** (aka `tree`, `ls`, `ps`) — live view; works even when the daemon
  is down (shows the declared stack).
- **`doze env`** — connection variables for your shell (`eval "$(doze env)"`), with
  `--json` / `--dotenv` variants.
- **`doze sync`** — after editing `doze.hcl`, reconcile the running stack to match
  (Terraform-style `+ / ~ / -` plan; `--dry-run` to preview).
- **`doze doctor`** — diagnose config, daemon, and (optional) local-DNS setup.
- **`doze dash`** (or just `doze`) — the full-screen TUI: logs, wake/sleep, and the
  command palette.

## Local AWS (optional)

Declare AWS services the same way — they're real, pure-Go emulators (no Docker,
no LocalStack):

```hcl
s3       "uploads" { port = 9000 }
dynamodb "orders"  { port = 8000  hash_key = "id"  attribute "id" { type = "S" } }
```

`eval "$(doze env)"` then exports `AWS_ENDPOINT_URL_S3`,
`AWS_ENDPOINT_URL_DYNAMODB`, and dummy credentials, so a stock AWS SDK or the
`aws` CLI works against your local stack.

## Shared canonical ports (optional)

Want two Postgres instances both on `:5432`, addressed by name? Enable
`defaults { domains = true }` and run `doze dns-setup` once (it needs `sudo` to
add a loopback range + resolver rule). Then each instance gets a stable
`<name>.<stack>.doze` hostname on its own loopback IP. This is optional — a
single service on a unique port needs none of it.
