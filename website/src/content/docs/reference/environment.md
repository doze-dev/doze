---
title: "Environment"
description: Every environment variable doze reads, and the ones it sets for your commands and prints for your shell.
---

This page lists every environment variable doze reads or sets, so you can look one up.

## Variables doze reads

| Variable | What it does |
|---|---|
| `DOZE_HOME` | The shared home (default `~/.doze`): engine and module cache, per-project state, the certificate authority. |
| `DOZE_VAR_<name>` | Sets `variable "<name>"`. Below `--var` in precedence, above `*.auto.doze.vars` and the default. |
| `DOZE_JSON` | `1` prints every command's result as a JSON envelope, as `--json` does. Commands with no JSON result are left alone. See [Scripting doze](/reference/cli/#scripting-doze). |
| `DOZE_YES` | `1` answers yes to every question, as `-y` does, `doze reset`'s included. |
| `NO_COLOR` | Any value turns colour off, as `--no-color` does. |
| `DOZE_NO_GUI` | Any value stops `doze` and `doze open` from opening a window, even where there is a screen. |
| `DOZE_ZONE` | `off` (or `0`, `false`, `no`) turns `.doze` names off for every doze program, whatever a config says. For CI and containers. |
| `DOZE_MODULES` | `off` stops doze fetching modules. Only apps, services and tasks run. |

### How doze decides there is a screen

`doze` with no command opens the app when there is a screen. On macOS that is
any session that is not SSH (`SSH_CONNECTION` and `SSH_TTY` unset). On Linux it
is a set `DISPLAY` or `WAYLAND_DISPLAY`. `DOZE_NO_GUI` overrides both.

### The terminal dash

Bare `doze` over SSH is the terminal dash. These change how it looks.

| Variable | What it does |
|---|---|
| `DOZE_THEME` | The colour theme: `teal`, `burgundy`, `amber` or `slate`. Wins over the one chosen last. |
| `DOZE_GLYPHS` | The glyph set: `unicode`, `narrow` or `ascii`. By default doze picks from `TERM` and the locale. |
| `DOZE_ASCII` | Any value: ASCII glyphs only. |
| `DOZE_NO_MOUSE` | Any value: the dash leaves the mouse to the terminal, so you can select text. |

### For testing

| Variable | What it does |
|---|---|
| `DOZE_ZONE_RESOLVER` | Move the `.doze` DNS server to this `host:port`. |
| `DOZE_ZONE_INGRESS` / `DOZE_ZONE_INGRESS_TLS` | Move the front door (http and https) to this `host:port`. |

With `DOZE_HOME`, these make a private zone for tests. Every doze program in it
needs the same values.

### Set by doze itself

You should not set these.

| Variable | What it is |
|---|---|
| `DOZE_ADMIN_PROMPT` | The app sets it on the doze commands it runs, so a step that needs root asks with the system's password prompt instead of `sudo`. |
| `DOZE_APP_CHILD` | Marks the app's own child process. |

## Variables doze sets for your commands

Every `app`, `service` and `task` gets the environment doze runs in, then its
`env_file`, then its `env`, each layer winning over the one before.
[`defaults { env }`](/reference/doze-hcl/#defaults) fills in the keys a
block's own `env` does not set. doze adds these:

| Variable | Set for | Value |
|---|---|---|
| `PORT` | an app or service with a `port`, not `direct` | The private port to listen on. doze holds the public `port` and forwards to this one. |
| `HOST` | the same | The address to listen on, `127.0.0.1`. |
| `DATA` | every app and service | A folder of its own that survives restarts. |
| `DOZE_CA_FILE` | every command, when the stack serves a domain from the doze certificate authority | The authority's root certificate. |
| `NODE_EXTRA_CA_CERTS` | the same | The root, which Node adds to its own store. |
| `SSL_CERT_FILE` | the same | A bundle of the machine's roots plus doze's, for OpenSSL, curl and Go on Linux. |
| `REQUESTS_CA_BUNDLE` | the same | The same bundle, for Python requests. |

A block's own `env` wins: if it sets `REQUESTS_CA_BUNDLE` to a company bundle,
doze leaves it. Processes already running when the authority is made keep the
old environment until they restart.

Connection strings are not added. Pass each one by reference:

```hcl
app "api" {
  run  = "go run ./cmd/api"
  port = 8080
  env  = { DATABASE_URL = module.db.url }
}
```

## Variables `doze env` prints

`doze env` prints one variable per service, for your shell; `doze run --env`
runs a command with them set. The same set goes to `.doze/endpoints.yaml`
beside `doze.hcl`, for other tools.

| Variable | For |
|---|---|
| `DATABASE_URL` | `postgres`, `mariadb` |
| `REDIS_URL` | `valkey`, `kvrocks` |
| `MONGODB_URI` | `ferret` |
| `TEMPORAL_ADDRESS` | `temporal` |
| `KAFKA_BROKERS` | `kafka` |
| `AWS_ENDPOINT_URL` | `aws` |
| `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION` | added with `aws`: `doze`, `doze`, `us-east-1`. AWS SDKs refuse to sign without them; doze ignores them. |
| `DOZE_CA_FILE`, `NODE_EXTRA_CA_CERTS`, `SSL_CERT_FILE`, `REQUESTS_CA_BUNDLE` | added when the stack serves a domain from the doze certificate authority |

When two services want the same variable, the first one declared keeps it and
the next gets its name as a suffix: two postgres blocks `db` and `audit` print
`DATABASE_URL` and `DATABASE_URL_AUDIT`. A [service](/hcl/your-own-services/)
prints nothing; reach it with `doze url <name>` or a reference.

## For module contributors

| Variable | What it does |
|---|---|
| `DOZE_<ENGINE>_BINDIR` | Read by the acceptance tests in doze-modules (`DOZE_POSTGRES_BINDIR=… go test -tags acceptance ./modules/postgres/...`): the engine's `bin` folder to test against. doze itself does not read it. See [Test it](/contribute/test/). |
