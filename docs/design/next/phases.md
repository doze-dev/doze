# Phases

The build order, in units small enough to review one at a time.

## Reading the estimates

Estimates are focused working days for one person. They are ranges because
they come from a survey of the code, not from having changed it. Each phase
starts by re-reading its own files and replacing its range with a number.

| Phase | What ships | Days | Depends on |
|---|---|---|---|
| 0 | These documents, and the open decisions settled | 2 to 3 left | none |
| 1 | The config language | 5 to 7 | none |
| 2 | Daemon events, logs, and the CLI on top | 9 to 13 | none |
| 3 | Names and ports | 9 to 13 | none |
| 4 | Services, apps and tasks | 9 to 13 | 1, 2, 3 |
| 5 | The dash | 14 to 20 | 2 |
| 6 | Module consoles | 8 to 12 | 5 |
| 7 | add, init, doctor, sync | 7 to 10 | 1, 4, 5 |
| | Total | 63 to 91 | |

Phases 1 and 2 do not depend on each other. Phase 5 can start as soon as
phase 2 is done, alongside phases 3 and 4.

## Milestones

| Milestone | Phases | What a user gets |
|---|---|---|
| M1 | 1, 2 | The new language and a full CLI. The old dash still runs. |
| M2 | 3, 4 | Outside services, tasks, `$PORT`, reliable names. |
| M3 | 5 | The new dash. |
| M4 | 6, 7 | Consoles and the guided flows. |

Each milestone leaves `main` releasable.

## Phase 0: finish the plan

| Unit | Work |
|---|---|
| 0.1 | Owner reads these documents and corrects anything that misreads the intent. The only unit left. |
| 0.2 | Done on 2026-10-10: decisions D7 to D14 in [README.md](README.md). |
| 0.3 | Done on 2026-10-10: decisions D15 to D22. |
| 0.4 | Done on 2026-10-10: `examples/shop` runs end to end on today's doze (page load, an order through postgres and kafka, a receipt in S3). |
| 0.5 | Done on 2026-10-10: three fixed, one found not to be a bug. Five more were found and are listed in [gap-analysis.md](gap-analysis.md#bugs-found-on-the-way). |

Exit: every open question has an answer and a date.

## Phase 1: the config language

Contract: [config.md](config.md).

| Unit | Work | Files |
|---|---|---|
| 1.1 | Parse `module <type> "<name>"`. Generalise the `use` rewrite into it. | `internal/config/use.go`, `load.go`, `shallow.go` |
| 1.2 | Parse `app`, `service`, `task` headers and their attributes. Behaviour of `app` is today's `process`; `service` and `task` parse and lint but refuse to run until phase 4. | `internal/config/load.go`, `instance.go`, `engine/process/config.go` |
| 1.3 | References by kind: `module.x`, `service.x`, `app.x`, `task.x`. Unique names within a kind. `after`. | `internal/config/eval.go`, `foreach.go` |
| 1.4 | Remove the `modules` block, mirror, source and namespaces. Compile the registry key in. Withdraw the registry-hosts design and the self-hosting docs. | `internal/config/decode.go`, `shallow.go`, `internal/modules/modules.go`, `internal/hostboot/hostboot.go`, `cmd/doze/modules.go`, `doze-sdk/binaries/lock.go`, `docs/design/registry-hosts.md` |
| 1.5 | Offline lint: the shape pass. | `internal/config/lint.go` (new), `cmd/doze/lint.go` |
| 1.6 | Remove the old forms outright: bare module blocks, `process`, `use`, `modules`. Port every config in the repo, the tests and the website docs by hand. | `internal/config/use.go`, `test/`, `website/src/content/docs/` |
| 1.7 | Clean the stale lock pins and the `initSeed` list. Migrate the repo's own `doze.hcl` and `examples/shop`. | `doze.lock`, `doze.hcl`, `cmd/doze/init.go`, `examples/shop` |

Exit measures:

- `examples/shop` and the repo's `doze.hcl`, ported by hand, run the same
  stack as before.
- `doze lint` passes with the network off.
- No file in the repo uses an old form, and an old form fails lint with a
  message naming the new one.

## Phase 2: daemon events, logs, and the CLI

Contract: [daemon-api.md](daemon-api.md).

| Unit | Work | Files |
|---|---|---|
| 2.1 | Measure first: idle daemon RSS and wake latency on the fixture stack, in CI. | `test/bench/` (new), `.github/workflows/test.yml` |
| 2.2 | States: `crashing`, `failed`, `done`. Events for restart count and pin. Fix apps stuck in `idle`. | `internal/registry/registry.go`, `subscribe.go`, `internal/runtime/runtime.go` |
| 2.3 | Holders in the snapshot. | `internal/registry/registry.go`, `internal/control/types.go`, `internal/daemon/handler.go` |
| 2.4 | The journal, with cause and duration. | `internal/journal/` (new), `internal/runtime/runtime.go`, `internal/proxy/proxy.go` |
| 2.5 | The log store, replacing the ring. | `internal/logstore/` (new), `internal/supervisor/supervisor.go`, `ring.go` |
| 2.6 | Protocol 2: handshake, multiplexed connection, `subscribe`. `stats` sampled only when subscribed. | `internal/control/server.go`, `client.go`, `types.go` |
| 2.7 | The `Daemon` interface and `fakedaemon`. | `internal/control/daemon.go` (new), `internal/control/fakedaemon/` (new) |
| 2.8 | CLI: bare `doze`, `up` progress, `status`, `logs`, `wake`, `sleep`, `restart`, `reset`, `pin`. Parity test extended. | `cmd/doze/main.go`, `up.go`, `tree.go`, `tree_render.go`, `logs.go` (new), `verbs.go` (new), `parity_test.go` |
| 2.9 | Point the old dash at `subscribe` so it stops polling. A small change that keeps it alive until phase 5. | `internal/tui/tui.go` |

Exit measures:

- Idle RSS with nothing subscribed is no higher than the 2.1 baseline.
- `doze logs api` works while `api` is asleep, and shows the previous run.
- A crash-looping app shows as `crashing` with an attempt count in
  `doze status`.
- CLI output for `up`, `status` and `logs` matches golden files produced from
  `fakedaemon`.

## Phase 3: names and ports

Contract: [networking.md](networking.md).

| Unit | Work | Files |
|---|---|---|
| 3.0 | Add a format version to `names.json`. | `doze-names/registry.go`, `doze-names/README.md` |
| 3.1 | An end-to-end resolver test on macOS and Linux runners, before changing anything. It runs doze, standalone doze-aws and standalone doze-kafka together, in each start order. | `test/integration/names_test.go` (new), `.github/workflows/integration.yml` |
| 3.2 | doze-names: one allocator behind the registry lock; return addresses on release; widen or re-scope the pool. | `doze-names/registry.go`, `names.go` |
| 3.3 | doze: drop `internal/loopback`'s own file; lock or remove `stacks.json`; bump the pin. | `internal/loopback/`, `internal/daemon/bindplan.go`, `stacks.go`, `go.mod` |
| 3.4 | Setup through `names.Install`. `doze doctor --fix`. Correct Linux messages. | `cmd/doze/dnssetup.go`, `doctor.go`, `internal/daemon/domains.go` |
| 3.5 | Private ports: proxy listeners for commands, `$PORT` and `$HOST`. For apps, the extra `localhost` listener when the port is free, and `direct = true`. | `internal/daemon/bindplan.go`, `dynamic.go`, `internal/proxy/proxy.go`, `engine/process/process.go` |
| 3.6 | Front-door routes in `names.json` only, made automatically, and they wake. | `doze-names/ingress.go`, `internal/daemon/ingress.go`, `awsingress.go` |
| 3.7 | Console listeners. | `internal/daemon/dynamic.go`, `doze-names/registry.go` |
| 3.8 | Per-service `sleep`. | `internal/runtime/runtime.go`, `internal/registry/registry.go` |
| 3.9 | doze-names README and the names spec corrected. | `doze-names/README.md`, `notes/launch/07-names-spec.md` |
| 3.10 | doze-aws, doze-kafka and doze-modules moved to the same doze-names version, and the address table for module versus standalone written into their docs. | `doze-aws/go.mod`, `doze-kafka/go.mod`, `doze-modules/go.mod`, their `docs/` |

Exit measures:

- The resolver test passes on both runners, with the standalone tools
  running beside doze in each start order.
- Two postgres instances on 5432 in one stack.
- An app started with `$PORT` is reachable at `api.shop.doze` with and without
  setup having been run (by name in the first case, on `127.0.0.1` in the
  second).
- Proxy overhead on a dev server is measured and written down.

## Phase 4: services, apps and tasks

Contract: [lifecycle.md](lifecycle.md).

| Unit | Work | Files |
|---|---|---|
| 4.1 | One command engine with three presets. `service` wakes on connect and sleeps. | `engine/process/` becomes `engine/command/`: `config.go`, `command.go` |
| 4.2 | Steps: `setup` once, `on_ready`, `stop`. Step events and step logs. SIGTERM by default. | `engine/command/steps.go` (new), `internal/supervisor/supervisor.go` |
| 4.3 | Download: zip and bare binaries, streamed hashing, `downloads:` in the lock, verify on wake. | `doze-sdk/binaries/binaries.go`, `lock.go`, `engine/command/download.go` (new) |
| 4.4 | Tasks: run to completion, `inputs` hashing, `after`, `manual`, state and history. | `engine/command/task.go` (new), `internal/runtime/tasks.go` (new), `internal/state/` |
| 4.5 | `doze task <name>`. | `cmd/doze/task.go` (new) |
| 4.6 | `watch`, and input re-checks while running. | `internal/watch/` (new) |
| 4.7 | The fixture stack gains mongo (download), mailpit (installed), a container service, and two tasks. | `examples/shop` |

Exit measures: the four in [lifecycle.md](lifecycle.md#measures), run in CI
against `examples/shop`.

## Phase 5: the dash

Contract: [screens.md](screens.md) pages 1 to 3. Starts after phase 2.

New package beside the old one. `internal/tui` is deleted in the last unit.

```
internal/dash/
  app.go          root model: router, overlays, key bar
  page.go         the Page and Tab interfaces
  theme/          theme as a value, all five with light variants
  layout/         one description used for both drawing and hit-testing
  home/           the stack list and the preview pane
  frame/          the per-row frame: header, tabs, shared keys
  tab/logs/       log buffer, search, levels, runs
  tab/overview/   address, holders, vitals, recent
  tab/lastrun/    task output and history
  tab/web/        a console link
  overlay/palette/
  overlay/confirm/
  overlay/copy/   copy mode, copy menu, OSC 52
  testdata/golden/
```

Moved in unchanged: `charts.go`, `history.go`, `format.go`,
`internal/actions`.

| Unit | Work |
|---|---|
| 5.1 | Skeleton: `Page` and `Tab` interfaces, router, theme as a value, layout description. The golden harness on `fakedaemon`, with one screen under test. Built on the latest stable Charm libraries (D9). |
| 5.2 | Home at full size, with the preview pane. |
| 5.3 | The frame, and the logs tab for an app. |
| 5.4 | Overview tab for modules. Last-run tab for tasks. |
| 5.5 | Stack-wide logs with search and source toggles. |
| 5.6 | Copy mode, the copy menu, OSC 52. |
| 5.7 | Confirmations, including type-the-name. |
| 5.8 | Palette, showing the CLI form of each row. |
| 5.9 | Themes: picker, live preview, light variants. |
| 5.10 | Home at 80 × 24, and the crash state on home. |
| 5.11 | Switch `doze` to the new dash and delete `internal/tui`. |

Exit measures:

- A golden file for each core and should screen on pages 1 to 3, at 120 × 36
  and at 80 × 24.
- No file in `internal/dash` imports the socket client; only the `Daemon`
  interface.
- Keystroke-to-frame time on home with 30 instances, measured and recorded.

## Phase 6: module consoles

Contract: [consoles.md](consoles.md).

| Unit | Work | Files |
|---|---|---|
| 6.1 | `Console` interface, `DescribeConsole` and `Tail` in the protocol, version 2. | `doze-sdk/engine/console.go` (new), `doze-sdk/plugin/proto/engine.proto`, `plugin/server.go`, `host.go`, `plugin.go` |
| 6.2 | The `console` topic in the daemon. | `internal/daemon/handler.go`, `internal/control/types.go` |
| 6.3 | The generic console tab: tables, counters, streams, actions. | `internal/dash/tab/console/` (new) |
| 6.4 | kafka: describe, tail, produce. | `doze-modules/modules/kafka/console.go` (new), `admin.go` |
| 6.5 | aws: resources, services in use, the wire, moved behind the module. | `doze-modules/modules/aws/console.go` (new), `admin.go` |
| 6.6 | Web fronts: `WebURL` and `console =`. The runtime marker. | `internal/dash/tab/web/`, `internal/dash/tab/console/` |

Exit measures: the three in [consoles.md](consoles.md#measures), plus golden
files for both consoles.

## Phase 7: add, init, doctor, sync

| Unit | Work | Files |
|---|---|---|
| 7.1 | `doze add`: the catalog, writing a block with hclwrite, the wiring line. | `cmd/doze/add.go` (new), `internal/config/edit.go` (new) |
| 7.2 | `doze add <unknown>`: the service path. | `cmd/doze/add.go` |
| 7.3 | Detection: Go, Node, compose files, Makefile targets, `.env.example`. | `internal/detect/` (new) |
| 7.4 | `doze init` on detection, and the first-run screen. | `cmd/doze/init.go`, `internal/dash/home/` |
| 7.5 | `doze sync`: block-level diff and what restarts. | `cmd/doze/sync.go`, `planview.go`, `internal/state/plan.go` |
| 7.6 | Doctor: who holds a port, and the fix options. | `cmd/doze/doctor.go` |

Exit measures:

- `doze init` in a copy of `examples/shop` with its `doze.hcl` removed
  recreates a file that runs the stack.
- `doze add postgres` and `doze add mongo` each produce a file that passes
  lint.

## After phase 7

Dream-tier items, each a time-boxed experiment with a keep-or-cut result:

- Caller identity by peer PID lookup.
- Snapshots by data-folder copy.
- Keep in doze.hcl.
- HTTPS for `.doze` names.
