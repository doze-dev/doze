---
title: "Real-engine modules"
description: Resolve against a binaries mirror, converge structure, own the wire — postgres as the worked example.
---

A module wrapping a real upstream server (a database, a queue broker) adds
three responsibilities the self-contained shape doesn't have. Read this with
two references open: `modules/valkey` (the minimal versioned module, ~200
lines) and `modules/postgres` (everything) in
[doze-modules](https://github.com/doze-dev/doze-modules).

## 1. Resolve: from `version = 16` to executables

The user's declared version arrives as a `VersionSpec` ("16" or "16.14"). Your
job — with the `Fetcher` and `Locker` doze hands you:

```go
func (Driver) Resolve(ctx context.Context, spec engine.VersionSpec, plat engine.Platform,
    lk engine.Locker, fetch engine.Fetcher) (engine.Toolchain, error) {

    // 1. A lock pin wins — reproducibility beats freshness.
    if pin, ok := lk.Get("myengine", spec, plat); ok && pin.Resolved != "" {
        binDir, _, err := fetch.Ensure(ctx, "myengine", pin.Resolved, plat, pin.Hashes[plat.Triple])
        …
    }
    // 2. Exact spec ("16.14") → normalize to your mirror's full form.
    // 3. Bare major ("16")   → fetch.ResolveMajor("myengine", "16") → "16.14.0".
    full, err := fetch.ResolveMajor("myengine", spec.String())
    binDir, digest, err := fetch.Ensure(ctx, "myengine", full, plat, "")
    // Ensure records the pin; return the toolchain (bin dir + named tools).
}
```

`Ensure` handles download, checksum verification, and the content-addressed
cache; `DOZE_<ENGINE>_BINDIR` overrides all of it for development. Where do
binaries come from? A mirror in the
[doze-binaries format](/operate/mirror-binaries/) — use the official one for
official engines, or publish your own.

Advertise what you support in `Describe().Versions` (`{"14"…"18"}`) — that
list becomes the signed registry gate that catches `version = 19` before
anything runs.

## 2. Converge: declared structure into the running engine

Implement the structural trio when your engine has objects worth declaring:

- **`Converge`** runs after first provision (and on `doze sync`): connect over
  the *backend* socket as superuser and idempotently create/update what the
  config declares — postgres does roles, the database, schemas, extensions,
  grants; mariadb does databases/users/grants; s3 creates its bucket.
- **`Inventory`** lists the objects the config implies (no live queries), so
  plan/apply can diff.
- **`Pruner`** drops objects removed from config.

Convergence owns **structure, not data** — never touch user tables.

## 3. The run path

`Plan` returns SpawnSpecs core supervises: the server binary from your
toolchain, args pointing at the instance's data dir and **unix socket** (the
proxy dials `BackendSocket(...)`; you usually don't bind TCP at all), and a
readiness probe (`socket`, `tcp`, `http`, `exec`, or `log_line`). Composite
engines return multiple specs with `After` ordering — ferret's plan is
\[private postgres, extension setup hook, ferretdb gateway\].

Worth stealing from postgres when relevant:

- **`Templater`** — run `initdb` once per version into a shared template,
  clone per instance (copy-on-write where the filesystem allows). Turns 2s
  provisions into 50ms.
- **`ProxyFilter`** — only if your protocol needs preamble handling (postgres:
  the SSL negotiation, startup message buffering, out-of-band CancelRequest
  routing). The filter runs in *your* process; core hands you the client fd.
- **`SlowBooter`** — declare an honest first-boot budget if provisioning is
  legitimately slow (ferret's extension setup).

## 4. Version-gated behavior

When engine majors differ (a config argument that only exists since 18, an
initdb flag that changed), gate at decode time:

```go
if _, set := raw.Settings["io_method"]; set {
    if err := engine.RequireVersion(version, 18, `settings["io_method"]`); err != nil {
        return nil, err   // "requires engine version >= 18 (declared 16)" at doze lint
    }
}
```

…and mark the argument `Since: "18"` in `Describe()` so the docs carry the
badge. One source, both surfaces — see [Describe](/modules/describe/).
