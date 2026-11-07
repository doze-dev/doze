---
title: "Test it"
description: Run the same checks doze-modules CI runs on your module, from unit tests to acceptance against the real backend.
redirect_from: ["/modules/testing/"]
---

This page shows you how to run every check CI runs on a module, on your own machine, before you open the pull request.

A module is tested in two layers. Unit tests run on every pull request and need
no engine. Acceptance tests boot the real engine and run before any release. Run
both locally: acceptance does not run on the pull request, and a module whose
acceptance fails cannot release.

## Unit tests and vet

From the root of doze-modules:

```sh
go vet ./...
go test -race ./...
```

This is what `test.yml` runs on every pull request and push to `main`, on
`ubuntu-latest` and `macos-latest`. Besides your module's own tests, it runs
the guards in `cmd/dzm`:

- every module in `modules.yaml` has a describer, and every describer has a
  `modules.yaml` entry;
- every `Describe()` is well formed (`Source`, title, tagline, category, config,
  example, versions; see [Write it in doze-modules](/contribute/write/#describe-docs-and-gates-from-the-code));
- the module table in `README.md` matches what the describers generate. If it
  fails, run `go run ./cmd/dzm readme`.

Your own unit tests decode HCL and check the result, with no backend. Cover:

- defaults and the generated engine config;
- validation errors, and their messages, because users read them;
- unknown fields, which must fail (valkey's `TestValkeyUnknownKey` decodes
  `bogus = "x"` and expects an error naming it);
- each version gate, firing on the old major and passing on the new one;
- `ConnString`, with and without a password.

## Acceptance: the real backend

Acceptance tests boot a real engine straight from your driver with
`doze-sdk/enginetest`: resolve, provision, run the `SpawnPlan` to readiness,
converge. No doze daemon or proxy is involved. Then they check that each
option actually reached the running engine.

Put them in `acceptance_test.go` behind the `acceptance` build tag. This is the
start of valkey's:

```go
//go:build acceptance

func TestAcceptance(t *testing.T) {
	b := enginetest.Boot(t, Driver{}, enginetest.Options{
		Name: "cache",
		HCL:  "maxmemory        = \"100mb\"\nmaxmemory_policy = \"allkeys-lru\"",
	})

	t.Run("maxmemory applied", func(t *testing.T) {
		out := valkeyCli(t, b, "CONFIG", "GET", "maxmemory")
		lines := strings.Fields(out)
		if got := lines[len(lines)-1]; got != "104857600" {
			t.Fatalf("maxmemory = %q, want 104857600 (100mb)", got)
		}
	})
}
```

What the harness gives you:

- `enginetest.Boot(t, driver, opts)` boots a backend. `opts.HCL` is the block
  body, `opts.Version` the engine major (empty for a versionless engine).
  Cleanup is registered on `t`.
- `b.Converge(hcl)` converges more config into the running backend. Call it
  twice with the same config to prove the second run changes nothing.
- `b.Objects()` and `b.Prune(removed)` test `Inventory` and `Pruner`.
- `b.SocketDir()`, `b.Port()` and `b.Instance()` let your test connect.
- `enginetest.BootExample(t, driver, version)` boots your `Describe().Example`.
  Postgres uses it, so the example users copy is proven to converge.

Test that structure lands in the engine, not only that it decodes. For a
module with a `Converger`, that means querying the engine for each role,
database or grant you declared.

### Run it locally

The harness reads the backend from `DOZE_<NAME>_BINDIR` and **skips** the test,
which counts as a pass, when the variable is unset. So set it. The repo's
script fetches the same published backend CI uses (it needs `curl`, `tar` and
`yq`). The platform defaults to `x86_64-unknown-linux-gnu`; on a Mac, name
yours:

```sh
scripts/backend-env.sh <name> aarch64-apple-darwin > /tmp/<name>.env
set -a; . /tmp/<name>.env; set +a

go test -tags acceptance -v -timeout 20m ./modules/<name>/...
```

Or point the variable at a backend you already have, for example one doze has
downloaded:

```sh
DOZE_VALKEY_BINDIR=~/.doze/valkey/9.1.1-aarch64-apple-darwin/bin go test -tags acceptance ./modules/valkey/...
```

Some modules pin the major they test through `DOZE_<NAME>_VERSION`; the script
prints it when it does (postgres tests 16, mariadb 11.4).

Watch the output for `SKIP`. A skipped acceptance run proves nothing.

## Build the archives (optional)

To see what release CI will build:

```sh
go build -o /tmp/dzm ./cmd/dzm
/tmp/dzm --repo . --out dist --module <name>
/tmp/dzm meta --out dist --module <name>
```

`dist/<name>/` then holds an archive per platform, an unsigned `index.yaml` and
`meta.yaml`. Read `meta.yaml`: it is your module's page in the registry.

## Where CI runs what

| Workflow | When | What |
|---|---|---|
| `test.yml` | Every pull request and push to `main` | `go vet ./...` and `go test -race ./...` on Linux and macOS |
| `acceptance.yml` | Weekly (Monday 04:00 UTC) and on demand | Acceptance for every module in its `matrix.module` list, against the published backends |
| `release.yml` | Push to `main` | Acceptance for every module whose version changed, as the gate before it is built and published |

Next: [Submit and release](/contribute/submit/).
