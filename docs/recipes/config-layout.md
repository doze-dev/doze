# Recipes — Config layout

Quick patterns for organizing config across files. For the full picture — where
doze stores everything, what to commit vs ignore, and resetting state — see the
**[Files & storage guide](../guide/files-and-storage.md)**.

## Split config with `doze.d`

Root settings live in `doze.hcl`; instance blocks can be split into a sibling
`doze.d/*.hcl` directory, merged automatically (sorted, deterministic).

```
my-app/
  doze.hcl            # root settings (listen/defaults/tls) + core instances
  doze.d/
    databases.hcl     # extra postgres instances
    cache.hcl         # valkey / kvrocks
    aws.hcl           # s3 / sqs / sns
```

```hcl
# doze.hcl
defaults { idle_timeout = "5m" }

postgres "app" {
  version = 16
  role "app" { password = "app" }
}
```

```hcl
# doze.d/aws.hcl
s3 "media" {
  bucket "uploads" {}
}
sqs "jobs" {
  queue "emails" {}
}
```

```sh
doze status     # shows app (doze.hcl) + media, jobs (doze.d/)
doze doctor     # validates the merged whole
```

Instance names must be unique across all files; root settings (`listen`,
`defaults`, `tls`, …) belong only in `doze.hcl`. Errors are reported with the
file, line, and a snippet.

Or merge every `*.hcl` in a directory:

```sh
doze --config ./config status
```

## Per-developer overrides

Shared instances stay committed in `doze.hcl`; each developer adds personal ones
in a **gitignored** `doze.d/local.hcl`:

```hcl
# doze.d/local.hcl  (gitignored)
postgres "scratch" {
  version = 17
  role "me" { password = "me" }
}
```

```gitignore
# .gitignore
.doze/
doze.d/local.hcl
```

## Versions & TLS

These root-level concerns are covered in the reference:

- **[Versions & the lockfile](../reference/configuration.md#versions--the-lockfile)** — major vs exact, `doze.lock`, `doze binaries available`.
- **[TLS](../reference/configuration.md#tls)** — auto self-signed or bring your own cert.
