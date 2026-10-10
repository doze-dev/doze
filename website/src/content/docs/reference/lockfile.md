---
title: "doze.lock"
description: The lockfile format — two pin layers, their semantics, and how each changes.
---

`doze.lock` lives next to `doze.hcl`, is written by doze, and is **meant to be
committed** — it's the project's reproducibility contract. YAML (an older JSON
lockfile still parses; JSON is a YAML subset).

## The two layers

```yaml
engines:
  postgres:
    "16":                              # keyed by the DECLARED spec ("16" or "16.14")
      resolved: 16.14.0                # what it resolved to
      source: mirror                   # mirror | override
      hashes:
        aarch64-apple-darwin: sha256:67b4…   # per-platform archive checksums,
        x86_64-unknown-linux-gnu: sha256:ef59…  # merged as platforms resolve

modules:
  doze/postgres:                       # keyed by module SOURCE, one pin per source
    version: 0.2.1                     # the MODULE release (not an engine version)
    protocol: 1                        # doze plugin protocol it speaks
    engines: ["14", "15", "16", "17", "18"]  # engine majors it supports
    hashes:                            # EVERY published platform, from the signed
      aarch64-apple-darwin: sha256:64607…    # index — teammates on other platforms
      aarch64-unknown-linux-gnu: sha256:044e…  # verify against the same pin
      x86_64-unknown-linux-gnu: sha256:9bd7…
```

## Semantics worth knowing

- **Engines layer** — written when an instance's declared version first
  resolves. A bare major pins the newest minor *at that moment*; it never
  moves on its own. New platforms merge their hash into the same pin.
- **Modules layer** — written on first module resolution; carries `protocol`
  and `engines` so compatibility gating works **offline**. Replaced wholesale
  by `doze modules upgrade` (the diff in review shows release, support list,
  and checksums moving together).

## What changes it, and what never does

| Action | Effect on the lock |
|---|---|
| First resolve of an engine or module | Adds the pin |
| `doze modules upgrade [type]` | Moves module pins (and prints a commit reminder) |
| Changing `version = 16` → `"16.15"` in config | New engines entry under the new spec |
| `doze up` / `run` / time passing / registry changes | **Nothing — ever** |

- Pinned + cached resolution touches **no network**.
- `DOZE_<ENGINE>_BINDIR` / `DOZE_<TYPE>_PLUGIN` overrides bypass the lock
  (that's their job) and never write to it.
- Deleting the file is safe-but-loud: everything re-resolves fresh (majors may
  land on newer minors, modules on newer releases) and re-pins. Prefer
  `doze modules upgrade` — it's the same effect for modules, with intent.
