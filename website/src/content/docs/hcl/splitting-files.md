---
title: Splitting files
description: Split a stack's doze.hcl across several files, and change a few settings on your machine only with an override file.
redirect_from:
  - /guides/recipes/config-layout/
---

Split a large doze.hcl into files by topic, and change a port or a sleep time
on your machine without touching what your team runs.

## One stack, several files

doze reads `doze.hcl` and then every `*.doze.hcl` file beside it, in name
order, as one config:

```text
my-app/
  doze.hcl              stack name, defaults, variables
  data.doze.hcl         postgres, valkey
  aws.doze.hcl          the aws module
  services.doze.hcl     your apps and tasks
```

```hcl
# doze.hcl
stack = "shop"

defaults {
  names = true
}
```

```hcl
# aws.doze.hcl
module aws "aws" {
  bucket "uploads" {}
  queue "emails" {}
}
```

The rules:

- Blocks can live in any file. Names must be unique across all of them, and a
  clash is an error that says where each one is.
- A stack-wide setting (`stack`, `defaults`, `tls`, `data_dir`, …) is set
  once. Setting it in two files is an error; keep them in `doze.hcl`.
- Only `*.doze.hcl` is read. A plain `*.hcl` file in the folder, such as
  Terraform, is left alone.
- A path in an app, service or task (`dir`, `env_file`) is relative to
  the file that declares it.

`doze fmt` formats `doze.hcl` and its `*.doze.hcl` siblings; `doze lint`
checks them together.

To keep the config in a folder of its own, point `--config` at the folder:
every `*.hcl` in it is read.

```sh
doze --config ./config up
```

## Change a few settings on your machine only

A file whose name ends in `.override.doze.hcl` changes blocks declared
elsewhere, for you alone. Keep it out of git.

```hcl
# local.override.doze.hcl
module postgres "db" {
  port = 5499
}

app "admin" {
  enabled = false
}

module kafka "events" {
  sleep = "never"
}
```

An override names a block by its kind, module and name, and can change only
`port`, `enabled`, `sleep` and `version`. Anything else is an error that names
it, and so is an override for a block nothing declares, so a renamed block
cannot leave a stale override quietly doing nothing.

## Add something of your own

To add a block only you need, such as a scratch database, put it in another
`*.doze.hcl` that you keep out of git:

```hcl
# local.doze.hcl
module postgres "scratch" {
  version = 18
}
```

To change a variable's value instead, use a `*.auto.doze.vars` file
([References and env](/hcl/references-and-env/#variables)).

## What to commit

| File | Commit |
|---|---|
| `doze.hcl`, shared `*.doze.hcl` | yes |
| `doze.lock` | yes |
| `*.override.doze.hcl`, `local.doze.hcl`, `*.auto.doze.vars` you keep to yourself | no |
| `.doze/` | no |

```gitignore
.doze/
*.override.doze.hcl
local.doze.hcl
```

Where doze keeps data, binaries and logs is in
[Files and storage](/guides/files-and-storage/).
