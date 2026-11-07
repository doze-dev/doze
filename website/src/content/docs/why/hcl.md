---
title: "Why HCL"
description: Why doze.hcl is HCL and not YAML or JSON, argued from what doze does with it.
---

You'll know why `doze.hcl` is written in HCL, the language Terraform made
familiar, and what that buys you.

The short version: a doze config has types, references between blocks and
expressions. YAML and JSON are data formats, and would need a second language
on top to do the same.

## A typo is an error, not a setting

Every block is checked against a schema. A module checks its own block, so the
schema is always the one the running code uses. An argument it does not know
is an error with the file, the line and a suggestion:

```text
error: module postgres "app": doze.hcl:6,3-16: Unsupported argument; An argument named "shared_bufers" is not expected here. Did you mean "shared_buffers"?
```

In YAML the same typo is silent: the key is carried along and ignored, and you
find out when the setting you thought you set never applied. Add YAML's own
traps (`no` read as `false`, `3.10` read as `3.1`, indentation deciding
structure) and its ease of starting becomes a cost later. JSON avoids those but
cannot hold a comment, which a file teammates read needs.

## References are the start order

Blocks refer to each other as values:

```hcl
module postgres "app" {
  version = 18
}

app "api" {
  run  = "go run ./cmd/api"
  port = 8080
  env = {
    DATABASE_URL = module.app.url
  }
}
```

That reference is three things at once. It fills in the connection string. It
orders the start: `api` waits until `app` is ready. And it is checked before
anything runs:

```text
Error: reference to undeclared module "ap"

  on doze.hcl line 4, in app "api":
   4:   env = { DATABASE_URL = module.ap.url }

no module named "ap" is declared. Did you mean module.app?
```

In YAML the same link is a string that happens to match a name, enforced by
nothing until something breaks while running. Compose has `depends_on` because
a YAML value cannot say "this comes from that service"; the order is written
a second time beside the data.

## Expressions instead of templates

Real projects want three tenant databases from one list, a port each, or a
value that differs between a laptop and CI. HCL has variables, locals,
functions, `for_each` and `count`:

```hcl
variable "tenants" {
  default = { acme = 5432, globex = 5433, initech = 5434 }
}

module postgres "tenant" {
  for_each = var.tenants
  version  = 18
  port     = each.value
  owner    = each.key
}
```

The YAML answer is a template language on top (Helm's Go templates, `envsubst`,
anchors stretched past their purpose), and then the config is no longer what
is in the file. In HCL, the file is the program.

## It reads like what it declares

```hcl
module postgres "app" {
  version = 18

  role "app" {
    password = "app"
    login    = true
  }

  extension "vector" {}
}
```

Blocks with labels have the shape of the thing: a server holding roles and
extensions. It reads well in a diff, takes comments, and anyone on your team
who has used Terraform reads it already.

## The cost

HCL is one more syntax, and less common than YAML outside the Terraform world.
That cost is paid once, in the first hour. To keep it small, most stacks use
only what is on this page: blocks, arguments and references. `doze fmt` lays a
file out, `doze lint` checks it, and an editor schema at
`/schema/doze-hcl.json` gives completion. Each module's page in the registry
lists every argument it takes, generated from the module itself.
