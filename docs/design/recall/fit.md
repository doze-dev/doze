# How doze feeds recall

Status: design, 2026-10-20; rewritten 2026-10-21 when recall became
independent of doze (R103). Code references checked against `doze` at
`142af83`.

Recall is its own program, `doze-recall`, and needs nothing from doze to run
([running.md](running.md)). doze makes it better in three ways:

1. **It writes a declared-stack file** from `doze.hcl`, so recall's links
   between apps come from declarations instead of being inferred from names
   in code (§2).
2. **It ships a pinned copy** and a `doze recall` command, so a doze user
   never installs or updates recall separately (§3).
3. **Its dash shows recall's screen**, a client of recall's local API (§4).

doze doesn't run recall as one of its instances, doesn't proxy it, and
doesn't feed it file changes. Recall is started by the agent, over stdio,
like any MCP server.

## 1. The `recall {}` block

```hcl
recall {}
```

Its presence turns on the stack file and the dash's recall screen. It takes
no keys: recall's settings (`include`, `exclude`, `agents`, `roots`) live in
`.recall/recall.toml`, so they are the same with or without doze
([running.md](running.md) §2).

`recall` joins `reservedBlocks` (`internal/config/load.go:309`), and the name
`recall` is reserved as an instance name whether the block is there or not.

## 2. The declared-stack file

doze writes `.doze/recall/stack.json` (its own gitignored folder, beside
`doze.hcl`):

- on every config load, while the daemon runs;
- on `doze recall stack`, with no daemon, built from the config in-process.

The format is recall's, published with a version ([graph.md](graph.md)
§4.1). doze is its first producer, not its only possible one. The file is
built in `internal/config`, so recall never parses HCL and the two can't
disagree about what the stack is.

Recall needs more than the public `Inspection.Topology()` gives
(`inspect.go:49`), which returns instance names and boot order
(`types.go:56`) and nothing about which attribute flows where. Recall needs
the attribute-level wiring: `app.api` env `DATABASE_URL` comes from
`module.db.url`, `app.worker` env `JOBS_QUEUE` comes from
`module.cloud:queue.jobs.url`. Three gaps to fill:

- **Which attribute fed which env key** is lost after `evaluate`. Keep the
  map: walk each env expression's traversals.
- **The queues, topics and buckets inside the aws and kafka modules** are
  opaque to doze core. Not `Inventory.Objects`: both plugins converge, so
  answering it would make plan and sync diff their resources. Instead, a
  read-only `Declared` capability in doze-sdk, implemented by doze-aws and
  doze-kafka, returns declared resources with the attributes doze mints.
  This is a doze-side addition; recall only reads the file it ends up in.
- **Sources and logs.** The file lists the files it was made from with their
  hashes (`doze.hcl`, env files), so recall can tell when it is stale
  ([running.md](running.md) §5), and the paths of the journal
  (`run/journal.jsonl`) and each app's log store, for `diagnose`.

## 3. The pinned copy and `doze recall`

doze pins an exact `doze-recall` version and SHA-256 in its build. The first
time a `doze.hcl` has `recall {}`, doze downloads it (through `DOZE_MIRROR`
when set), checks it, and keeps it in its cache. The download never touches
the person's `doze.lock` (R86).

`doze recall <args>` writes the stack file if it is out of date and runs the
pinned binary with the same arguments: `serve`, `review`, `status`,
`generate`, `connect`, `check`. So a doze user's connect line is:

```
claude mcp add doze-recall -- doze recall serve
```

`doze recall check` is the CI check ([context.md](context.md) §8): it writes
the stack file without a daemon and runs `doze-recall check`. Recall's
dependencies (a SQLite driver, tree-sitter grammars) stay out of the doze
binary. `cmd/doze/parity_test.go` needs to know about `doze recall`, which
has no dash twin for `serve` and `check`.

## 4. The dash

- **The header strip** shows recall's state (asleep, indexing, fresh, n to
  review) from `doze-recall status --json`, which reads `status.json` from
  disk and never starts recall.
- **Recall's screen** has the tabs Overview, Notes, Review, Topology,
  Queries, Connect and Generate ([context.md](context.md) §7). Each is a
  client of recall's local API ([running.md](running.md) §4). Opening a tab
  that needs the graph starts recall's background process, since the person
  asked for it.
- **Writes** happen inside recall, on the person's keypress, the same as in
  `doze-recall review`. The dash sends the key; recall does the write.

## 5. What doze no longer needs

Earlier drafts had doze host recall. Each of these is dropped:

| Dropped | Because |
|---|---|
| A hidden built-in recall instance, woken by the proxy | the agent starts recall over stdio |
| A reserved address and per-stack routing (the old P0.1) | stdio has no address |
| A daemon-owned change feed shared with `watch` | recall watches files itself; `watch` keeps its poller |
| Hidden helper services and `doze __lspbridge` for language servers | recall runs them as child processes |
| A download path for helpers that skips `doze.lock` | recall downloads its own servers |
| A gunzip fallback in `doze-sdk/binaries` | it lives in recall |

What remains on doze's side: the block, the stack file with env provenance
and `Declared`, the pinned copy and `doze recall`, the dash screen.

## Budget

Recall isn't a doze instance, so doze's idle bench (`test/bench/budget.json`)
doesn't change: with no agent session open, no recall process runs. Recall's
own budgets (awake and serving, indexing peak, save-to-fresh latency) are in
[context-system.md](context-system.md) §7 and are enforced in doze-recall's
CI. Writing the stack file on a config load is a few milliseconds.

## Open

- Whether doze should write the stack file only when `recall {}` is present,
  or always (it's cheap, and a person who installs doze-recall later would
  get declarations at once). Leaning: only with the block, so doze writes
  nothing nobody asked for.
- Whether a machine-wide cache under `~/.doze-recall` is acceptable for
  teams with strict data rules, or must everything stay inside the project
  (O5).
