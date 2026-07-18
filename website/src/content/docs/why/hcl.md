---
title: "Why HCL (and not YAML or JSON)"
description: Config that declares infrastructure should have types, references, and expressions — argued from doze's actual features.
---

`doze.hcl` is HCL — the language Terraform made familiar. This page argues the
choice from what doze actually does with it, not from taste. The short version:
**doze's config is a small program with a schema, and YAML/JSON are data
formats being asked to impersonate one.**

## Typos are errors, not settings

Every doze block is decoded against a strict, typed schema — by the module that
owns it, so the schema is always the one the running code enforces. An unknown
attribute is a positioned error with a suggestion:

```
error: postgres "app": unsupported argument "shared_bufers"

  on doze.hcl line 7:
   7:   shared_bufers = "256MB"

did you mean "shared_buffers"?
```

YAML's failure mode for the same typo is *silence* — the key is carried along
and ignored by whatever reads it, and you discover it at 5pm when the setting
you thought you set never applied. Add the classic traps (`no` parsing as
`false`, `3.10` as `3.1`, indentation deciding structure invisibly) and YAML's
approachability starts looking like deferred cost. JSON avoids the ambiguity
but can't hold a comment — disqualifying for a file whose job is to be read by
teammates.

## References are the dependency graph

This is the feature the others can't fake. doze blocks reference each other as
**values**:

```hcl
sqs "jobs" { port = 9324 }

sns "signups" {
  port = 9911
  sqs  = sqs.jobs.name        # a typed reference, not a string that happens to match
}
```

That reference *is* the dependency edge: doze derives boot order from it (SNS
boots and holds its SQS), validates it (a reference to an undeclared instance
is a positioned error, not a runtime surprise), and re-evaluates it if the
target changes. In YAML you'd write `sqs: jobs` as a string, and the fact that
it names another service is a convention enforced by nothing until something
breaks at runtime. Compose's `depends_on` exists precisely because YAML can't
express "this value comes from that service" — it bolts the graph on beside the
data.

## Expressions scale past the toy example

Real projects want three tenant databases from a list, a port that's
`base + index`, a password from an environment variable in CI but a literal
locally. HCL has variables, locals, functions, and `for_each`/`count`:

```hcl
variable "tenants" { default = ["acme", "globex", "initech"] }

postgres "tenant" {
  for_each = var.tenants
  version  = 18
  port     = 5432 + index(var.tenants, each.value)
  owner    = each.value
}
```

The YAML answer to this is templating YAML with a second language — Helm's Go
templates, `envsubst`, YAML anchors stretched past their design — each of which
means your config is no longer what's in the file. HCL keeps the program and
the file the same artifact.

## It reads like what it declares

```hcl
postgres "app" {
  version = 18

  role "app" {
    password = "app"
    login    = true
  }

  extension "pgvector" {}
}
```

Blocks-with-labels mirror the shape of the infrastructure: an instance
containing roles containing settings. It's scannable in review diffs, it
comments naturally, and every Terraform user on your team already reads it
fluently.

## The counterargument, acknowledged

HCL is one more syntax, and outside the Terraform world it's less universal
than YAML. That's a real cost — paid once, in the first hour. The YAML costs
above are paid on every silent typo, every stringly-typed reference, and every
template layer, forever. doze also keeps the learning surface small: most
projects use exactly what's on this page — blocks, attributes, references —
and each engine's registry page puts every valid argument one click away,
generated from the module itself.
