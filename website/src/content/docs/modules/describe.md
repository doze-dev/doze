---
title: "Describe(): docs and gates from code"
description: One method generates your registry page, terminal docs, and the signed engine-support list — so nothing can drift.
---

`Describe()` exists because hand-written module docs rot. doze's original
hand-authored metadata drifted badly enough to document config blocks that no
longer existed — so now **everything user-facing generates from the driver**,
and the release tool refuses to package a module without it.

## What generates from it

| Surface | From |
|---|---|
| The module's registry page (config tables, example, tagline) | `Describe()` → `meta.yaml` |
| The raw `meta.yaml` a registry serves | the same document |
| The **signed index's engine-support gate** (`releases.<v>.engines`) | `Describe().Versions` |
| The catalog (`index.json` — registry search, init wizard) | title/tagline/category/versions |

That third row is the one with teeth: the versions you claim become the
machine-enforced compatibility contract. Claim `{"14"…"18"}` and a user
declaring `version = 19` is stopped at lint with the upgrade message —
*before* your code runs against an engine you never tested.

## The shape

```go
func (Driver) Describe() engine.Description {
    return engine.Description{
        Title:    "PostgreSQL",
        Tagline:  "Real local Postgres, declared not scripted.",
        Category: "database",             // database | cache | queue | storage | workflow | other
        Port:     5432,
        Versions: []string{"14", "15", "16", "17", "18"},  // the engine-support gate
        Source:   "doze/postgres",
        Example:  `postgres "app" { … }`, // a complete, runnable block
        Config: []engine.ConfigArg{
            {Name: "owner", Type: "string", Desc: "Owner role for the default database."},
            {Name: "io_method", Type: "string", Since: "18",
             Desc: "Asynchronous I/O method."},          // renders an "engine ≥ 18" badge
        },
        Blocks: []engine.ConfigBlock{
            {Name: "role", Label: "name", Desc: "A login role, converged on the server.",
             Args: []engine.ConfigArg{ … }},
        },
    }
}
```

Conventions that matter:

- **`Versions` are engine majors** you actually support end to end (Resolve +
  converge + tests). Versionless engines (self-contained servers) leave it
  empty — no gate.
- **`Since`/`Until`** on an argument must have a matching
  `engine.RequireVersion` check in `DecodeConfig` — badge and gate from one
  intent. Reviewers check them as a pair.
- **The example must run as written.** It's the first thing every user pastes.

## The drift guard

Keep the template's test — it fails when `Describe()` and your decode schema
diverge, in either direction:

```go
func TestDescribeMatchesConfig(t *testing.T) {
    documented := …from Describe().Config…
    for _, want := range []string{"root", "owner", …} {  // keep in sync with Config struct
        if !documented[want] { t.Errorf("decoded but undocumented: %q", want) }
    }
}
```

Cheap insurance: the moment a PR adds a config field without documenting it
(or documents fiction), CI says so — which is the entire reason users can
trust the generated registry page over a README.
