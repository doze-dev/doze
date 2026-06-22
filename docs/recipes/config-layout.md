# Recipes — Config layout

Organizing config across files, pinning versions, TLS, and tuning the lifecycle.

## Split config with `doze.d`

Root settings (`listen`, `home`, `data_dir`, `defaults`, `tls`) live in
`doze.hcl`; instance blocks can be split into a sibling `doze.d/*.hcl` directory,
which doze merges automatically (sorted, deterministic).

```
project/
  doze.hcl              # root settings + core instances
  doze.d/
    storage.hcl         # s3 buckets
    queues.hcl          # sqs + sns
    analytics.hcl       # a second postgres
```

```hcl
# doze.hcl
defaults { idle_timeout = "5m" }
listen = "127.0.0.1:6432"

postgres "app" {
  version = 16
  role "app" { password = "app" }
}
```

```hcl
# doze.d/storage.hcl
s3 "media" {
  bucket "uploads" {}
}
```

Everything is loaded as one config:

```sh
doze status        # shows app (doze.hcl) and media (doze.d/storage.hcl)
doze doctor        # validates the merged set
```

Or point `--config` at a directory to merge all `*.hcl` in it:

```sh
doze --config ./config status
```

Config errors are reported with the **file, line, and a snippet** — including
cross-file duplicate instance names and unknown block types (with a "did you
mean?" hint).

## Per-developer overrides

Keep shared instances in `doze.hcl` (committed) and let each developer add
personal instances/tweaks in a gitignored `doze.d/local.hcl`:

```
# .gitignore
doze.d/local.hcl
```

```hcl
# doze.d/local.hcl  (not committed)
postgres "scratch" {
  version = 17
  role "me" { password = "me" }
}
```

## Versions & the lockfile

```hcl
postgres "app"   { version = 16 }        # newest 16.x, resolved + pinned
postgres "exact" { version = "16.14" }   # always 16.14.0
valkey   "cache" { version = 9 }
```

- A bare major resolves to the newest minor and is pinned in **`doze.lock`**.
- A dotted string pins an exact minor.
- Commit `doze.lock` so every machine gets the same binaries (it records the
  resolved version + SHA-256 per platform). See [Managing binaries](../BINARIES.md).

```sh
doze versions postgres     # what the mirror offers; marks installed + pinned
```

## TLS (Postgres clients)

```hcl
tls {}                       # auto-generate a self-signed cert; sslmode=require works
```

```hcl
tls {
  cert     = "./server.crt"  # or bring your own PEM cert + key
  key      = "./server.key"
  required = true            # reject plaintext TCP clients
}
```

TLS is terminated at the proxy for Postgres; the backend speaks plaintext over a
local unix socket.

## Lifecycle & storage

```hcl
defaults {
  idle_timeout = "5m"        # reap an instance after this long at zero connections
}

listen   = "127.0.0.1:6432"  # base address; each instance gets the next port,
                             # or set `listen` per instance, or "unix:/path"
home     = "~/.doze"         # shared toolchains + cache (deduped across projects)
data_dir = "./.doze-state"   # this project's state (default: <home>/projects/<slug>)
```

- Reaping is by **connection count**, never query inactivity — pools that hold
  idle connections keep their backend alive.
- Per-instance `listen` overrides the auto-assigned port:
  ```hcl
  s3 "media" {
    listen = "127.0.0.1:4566"
    bucket "uploads" {}
  }
  ```
