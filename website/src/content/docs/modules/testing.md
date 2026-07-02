---
title: "Testing modules"
description: Three layers — decode tests, the enginetest harness against real backends, and acceptance CI.
---

Module testing has three layers, each catching what the previous can't.

## 1. Decode tests (fast, always on)

Pure functions over your `DecodeConfig` — every `go test` run, no backend:

```go
func TestDecodeConfig(t *testing.T) {
    spec, err := decode(t, `maxmemory = "256mb"`)   // parse HCL, call DecodeConfig
    …
    // strictness is a feature — prove it stays:
    if _, err := decode(t, `maxmemoryy = "1"`); err == nil {
        t.Fatal("unknown attributes must error")
    }
}
```

Cover: defaults, validation errors (they're UX — assert the *messages*),
version gates (`RequireVersion` firing on the right majors and passing on
empty), and the [Describe drift guard](/modules/describe/#the-drift-guard).

## 2. enginetest: your driver against a real backend

`doze-sdk/enginetest` boots a **real engine straight from your driver** —
resolve → provision → run your SpawnPlan to readiness → converge — with no
doze daemon or proxy in the way. It's doze's analog of Terraform's acceptance
harness:

```go
//go:build acceptance

func TestRolesConverge(t *testing.T) {
    b := enginetest.Boot(t, postgres.Driver{}, enginetest.Options{
        Version: "18",
        HCL: `postgres "acc" {
            role "app" { password = "x" login = true }
        }`,
    })
    // b is a live backend: connect to b.Port(), assert the role exists,
    // then change config and b.Converge() again to test idempotency.
}
```

Two deliberate properties:

- **It skips without a binary.** `Boot` requires
  `DOZE_<ENGINE>_BINDIR` and calls `t.Skip` when unset — plain `go test`
  stays green offline. Gate the files with an `acceptance` build tag.
- **It tests *convergence*, not just decode** — that `role`/`extension`/
  `bucket` blocks actually materialize in the running engine, including the
  second run doing nothing (idempotency is the contract).

## 3. Acceptance in CI

The official modules run the acceptance matrix weekly and on demand: CI builds
each engine's real backend from the
[doze-binaries recipes](/operate/mirror-binaries/), exports the bindir, and
runs `go test -tags acceptance`. For your module, the same shape works with
any way of producing a backend binary — build from source, download upstream,
or point at your mirror:

```yaml
- run: |
    # produce a backend, then:
    export DOZE_MYENGINE_BINDIR=$PWD/backend/bin
    go test -tags acceptance -timeout 20m ./...
```

## The end-to-end smoke that isn't a test

Before releasing, run the real thing once — it catches the integration
surprises unit layers can't:

```sh
go build -o /tmp/my-plugin ./plugin
DOZE_MYTYPE_PLUGIN=/tmp/my-plugin doze up     # in a scratch project
doze lint && doze status && <connect a real client>
```

Ten seconds, and it exercises the actual plugin protocol, the proxy splice,
readiness, and reaping — the full production path minus the registry.
