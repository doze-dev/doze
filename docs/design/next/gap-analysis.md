# Gap analysis

What exists on 2026-10-10, and what the design needs that it doesn't have.
Paths are relative to the `doze` repo unless they start with `doze-sdk/`,
`doze-modules/` or `doze-names/`.

Each section ends with a verdict: **wire** (the pieces exist and need
connecting), **extend** (a working base needs more), or **new**.

## Summary

| Area | Verdict | The short version |
|---|---|---|
| Config grammar | extend | `use <type> "<name>"` already rewrites to the bare form. `module` is that sugar made mandatory. |
| One registry | delete | A known list of fields and env vars goes away. |
| References | extend | Typed references and dependency edges exist. Namespaces change. |
| Push events | wire | The daemon has an `events` stream. The dash ignores it and polls. |
| State model | extend | Four states. No "crashing", no "done", no "failed". |
| Logs | new | 200 lines in memory, no timestamps, lost on every restart. |
| Wake journal | new | Wakes are free-text lines in a log file. No cause, no duration. |
| Apps on the network | new | Apps bind their own port. doze cannot see or wake them. |
| `$PORT`, `$DATA` | new | The process engine injects nothing. |
| Caller identity | new | No per-connection record at all. |
| Names | extend | doze-names works. Setup, allocation and docs have drifted apart. |
| Services | extend | `process` plus the binary downloader cover most of it. |
| Tasks | new | No run-to-completion concept. |
| File watching | new | No watcher in the repo. |
| Snapshots | new | Only a first-provision template clone exists. |
| Module consoles | extend | `Resources` / `Actions` / `RunAction` are already tables and verbs. No streams, no layout, no URL. |
| Dash structure | new | One 45-field model with boolean modes. Not reusable as a frame. |
| Dash parts | keep | Charts, action registry, format helpers, theme tables, gallery test. |
| Measurement | new | No benchmark or memory check anywhere. |

## Config

- Pipeline: `internal/config/load.go` `LoadWithVars` runs discovery, the `use`
  rewrite, parse and merge, `buildConfig`, instance expansion, `evaluate`,
  then port and domain validation. HCL is `hashicorp/hcl/v2 v2.24.0`.
- A top-level block is an engine instance when its type is not in
  `reservedBlocks` (`defaults`, `tls`, `modules`, `variable`, `locals`,
  `output`). The block type is the engine type.
- `internal/config/use.go` `rewriteUseBlocks` turns `use <type> "<name>"` into
  the bare form with hclwrite before parsing. It rejects `use process`.
- **Constraint.** A plugin decodes its own block: `DecodeRemote` re-parses the
  source and finds the block by type and label (`doze-sdk/plugin/server.go`).
  So the host must keep handing plugins the bare form, or the SDK decoder
  changes. Rewriting on the host needs no SDK change.
- References are plain HCL traversal: `ctx.Variables[type]` is an object of
  instances. Default attributes are `name, engine, address, url, env_var,
  host, port, socket`; plugins add more through `engine.Attributer`.
- A reference creates a dependency edge with condition `healthy`.
  `depends_on` can set `healthy`, `started` or `lazy`.
- There is no `self` object and no templating beyond `${}`.
- `sensitive` on variables and outputs is decoded and never read.
- `doze lint` runs the full load, including module fetch, so it needs the
  network or a warm cache.
- Stale: `doze.lock` in the repo pins `doze/s3`, `doze/sns`, `doze/sqs`, which
  no longer exist, and has no `doze/aws` pin. `initSeed` in
  `cmd/doze/init.go` offers `documentdb`, `s3`, `sqs`, `sns`.

Verdict: **extend**. The keyword is a small host change. `LoadShallow`
(`internal/config/shallow.go`) duplicates the block logic and must change with
it.

## Registry

Everything that supports more than one registry or namespace:

- `hclModules.Mirror`, `hclModule.Source`, `ModulesConfig.Mirror` and
  `.Sources` (`internal/config/decode.go`), and the same logic in
  `shallow.go`.
- The `mirror` parameter of `Manager.Configure` and its three callers
  (`internal/hostboot/hostboot.go`, `cmd/doze/modules.go`,
  `Hooks.ConfigureModules`).
- `DOZE_MODULES_MIRROR`, `Manager.Mirror()`, `Manager.sources`, `sourceFor`,
  `splitSource`, the per-namespace manager and key maps
  (`internal/modules/modules.go`).
- The `keys:` map in the lock and its trust-on-first-use logic
  (`doze-sdk/binaries/lock.go`).
- `docs/design/registry-hosts.md`, which is accepted but unimplemented.

Kept: `DOZE_<TYPE>_PLUGIN` (a local plugin binary, used by module authors),
and the engine-binary mirror variables, which are a different mechanism.

Verdict: **delete**. Decision D11 removes every registry override. The
self-hosting pages under `website/src/content/docs/` go with it.

## Daemon and control protocol

- Transport: a unix socket at `<ProjectDir>/run/doze.admin.sock`,
  newline-delimited JSON, one request per connection, no version handshake
  (`internal/control/server.go`).
- Ops: `status, up, apply, destroy, down, boot, restart, reset, logs, add,
  remove, events, keepawake, resources, admin`.
- Two streams already exist. `logs` with `Follow` is a 250 ms server-side poll
  of the log rings. `events` is a real push from `registry.Subscribe`, but it
  is lossy, fires only on a change of state, health, last error or taint, and
  carries no CPU or memory.
- The dash uses neither. It polls `status` every 500 ms and `logs` every
  400 ms (`internal/tui/tui.go`). Each `status` call shells out to `ps`.
- States (`internal/registry/registry.go`): `reaped`, `booting`, `active`,
  `idle`. "disabled" is a string made up in the status handler.
- A service in restart backoff is `reaped` with a `LastError`. It cannot be
  told from a stopped one. Restart count and pin changes emit no event.
- Supervised processes sit in `idle` forever.
- Not tracked: boot duration, wake cause, exit code, total connections, bytes.
- One daemon per project. Running stacks are listed in `<home>/stacks.json`;
  stopped ones are only directories under `<home>/projects/`.

Verdict: **wire** for push, **extend** for states, **new** for the journal.

## Logs

- `internal/supervisor/ring.go`: 200 lines per process, in memory.
- No timestamps, no stdout or stderr tag, no per-line size cap.
- The ring belongs to the process, so logs vanish on sleep, restart or crash.
  Asking for the logs of a stopped instance is an error.
- Hook output is dropped unless the hook fails.
- The dash replaces its whole log buffer on every poll. It has no search
  outside copy mode, no filter, no wrap, and does not handle ANSI codes.

Verdict: **new**. The logs, copy, "earlier runs", task and crash screens all
depend on a real log store.

## Proxy and ports

- One listener per declared, enabled, non-supervised instance
  (`internal/proxy/proxy.go`, `internal/daemon/dynamic.go`).
- Engines bind a private unix socket under `<ProjectDir>/run/<name>/`. The
  proxy copies bytes both ways. Protocol-aware work is optional and belongs to
  the driver (`engine.ProxyFilter`, `engine.WireProxy`).
- Supervised `process` instances are not proxied. They bind
  `127.0.0.1:<port>` themselves.
- Ports are never assigned: `doze does not auto-assign ports`
  (`internal/config/instance.go`). `netutil.FreePort` exists but is only used
  as a probe.
- Per connection, only a counter is kept. No peer address, no peer pid, no
  open time.
- The dependency hold is real: a dependant `Acquire`s each dependency while it
  runs (`Runtime.bootDeps`). That is a truthful "held awake by" for declared
  references, available today.

Verdict: **new** for proxied apps, private ports and identity. "Held by" can
be served from dependency holds without any identity work.

## Names

See [networking.md](networking.md) for the detail. In short:

- doze-names provides the registry (`<home>/names.json`, file-locked), a UDP
  resolver, the shared `:80` front door, and per-OS setup.
- doze pins doze-names at a September commit and marks it indirect. HEAD has
  since added a loopback-only check on `:80`, a Windows build, an off switch
  and a reachability check. doze calls none of them.
- `doze dns-setup` re-implements setup instead of calling `names.Install`. On
  Linux it prints "no setup needed" and installs nothing.
- Two allocators hand out the same address range from two different files
  with no coordination. Only `names.json` is locked.
- The pool is 55 addresses for the whole machine.
- The zone is shared with standalone doze-aws and doze-kafka, which call
  doze-names directly. The three are on three different doze-names versions
  (doze: a September commit; doze-aws: v0.1.0; doze-kafka: HEAD), and any of
  them may be the process serving DNS and `:80` for the others.
- There is no end-to-end test through the OS resolver, and none across the
  three tools.
- There is no HTTPS, and no way to name a proxied engine's extra HTTP port.

Verdict: **extend**. The design is sound; the edges have drifted.

## Services, tasks, watching, snapshots

- `engine/process` already has `command`, `cwd`, `env`, `env_file`, health
  probes, restart with backoff, and hooks. `pre_start` runs on every boot,
  `post_start` after ready, `pre_stop` before stop. These are close to `run`,
  `on_ready` and `stop`. There is nothing that runs once per data directory.
- `doze-sdk/binaries` downloads an archive, checks a sha256, unpacks and
  caches per platform (`EnsureArtifact`). Limits: tar.gz only, the archive
  must contain `bin/`, and it is read into memory. A warm cache is not
  re-verified.
- No task concept. A process that exits 0 is left `reaped` with the error
  "backend exited unexpectedly".
- No file watcher.
- Reset removes the data and socket directories
  (`internal/runtime/apply.go`). There is no snapshot or restore.

Verdict: **extend** for services, **new** for tasks, watch and snapshots.

## Module consoles

- The SDK has an admin trio (`doze-sdk/engine/admin.go`): `Resources` returns
  rows of `{Kind, Name, Status, Info map}`, `Actions` returns verbs, and
  `Run` executes one. All are unary and host-polled.
- The kafka page uses it: the dash asks the daemon, the daemon asks the
  plugin, the plugin asks the broker.
- The aws page does not. The dash makes its own HTTP call to
  `/_console/api/glance` on the service.
- Which engines have a page, their console URLs and their column meanings are
  hard-coded in the dash in at least six places (`hasEnginePage`, `webURL`,
  `consoleSlug`, `glanceData`, the `Info` keys, `groupOrder`).
- `ProtocolVersion` is 1. doze uses doze-sdk v0.7.0; doze-modules uses v0.6.0.

Verdict: **extend**. The rows and verbs exist. Missing are a layout
description, a stream, and a console URL.

## Dash and CLI

- `internal/tui` is 4194 non-test lines. One `model` struct of about 45
  fields holds every page's state, with boolean mode flags and one key
  handling chain (`view_home.go`, 1083 lines).
- The dash calls a concrete `*control.Client`. There is no interface, so a
  fake daemon cannot be substituted.
- Layout is hand arithmetic, and mouse hit-testing repeats the constants.
  Minimum size is 64 by 18.
- Themes: five (`violet`, `emerald`, `amber`, `cyan`, `rose`) with real hex
  tables in `theme.go`. Only violet has a light variant. Styles are package
  globals.
- Copy writes to the system clipboard only. There is no OSC 52, so copying
  over SSH does not work.
- Confirm exists for sleep, restart and reset. It is a yes/no with one line
  of consequence.
- Tests are white-box and assert with `strings.Contains`. There are no golden
  files.
- Bare `doze` prints help. The root command has no `Run`, although its own
  help text says it opens the dash.
- `wake`, `sleep`, `restart`, `reset`, `pin`, `logs`, `url` exist only as dash
  actions, not as CLI commands.
- `doze init` asks three questions and detects nothing in the repo.
- `doze sync` diffs objects (buckets, topics, roles) by hash. It has no
  attribute-level diff and no view of which services restart.

Reusable as they are: `internal/actions`, `charts.go`, `history.go`,
`format.go`, the theme hex tables, `ui.Table`, and the gallery test.

Verdict: **new** structure, **keep** the parts.

## Bugs found on the way

Not part of the redesign. Each was re-checked against the code before being
touched (decision D24).

| # | Bug | Status on 2026-10-10 |
|---|---|---|
| 1 | Sleep-all (`Down("")`) calls `StopAll` without `BeginShutdown`, so a crash-restart could respawn a process mid-sweep. | **Not a bug.** `StopAll` re-takes its snapshot, pending restarts included, until nothing is left, and `BeginShutdown` is one-way on purpose so that later connections can still wake services. A respawn mid-sweep is caught by the next pass. |
| 2 | Mouse clicks in the APPS view are hit-tested against the hidden sidebar and can enter copy mode. | Open. Goes away with the dash rebuild (phase 5). |
| 3 | The reset confirmation showed a blank consequence line. | **Fixed** in `internal/tui/view_home.go`, with `TestConfirmModalStatesTheConsequence`. |
| 4 | On Linux, `doze doctor` told users to route to `127.0.0.1:5323`; doze-names listens on `192.0.2.53:5323` there. Only doctor was wrong; the daemon message already used `names.ResolverAddr()`. | **Fixed** in `cmd/doze/doctor.go`. |
| 5 | A removed instance does not return its loopback address until the daemon exits. | Open. Fixed in phase 3 (N3). |
| 6 | A warm binary cache was never compared with the lock. | **Fixed** in `doze-sdk/binaries/binaries.go`, with `TestCacheHitHonoursTheLock`. It compares the digest recorded at unpack time, since the archive is gone. It reaches doze at the next doze-sdk release. |

Found while bringing up the fixture stack, and still open:

| # | Bug | Where |
|---|---|---|
| 7 | `--var` is not passed to the daemon. `doze up --var domains=false` starts a daemon that uses the default. `DOZE_VAR_<name>` works, because the daemon inherits the environment. | `internal/daemonctl`: the daemon is started with `--config` only |
| 8 | A long `DOZE_HOME` makes the control socket path exceed the unix socket limit, and the daemon dies with `bind: invalid argument`. | `internal/control/server.go` |
| 9 | Three files in `internal/tui` are not gofmt-clean under the current Go, so `make check` fails before any test runs. | **Fixed.** |
| 10 | The registry served aws module 0.1.1 although `doze-modules/modules.yaml` says 0.1.2. Not checked further. | registry index, or an unpublished release |
| 11 | `TestAllocatorDistinctAndStable` expects the first address to be `127.0.0.2`; the allocator starts at `.10`. It is skipped where the loopback aliases are absent, so CI never runs it, and it fails on a machine that has run setup. | **Fixed**: the test now expects `.10`. |
| 12 | `doze init` wrote bare `<type> "<name>"` blocks for every module it took from the registry catalog, so its output stopped parsing the moment the `module` keyword became required. | **Fixed** in `cmd/doze/init.go`, with `TestInitWritesAConfigDozeReads`. |
| 13 | Eight doc pages and `doze run --help` said doze injects connection strings into a supervised app. It never did: `engine/process` passes on only the environment the block declares. | **Fixed** in the docs and the help text. |
| 14 | Eleven `hcl` examples in the docs never parsed (one-line blocks with several arguments, `;` separators, a call to `index()`, which doze does not have). | **Fixed**, and `TestDocExamplesParse` now checks every example. |
| 15 | The aws module's published example is not valid HCL (see phase 1 status). | `doze-modules/modules/aws/describe.go` |
