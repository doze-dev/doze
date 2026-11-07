---
title: "Write it in doze-modules"
description: Lay out a new module in doze-modules, implement the driver, describe it, and register it in the four places the repo checks.
redirect_from: ["/modules/overview/", "/modules/real-engines/", "/modules/describe/", "/modules/first-module/"]
---

This page shows you how to write a module inside the doze-modules repo, from an empty folder to a driver that the repo's tests accept.

Start here once your [proposal](/contribute/propose/) is agreed and the
engine's backend is published in
[doze-binaries](https://github.com/doze-dev/doze-binaries). Clone
[doze-modules](https://github.com/doze-dev/doze-modules); every step below
happens in that repo.

## What a module is

A module is a pure-Go plugin binary. doze starts it as a separate process and
talks to it over gRPC, using the contract in
[doze-sdk](https://github.com/doze-dev/doze-sdk). doze itself knows nothing
about any engine: it owns the proxy, the supervisor, sleep and wake, the config
and the lockfile. Everything specific to the engine lives in the module: how to
find its binaries, how to initialise its data, how to start it, and which
`doze.hcl` fields it takes.

The SDK packages you will use:

| Package | What it gives you |
|---|---|
| `doze-sdk/engine` | The driver contract: interfaces and value types. |
| `doze-sdk/plugin` | `Main`, which serves your driver to doze. |
| `doze-sdk/enginetest` | Boots a real backend from your driver in tests ([Test it](/contribute/test/)). |
| `doze-sdk/modtool` | Builds the archives, the index and `meta.yaml`. The repo's `dzm` tool wraps it. |

## Lay out the folder

Copy the smallest module that resembles yours. `modules/valkey/` has only the
required driver methods. `modules/postgres/` shows convergence, data-dir
templates and a proxy filter.

```text
modules/<name>/
  <name>.go            # the engine.Driver: Type, Resolve, Provision, Provisioned, Plan, …
  config.go            # the struct the block decodes into, and its validation
  describe.go          # Describe(): the docs and the version gate
  config_test.go       # unit tests: decode, validation, generated config
  acceptance_test.go   # //go:build acceptance: boots the real backend
  plugin/main.go       # the plugin binary
```

`plugin/main.go` is a few lines. This is valkey's:

```go
package main

import (
	"github.com/doze-dev/doze-modules/modules/valkey"
	dozeplugin "github.com/doze-dev/doze-sdk/plugin"
)

func main() { dozeplugin.Main(valkey.Driver{}, &valkey.Config{}) }
```

## The driver

Your driver is a stateless value that implements `engine.Driver` and
`engine.Spawner`:

| Method | Its job |
|---|---|
| `Type()` | The block keyword and registry name: `"valkey"` makes `module valkey "…"`. |
| `Resolve(ctx, spec, plat, lk, fetch)` | Find or download the engine binaries for the declared version, honouring the pin in `doze.lock`. |
| `Provision(ctx, inst, tc)` | Make the data directory bootable (for example, run `initdb`). Must be safe to run twice. |
| `Provisioned(dataDir)` | Report whether the data directory is already initialised. |
| `Plan(ctx, inst, tc)` | Return a `SpawnPlan`: the processes to run and a readiness probe. doze runs and supervises them. |
| `BackendSocket(socketDir, port)` | The unix socket doze's proxy dials to reach the backend. |
| `ConnString(inst, ep)` | The connection URL doze shows users, and its conventional variable name (`DATABASE_URL`, `REDIS_URL`). |

### Resolve: from `version = 9` to binaries

Most drivers are an override check plus one call. This is valkey's `Resolve`:

```go
func (Driver) Resolve(ctx context.Context, spec engine.VersionSpec, plat engine.Platform, lk engine.Locker, fetch engine.Fetcher) (engine.Toolchain, error) {
	if dir := os.Getenv(envBinDir); dir != "" { // DOZE_VALKEY_BINDIR
		return engine.Toolchain{Engine: "valkey", BinDir: dir, Full: spec.String()}, nil
	}
	return engine.ResolveVia(ctx, lk, fetch, plat, "valkey", spec, engine.ExactDots(2))
}
```

`ResolveVia` uses a pin from `doze.lock` when there is one. Otherwise it asks
doze to map a major (`"9"`) to a full version and download it from
doze-binaries, with the checksum checked and the result pinned. Your module
never downloads anything itself.

Your `Resolve` **must** honour `DOZE_<NAME>_BINDIR`: when it is set, use the
binaries in that directory. That is how the acceptance tests and CI hand your
driver its backend.

### Plan: how the backend runs

`Plan` returns one or more spawn specs: the server binary from the toolchain,
arguments that point at the instance's data directory and unix socket, and a
readiness probe (`socket`, `tcp`, `http`, `exec` or `log_line`). The backend
usually binds no TCP port at all; doze's proxy owns the port and dials your
socket. An engine made of several processes returns several specs in order.
FerretDB's plan is a private Postgres, then a setup step, then the FerretDB
gateway.

### Optional capabilities

Everything else is an optional interface. doze checks for it by type
assertion, so implement only what your engine needs:

| Interface | When you need it |
|---|---|
| `ConfigDecoder` | Your block has fields. Decode strictly; unknown fields are errors. The declared engine version arrives too, for version-gated fields. |
| `Converger` | The block declares structure (roles, databases, grants). Converge creates it in the running engine after a fresh provision and when the stack is reconciled (`doze sync`). Structure only, never user data. |
| `Inventory`, `Pruner` | Pair with `Converger`: list the objects the config declares, and drop the ones removed from it. |
| `Templater` | Provisioning is slow. Provision once into a template per version and clone it per instance. Postgres does this for `initdb`. |
| `ProxyFilter` | The wire protocol needs handling before the bytes are spliced, such as Postgres's TLS negotiation and cancel requests. |
| `SlowBooter` | The first boot is slow by nature (FerretDB sets up an extension) and needs a longer budget. |
| `Attributer` | The block exposes references beyond the usual name, host, port and url. |
| `Versionless` | The engine has no selectable version. Rare. |

`modules/postgres/` implements most of these, and is the place to read real
examples.

### Gate fields by engine version

When a field only exists from some engine major on, reject it at decode time:

```go
if err := engine.RequireVersion(version, 18, `settings["io_method"]`); err != nil {
	return nil, err // settings["io_method"] requires engine version >= 18 (declared 16)
}
```

Mark the same field `Since: "18"` in `Describe()` so its docs carry the badge.
`RequireVersionBelow` and `Until` are the pair for fields that were removed.
Reviewers check that each badge has its gate.

## Describe(): docs and gates from the code

`Describe()` is mandatory. It is the one source for the module's registry page
(`meta.yaml`) and for the engine majors stamped into the signed index. This is
valkey's, trimmed:

```go
func (Driver) Describe() engine.Description {
	return engine.Description{
		Title:        "Valkey",
		Tagline:      "In-memory cache, Redis-compatible.",
		Category:     "cache",
		Description:  "A local Valkey server, the open-source Redis fork. …",
		Port:         6379,
		Versions:     []string{"8", "9"},
		Source:       "doze/valkey",
		ExampleLabel: "cache",
		Example:      `…`, // a complete valkey block
		Config: []engine.ConfigArg{
			{Name: "version", Type: "number", Required: true, Desc: "Engine major to run — 8 or 9."},
			{Name: "maxmemory", Type: "string", Desc: "Memory ceiling before eviction, e.g. \"256mb\"."},
			// … one entry per field the block accepts
		},
	}
}
```

The tests in `cmd/dzm` refuse a description without:

- `Source` set to `"doze/<name>"`;
- `Title`, `Tagline` and `Category`;
- the config documented, in `Config` (fields) and/or `Blocks` (nested blocks);
- an `Example` that contains a block for the engine;
- `Versions`, the engine majors the module supports, unless the driver is
  `engine.Versionless` (then it must list none).

Two rules the tests can't check, which review does:

- **`Versions` are the majors you support end to end:** resolve, converge and
  tests. A user who declares a major that isn't listed is stopped before your
  code runs. Adding a new major later means extending `Versions`, doing the
  work it needs, and bumping the module version.
- **The example must run as written.** It is the first thing users copy, and
  acceptance converges it before every release.

## Register it

Four places, all in doze-modules:

1. **`modules.yaml`**: one line with the module's first version.

   ```yaml
   <name>: { path: ./modules/<name>/plugin, version: "0.1.0" }
   ```

2. **`cmd/dzm/meta.go`**: add the driver to the `describers` map, and its
   import. `dzm` refuses to build a module that has no describer.
3. **`scripts/backend-env.sh`**: a `case` arm that fetches the backend from
   doze-binaries and prints `DOZE_<NAME>_BINDIR` (and `DOZE_<NAME>_VERSION`
   when the tests pin a major). Without it the script stops with "has no
   backend mapping".
4. **`.github/workflows/acceptance.yml`**: add the module to the
   `matrix.module` list, so the weekly acceptance run covers it.

Then regenerate the module table in `README.md`. A test fails when it is stale:

```sh
go run ./cmd/dzm readme
```

## The backend comes first

`scripts/backend-env.sh` downloads the backend from doze-binaries releases.
If the engine is not published there, it stops with "no prebuilt … backend in
doze-binaries", acceptance fails, and the module cannot release. That is by
design: a module users can't boot must not ship.

For an engine new to doze, open a pull request to doze-binaries first: a build
recipe in `recipes/<engine>/` and the versions in `versions.yaml`, following
that repo's README. It must build for `aarch64-apple-darwin`,
`aarch64-unknown-linux-gnu` and `x86_64-unknown-linux-gnu`.

Next: [Test it](/contribute/test/).
