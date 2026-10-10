# Contract 1: the config language

Tier: core. Built in phase 1. Background in
[gap-analysis.md](gap-analysis.md#config).

## The whole language

```hcl
stack = "shop"

module postgres "db" {
  version = 17
  role "shop" {}
}

service "mongo" {
  download = "https://fastdl.mongodb.org/osx/mongodb-macos-arm64-8.0.4.tgz"
  run      = "bin/mongod --dbpath $DATA --port $PORT"
  port     = 27017
}

app "api" {
  run   = "go run ./cmd/api"
  port  = 8080
  after = [task.migrate]
  env = {
    DATABASE_URL = module.db.url
    MONGO_URL    = "mongodb://${service.mongo.address}"
  }
}

task "migrate" {
  run    = "go run ./cmd/migrate up"
  inputs = ["migrations/*.sql"]
  env    = { DATABASE_URL = module.db.url }
}
```

## Top-level blocks

| Block | Labels | Meaning |
|---|---|---|
| `module <type> "<name>"` | type, name | An engine from the doze registry. The body belongs to the module. |
| `service "<name>"` | name | A long-running command from outside. Asleep until used. |
| `app "<name>"` | name | A long-running command that is your code. Starts with `doze up`. |
| `task "<name>"` | name | A command that runs to completion. |
| `defaults`, `variable`, `locals`, `output`, `tls` | as today | Unchanged. |

Removed: the `modules` block, `process`, `use`, and bare `<type> "<name>"` blocks (see
Porting the old forms).

Root attributes: `stack` replaces `name`. `listen`, `home`, `data_dir` are
unchanged.

## Files

Unchanged from today (`gatherConfigFiles` in `internal/config/load.go`):

- `doze.hcl` is the anchor. Root attributes are read from it.
- Every `*.doze.hcl` beside it is merged in, sorted by name. There is no
  include directive.
- Pointing doze at a folder merges every `*.hcl` in it.

The merged files are one config: a reference reaches a block in any file, and
names are unique across all of them. Lint reports a clash with both files and
lines.

## Attributes

Shared by `service`, `app` and `task`:

| Attribute | Type | Default | Notes |
|---|---|---|---|
| `run` | string | required | Run with `sh -c`. |
| `dir` | string | the folder holding `doze.hcl` | Replaces `cwd`. |
| `env` | map | empty | Values may be references. |
| `env_file` | string | none | As today. |
| `after` | list of references | empty | Ordering without a value, for example `[task.migrate]`. |

`service` and `app` also take:

| Attribute | Type | Default | Notes |
|---|---|---|---|
| `port` | number | none | The public port. See [networking.md](networking.md). |
| `ready` | call | `tcp()` when `port` is set | `tcp()`, `http("/path")`, `log("text")`, `exec("cmd")`. `http` takes a path on the block's own port; `log` matches the text literally. |
| `ready_timeout` | duration | `"60s"` | How long the command gets to become ready. A first `go run` that compiles needs more. |
| `setup` | string or list | none | Once per data folder. See [lifecycle.md](lifecycle.md). |
| `on_ready` | string or list | none | Every wake, once ready. |
| `stop` | string or list | none | Instead of the default signals. |
| `restart` | block | as today | `policy`, `backoff`, `max_retries`. |
| `sleep` | duration or `"never"` | the stack default for `service`, `"never"` for `app` | Per-service idle time. Today the timeout is global only. |
| `front` | string | by kind | Which tab opens first in the dash. |
| `console` | URL | none | A web UI the dash links to. |
| `logs` | `"normal"` or `"quiet"` | `normal` | Quiet sources show only errors in stack-wide logs. |

`service` only:

| Attribute | Type | Notes |
|---|---|---|
| `download` | string, or a map of platform to string | Fetched once, hash recorded in `doze.lock`. Keys of the map form: `mac`, `mac_intel`, `linux`, `linux_arm`. |

`app` only:

| Attribute | Type | Notes |
|---|---|---|
| `watch` | list of globs | Restart when a matching file changes. Tier: should. |
| `direct` | bool | The app binds `port` itself and is not proxied. For anything the proxy cannot carry, such as UDP. |

`task` only:

| Attribute | Type | Notes |
|---|---|---|
| `inputs` | list of globs | Re-run when the content hash of the matches changes. |
| `manual` | bool | Only runs when asked (`doze task <name>`). |

`front`, `console` and `logs` are also accepted inside a `module` block. The
host reads them and strips them before the module decodes its body. This
part is not built in phase 1: it lands with the dash work that reads them
(phases 5 and 6).

`module` and `app` blocks also keep today's meta-arguments: `count`,
`for_each`, `enabled` and `depends_on`. `service` and `task` do not take them
yet.

## Environment given to every command

| Variable | Value |
|---|---|
| `PORT` | The port the command must listen on. Not always the public port; see [networking.md](networking.md#ports). |
| `DATA` | A folder for this instance that survives restarts and is removed by `doze reset`. |
| `HOST` | The address to bind. |

These are ordinary environment variables, so a command uses `$PORT`. There is
no template syntax of doze's own.

## References

One rule: `kind.name.attribute`.

| Reference | Attributes |
|---|---|
| `module.<name>` | `url`, `address`, `host`, `port`, `socket`, `env_var`, plus whatever the module adds |
| `service.<name>` | `address`, `host`, `port`, `data` |
| `app.<name>` | `address`, `host`, `port`, `url` (when it serves HTTP) |
| `task.<name>` | none. Used only in `after`. |

- A reference in `env` is a dependency with condition `healthy`, as today.
- A reference in `after` is ordering only: the target must have succeeded
  (task) or be ready (anything else).
- Names are unique across the stack, whatever the kind. A name is also a
  DNS label (`db.shop.doze`) and a data folder, so `module.db` and
  `service.db` cannot both exist. Lint reports the clash with both positions.
- A reference written the old way, type first (`postgres.db.url`), fails with
  the new form in the message.

## How the keyword reaches a plugin

A plugin decodes its own block and finds it by type and name
(`doze-sdk/plugin/server.go`, `DecodeRequest`). So the host keeps rewriting
`module postgres "db"` to `postgres "db"` in the bytes it hands to plugins, the
way `rewriteUseBlocks` handles `use` today. The plugin protocol does not
change.

In the host's evaluation context, each module instance is published under
`module.<name>` only. `<type>.<name>` no longer resolves.

## Lint

`doze lint` must work offline. Today it runs the full load, which fetches
modules. It splits into two passes:

1. **Shape**, no network: block kinds, labels, attributes of `service`, `app`
   and `task`, references that point at something declared, unique names,
   cycles, exactly one of the forms of `download`.
2. **Modules**, needs the cache or the network: each module decodes its own
   body.

Unknown module type: `no module called "postgre". Did you mean "postgres"?`
The suggestion logic exists today in `buildPending`.

## Porting the old forms

doze has never gone live, so there is no compatibility to keep (decision
D10). The old forms are removed outright, there is no deprecation window, and
there is no `doze migrate` command. An old form fails lint with a message
that names the new one.

The configs in this repo, its tests and the website docs are ported by hand
using this table:

| Old | New |
|---|---|
| `postgres "db" {}` and `use postgres "db" {}` | `module postgres "db" {}` |
| `process "api" { command, cwd }` | `app "api" { run, dir }` |
| `hooks { pre_start }` | `setup` if it is safe to run once, otherwise the first line of `run` |
| `hooks { post_start }` | `on_ready` |
| `hooks { pre_stop }` | `stop` |
| `health { http = … }` | `ready = http(…)` |
| `forward = 80`, `ingress = true` | dropped; an app with a `port` is named automatically |
| `postgres.db.url` | `module.db.url` |
| `name = "shop"` | `stack = "shop"` |
| `modules { … }` | removed |

## Out of scope

- `sensitive` stays a no-op for now. Making it real is separate work.
- Profiles and per-environment overrides. `doze up <name>` covers the common
  need.
