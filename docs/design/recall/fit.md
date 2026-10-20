# How recall fits into doze

Status: design, 2026-10-20. Code references checked against `doze` at
`142af83`.

Recall is doze looking at the stack, alongside the daemon, the proxy, `.doze`
names and the dash. It is not one of the four nouns: one per stack, turned on
by a block, with reads no module or app is allowed (every app folder, the
resolved config, the journal, logs). Most of what it needs to run already
exists. Two pieces do not: a way for the daemon to own a built-in process,
and a change feed over whole source trees.

## What recall reuses

| Recall needs | doze has today | Where |
|---|---|---|
| Sleep until called, wake on the first connection | The proxy boots a backend on connect, holds it while connections are open, releases after | `internal/proxy/proxy.go:127` (`handle`), `:213` (`bootBackend`), `:206` (`Acquire`/`Release`) |
| One lifecycle, not a second one | Registry states booting → active → idle → reaped, with crash backoff | `internal/registry/registry.go:14`, `Acquire` `:279`, `Reapable` `:315` |
| Supervision, events, logs | Supervisor, journal with `Subscribe`, log store (5 runs or 10 MB) | `internal/supervisor/supervisor.go`, `internal/journal/journal.go:196`, `internal/logstore/` |
| A name agents can reach | `<name>.<stack>.doze` from the daemon's resolver | `internal/config/domains.go` |
| The declared stack | The parsed config: instances, kinds, references, app `dir` and `env` | `internal/config/config.go:23`, `runnable.go:33`, `eval.go` |
| A config block | Top-level blocks that are not instances | `reservedBlocks`, `internal/config/load.go:309` |
| A memory budget | Idle daemon and idle tree checked in CI on darwin | `test/bench/budget.json`, `test/bench/bench_test.go` |

The proxy's hold-while-connected rule is what makes "asleep by default" free:
an agent session with an open MCP connection holds recall awake, and when the
last one closes recall idles out like any service.

## What has to be added

### 1. The `recall {}` block

```hcl
recall {
  exclude = ["**/*_generated.go", "fixtures/**"]   # .gitignore is always respected
  include = ["docs/**", "adr/**"]                   # extra prose, beyond READMEs
  agents  = ["claude", "codex"]                     # optional: who may run generation here
}
```

Its presence turns recall on. `recall` joins `reservedBlocks`, and the name
`recall` is reserved as an instance name whether the block is there or not, so
turning recall on never collides with an app someone already called that.
Every app with a `dir` is indexed; downloaded services and modules have no
source and are skipped. `agents` is a team policy: the agent CLIs allowed to
run generation on this stack (R51). Nothing else is configurable. Which agent
a person uses is their own setting, kept in the doze home
([generation.md](generation.md)).

### 2. A built-in instance the daemon owns

There is no hook for a process that `doze.hcl` doesn't declare.
`Daemon.AddInstance` (`internal/daemon/dynamic.go:131`) parses an HCL block
and wires it into the proxy and DNS, and services already become synthetic
`InstanceDecl`s (`internal/config/runnable.go:326`). Recall follows the
second pattern: when the block is present, config loading adds a synthetic
instance named `recall`, running the `doze-recall` binary with the process
engine, proxied at `recall.<stack>.doze`, sleeping after the idle time. It
then gets states, crash backoff, the journal and logs with no new code.

It differs from a user service in three ways:

- It is hidden from the instance rows in the dash and shown as a strip in the
  header instead.
- It is excluded from `doze up` (nothing wakes it but a call).
- It gets a stack hand-off at start (below).

### 3. The stack hand-off

Recall needs more than the public `Inspection.Topology()` gives
(`inspect.go:49`). That returns instance names and boot order (`types.go:56`)
and nothing about which attribute flows where. Recall needs the
attribute-level wiring: `app.api` env `DATABASE_URL` comes from
`module.db.url`, `app.worker` env `JOBS_QUEUE` comes from
`module.cloud:queue.jobs.url`. That is the backbone of the stack join.

The daemon starts recall with:

- The resolved stack as JSON: every instance with kind, type, `dir`, the
  resources nested in each module, and every reference with its source block,
  attribute and the env var or argument it lands in.
- The daemon socket, for reading the journal and subscribing to changes.
- A contract version, so a newer doze can refuse an older recall binary.

The JSON is written by doze, from `internal/config`, so recall never parses
HCL and the two can't disagree about what the stack is.

### 4. A change feed over source trees

Today's watcher (`internal/watch/poll.go:72`) checks the size and mtime of an
app's own `watch` globs once a second, only while that app is awake. It was
chosen over the kernel watcher because kqueue on macOS holds a descriptor per
file (`../next/phases.md`, phase 4.6). Recall needs every file of every app,
asleep or awake. How that works is in [freshness.md](freshness.md). The feed
is owned by the daemon and shared with `watch`, so a large repo is looked at
once, not twice.

### 5. A status file for the header strip

The dash shows recall's state (asleep, indexing, fresh, n to review) in its
header. Asking recall would wake it. Instead, recall writes
`<ProjectDir>/recall/status.json` whenever its state changes, and the dash
reads the file.

### 6. One headless command

Recall's interactive actions live in the dash. MCP is the programmatic
surface for agents. CI needs a third way in: `doze recall check` builds
what it can of the graph in the checkout and fails on notes, glossary rows or
decisions that don't resolve, on drifted decisions, and on secrets in them
([context.md](context.md) §8). It never runs an agent. This is the one
exception to "dash only", and `cmd/doze/parity_test.go` needs to know about
it.

### 7. Helpers recall starts

Language servers ([graph.md](graph.md) §4.4) are hidden services that recall
adds through the control socket's `add` once it knows an app's language. Four
small pieces make that work:

- **A `hidden` flag on an instance.** The dash rows, `doze status`,
  `doze up` and `doze env` skip hidden instances; the Overview in recall's
  screen shows them.
- **`doze __lspbridge`.** A hidden subcommand, like `__fetch`. It listens on
  the service's port and pipes one connection to the server's stdio. Servers
  speak stdio; the proxy wakes things on a connection.
- **Downloads that skip `doze.lock`.** The service `download` path records
  each URL in the person's committed lock on first fetch
  (`internal/runtime/runtime.go:347-354`). Helpers use recall's own pinned
  table through `__fetch --sha` instead, mirrored in doze-binaries so
  `DOZE_MIRROR` works.
- **A gunzip fallback** in `doze-sdk/binaries/download.go`, since
  rust-analyzer ships a gzipped binary, not a tarball.

### 8. Declared resources and env provenance

Two hand-off gaps found in the code:

- Which attribute fed which env key is lost after `evaluate`. Keep the map.
- The queues, topics and buckets inside the aws and kafka modules are opaque
  to doze core. Not `Inventory.Objects`: both plugins converge, so answering
  it would make plan and sync diff their resources. Instead, a read-only
  `Declared` capability in doze-sdk, implemented by doze-aws and doze-kafka,
  returns declared resources with the attributes doze mints
  ([graph.md](graph.md) §4.1).

The daemon writes the hand-off to `<ProjectDir>/recall/stack.json` on every
config load, so recall picks up `doze.hcl` changes without the daemon
restarting it.

The helpers in §7 are part of v1: the TypeScript/JavaScript, Python and Rust
packs need them.

## Where things live

| What | Where | Committed |
|---|---|---|
| `recall {}` | `doze.hcl` | yes |
| Stack notes, `glossary.md`, `topology.yaml` (decisions) | `recall/` beside `doze.hcl` | yes |
| Notes about a repo's code | `<repo>/.recall/` | yes, in that repo |
| Proposals waiting for review | `<ProjectDir>/recall/proposals.json` | no |
| Graph, search indexes, query log | `<ProjectDir>/recall/graph.db` | no, disposable, rebuilt on schema change |
| Status for the header | `<ProjectDir>/recall/status.json` | no |
| Extraction caches, keyed by content hash | `~/.doze/recall/cache/` | no |
| Logs | the log store, like any instance | no |

Recall is a second Go module in this repo (`recall/`), built as its own
binary in the same release. Contract changes land in one commit, and recall's
dependencies (a SQLite driver, tree-sitter grammars) stay out of the daemon.

## Connecting an agent

The design record registered a per-stack URL (`recall.shop.doze/mcp`) once per
user under one fixed name. That breaks as soon as a person has two stacks:
every project gets whichever URL was registered last. There are two ways out:

- **Per project, not committed.** `claude mcp add --scope local` stores the
  server for one project on one machine. It works today, but it is one
  command per stack per person, and every client has its own equivalent.
- **One machine-wide address** (preferred). The daemon serves
  `recall.doze/mcp` and routes each session to the right stack from the
  client's MCP roots or working directory. One registration, once, for every
  stack. It also answers the design record's open question about the address
  when `domains = false`: the daemon picks a fixed loopback port.

Either way, doze never edits agent files. The Connect tab shows the line to
copy, and an optional `AGENTS.md` section that tells agents to use the
`doze-recall` tools if they are present and to ignore the section if not, so
it is safe to commit for teammates without doze.

## Budget

Asleep, recall is not running and costs nothing against `idle_tree_kb`.
"Asleep" means no agent session is connected: an open session holds recall
awake through the proxy. The bench needs three new entries, all with
proposed starting values in [context-system.md](context-system.md) §7:

- awake and serving (≤ 80 MB up to 1M edges);
- indexing peak of the recall process (≤ 300 MB);
- save-to-fresh latency.

Language servers run one at a time, typically 0.1–0.7 GB while they work.
They sleep 5 minutes after their last batch, on the normal idle timer.

## Open

- An agent session left open all day holds recall awake all day. Is that
  acceptable, or does recall drop its in-memory graph after a quiet period
  while keeping the connection?
- Agents in sandboxes (Claude Code's sandbox, cloud agents) may not resolve
  `.doze` names or reach loopback. Find out what each one allows before
  picking the address.
- Is a machine-wide cache under `~/.doze` acceptable for teams with strict
  data rules, or must everything stay inside the project?
