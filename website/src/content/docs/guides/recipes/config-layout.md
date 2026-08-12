---
title: "Recipes — Config layout"
---


Quick patterns for organizing config across files. For the full picture — where
doze stores everything, what to commit vs ignore, and resetting state — see the
**[Files & storage guide](/guides/files-and-storage/)**.

## Split config across `*.doze.hcl` files

`doze.hcl` is the anchor (it holds root settings); every sibling **`*.doze.hcl`**
file beside it is merged automatically, sorted and deterministic. Split by concern:

```
my-app/
  doze.hcl              # root settings (listen/defaults/tls) + variables/outputs
  databases.doze.hcl    # postgres / valkey / kvrocks
  aws.doze.hcl          # s3 / sqs / sns
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
# aws.doze.hcl
aws "cloud" {
  port = 4566
  bucket "uploads" {}
  queue  "emails" {}
}
```

```sh
doze status     # shows app (doze.hcl) + cloud (aws.doze.hcl)
doze doctor     # validates the merged whole
```

Instance names must be unique across all files; root settings (`listen`,
`defaults`, `tls`, …) belong only in `doze.hcl`. A plain `*.hcl` sibling is **not**
merged — only `*.doze.hcl` — so unrelated HCL files in the folder are left alone.
Errors are reported with the file, line, and a snippet.

Or merge every `*.hcl` in a directory:

```sh
doze --config ./config status
```

## Per-developer overrides

Shared instances stay committed in `doze.hcl` (and friends); each developer adds
personal ones in a **gitignored** `local.doze.hcl`:

```hcl
# local.doze.hcl  (gitignored — yours alone)
postgres "scratch" {
  version = 17
  role "me" { password = "me" }
}
```

```gitignore
# .gitignore
.doze/
local.doze.hcl
```

For tweaking *values* (not adding instances), a gitignored `*.auto.doze.vars` file
overrides [variables](/reference/configuration/#variables-locals--outputs)
without touching the config.

## Versions & TLS

These root-level concerns are covered in the reference:

- **[Versions & the lockfile](/reference/configuration/#versions--the-lockfile)** — major vs exact, `doze.lock`, `doze binaries available`.
- **[TLS](/reference/configuration/#tls)** — auto self-signed or bring your own cert.
