# Phases

Status: plan, 2026-10-20; revised the same day after an independent review,
and again when v1 grew from Go to five languages (R76). Nothing is built.

The build order for recall v1, in units small enough to review one at a time,
in the style of [../next/phases.md](../next/phases.md). Phases are numbered
P0–P11, so they don't collide with the decision ids (R1…) in
[decisions.md](decisions.md).

v1 covers **Go, TypeScript, JavaScript, Python and Rust**. TypeScript and
JavaScript share one language pack. The pilot runs once Go and
TypeScript/JavaScript work. Python and Rust are built while its runs wait on
usage windows, so the evidence arrives about two thirds of the way through,
not at the end.

## Reading the estimates

- **Focused working days for one person.** They are ranges, because they
  come from reading the code and the research, not from having changed
  anything. Each phase starts by re-reading its own files and replacing its
  range with a number.
- **Calendar time is longer.** The pilot's runs, and the agents writing the
  eval stack and its tasks, wait on subscription usage windows.
- **The eval stack and its tasks (T)** are written by agents in sessions
  walled off from recall (V3, [eval.md](eval.md) §3). They cost subscription
  usage and the owner's supervision, not builder days, and they start on
  day one.

| Phase | What ships | Days | Depends on |
|---|---|---|---|
| P0 | The address and sandbox questions tried; a throwaway `impact` spike | 4 to 6 | none |
| P1 | doze's side: the hand-off, `Declared`, the built-in instance, helper services, the change feed | 25 to 38 | P0 |
| P2 | The graph core and Go | 30 to 45 | P1.1, and P1.8 for incremental updates |
| P3 | The language-pack machinery: syntax layer, pack framework, language-server client | 15 to 25 | P2, P1.6–P1.7 |
| P4 | The join: catalogue engine, value resolvers, Go rules | 22 to 32 | P2 |
| P5 | The TypeScript/JavaScript pack | 25 to 35 | P3, P4 |
| P6 | Answers and the retrieval eval (E0) | 18 to 26 | P4, P5; the first 10 eval tasks (T) |
| P7 | The pilot (E1) on a Go + TypeScript stack | 10 to 15 | P6; 20 eval tasks (T) |
| P8 | The Python pack | 20 to 30 | P5 |
| P9 | The Rust pack | 20 to 30 | P5 |
| P10 | The dash and what people add | 25 to 35 | P6 |
| P11 | Generation | 10 to 15 | P10 |
| | **Total** | **224 to 332** | |
| T | `parcel` built by agents (E1 slice, then the Python and Rust apps for E2) and tasks written by agents | agent time: about 4–6 weeks to the E1 slice, 6–11 to E2's stack; owner supervision about 5 to 8 days to E1 (the stack slice and 20 tasks), about 25 to 35 days to E2 (at a day per 20 tasks, plus the stack's audits) | none |

P8, P9 and P10 can go in any order after the pilot starts. The suggested
order is Python, then the dash, then Rust: Python is the more common
language, and the dash makes notes and review usable before the last pack.

## Milestones

| Milestone | Phases | What a user gets |
|---|---|---|
| M1 | P1–P6 | An agent in Claude Code asks `impact`, `brief`, `trace`, `find`, `topology` and `diagnose` about a stack of Go and TypeScript/JavaScript apps, and gets cited answers across apps that say whether they're complete. The dash shows the header strip only |
| M2 | P7 | The pilot's result, and decision V1: how strong a claim v1 ships on |
| M3 | P8–P10 | Python and Rust apps indexed; the recall screen with notes, glossary, decisions and `doze recall check` |
| M4 | P11 | Generate: edge verdicts, glossary rows and note checks through the person's own agent CLI, Claude Code first |

Each milestone leaves `main` releasable. Recall is off unless a `doze.hcl`
has a `recall {}` block, so shipping M1 changes nothing for anyone who
doesn't add one. Until a language's pack ships, its apps appear through
their `doze.hcl` wiring, and answers say `partial: <app> is not indexed`.

## P0: try the risky parts first

| Unit | Work |
|---|---|
| P0.1 | **The address, tried.** fit.md prefers one machine-wide address that routes sessions by stack. But the daemon is per project, and MCP roots arrive only after `initialize`, so routing needs a shared component that answers before it knows the stack. Try it against doze-names' shared `:80` ingress. If it doesn't hold up within the unit, choose a per-stack URL registered per project (`claude mcp add --scope local`). Decide the `domains = false` case. Write the result into [fit.md](fit.md) |
| P0.2 | **Sandboxes, tried** (O9): connect Claude Code inside its sandbox, Codex, and a cloud agent to a `.doze` name and to loopback |
| P0.3 | **The spike:** a hand-written `stack.json` for `examples/shop`, `go/types` over its module, and an `impact` answer for the order event across api and worker, printed to stdout. Thrown away afterwards |
| P0.4 | Settled: V4 is our own stack, `parcel` ([eval.md](eval.md) §2). Create the private `parcel` and `doze-recall-eval` repositories; measure the realism targets on Jitsu and Hatchet clones; write the trap list; fix the task format and the four validity checks as a bun script in `doze-recall-eval`, so T can start before P6. The building agents start T.1 |
| P0.5 | O5: the machine-wide cache under `~/.doze`, accepted or kept in the project |

Exit measures:

- Each question has an answer and a date in [decisions.md](decisions.md).
- The spike's `impact` lists the shop's cross-app consumers of the order
  event.

## P1: doze's side

Contract: [fit.md](fit.md), [graph.md](graph.md) §4.1, §4.8.

| Unit | Work | Files |
|---|---|---|
| P1.1 | The hand-off, built in `internal/config` so the daemon and `doze recall check` both produce it; the daemon writes it on every config load. Contents: each env key's `ref` (captured while `evaluate` walks traversals), `env_files`, `blocks` fingerprints, tasks, a contract version | `internal/config/eval.go`, `fingerprint.go`, new `handoff.go`, `internal/daemon/` |
| P1.2 | The `Declared` capability: read-only, never called by plan or sync, in the SDK, the plugin protocol and the host adapter. `Admin.Resources` exists but needs the instance awake; `Declared` is computed from config | `doze-sdk/engine/driver.go`, `doze-sdk/plugin/proto/engine.proto`, `doze-sdk/plugin/adapter.go`, `doze-sdk/plugin/server.go` |
| P1.3 | `Declared` in doze-aws (resources with URLs and ARNs, plus the operations it implements) and doze-kafka (topics, groups) | `doze-aws/module/`, `doze-kafka/module/` |
| P1.4 | Releases: the SDK tagged, both plugins released against it, doze's pins moved | `doze-sdk`, `doze-aws`, `doze-kafka`, `go.mod` |
| P1.5 | `recall` in `reservedBlocks`; the name and the `recall-` prefix reserved; the `recall {}` block parsed (`include`, `exclude`, `agents`) | `internal/config/load.go`, new `internal/config/recall.go` |
| P1.6 | Hidden instances: the built-in recall instance (proxied, sleeping on idle, excluded from `doze up`), and helper services recall adds through the control socket's `add`. Both skipped by dash rows, `status` and `env` | `internal/config/runnable.go`, `internal/daemon/`, `internal/dash/`, `cmd/doze/` |
| P1.7 | Helper plumbing: `doze __lspbridge` (listens on the service's port, owns the server process, one LSP session per process); downloads from recall's compiled-in `{url, sha256}` table through `__fetch --sha`, never written to `doze.lock`; `DOZE_MIRROR` for those downloads; the gunzip-to-one-file fallback; mirrored archives in doze-binaries; the hand-off carries the path of the `doze` executable so `doze-recall` can run `__fetch` | `cmd/doze/lspbridge.go`, `cmd/doze/fetch.go`, `doze-sdk/binaries/download.go`, `doze-binaries/` |
| P1.8 | The change feed in the daemon: FSEvents through purego on macOS with a persisted event id per root, inotify on Linux, git to reconcile, polling on Windows; a `files` subscription. Runs only while recall is awake | new `internal/feed/`, `internal/control/`, `internal/daemon/subscribe.go` |
| P1.9 | `recall/` module skeleton: `doze-recall` reads `stack.json`, serves MCP at P0.1's address with the `Origin` check, one placeholder tool, a `check` subcommand stub; built and released with doze | new `recall/`, `Makefile`, `.goreleaser.yaml` |
| P1.10 | `status.json` and the dash header strip that reads it without waking recall | `recall/internal/status/`, `internal/dash/` |
| P1.11 | The asleep budget: recall asleep adds nothing to the idle bench | `test/bench/` |

Exit measures:

- An MCP client wakes recall through the proxy and gets the placeholder
  answer. Closing the session lets it sleep.
- A helper service added through `add` runs a pinned binary behind
  `__lspbridge`, sleeps after its connection closes, and leaves `doze.lock`
  untouched.
- `stack.json` for `examples/shop` lists every env key with its `ref` or
  value, the declared queues and topics, and doze-aws's operations.
- Edits made while recall sleeps are all reported on the next wake, on macOS
  and Linux.
- After the first index, with recall asleep, the idle bench is unchanged.

## P2: the graph core and Go

Contract: [graph.md](graph.md) §1–§3, §4.2, §4.4, §5–§9.

| Unit | Work | Files |
|---|---|---|
| P2.1 | Ids: the grammar, quoting, generics to origin, `init` ordinals, build-tag duplicates; the per-language id rules for the packs written down (graph.md §4.6) | `recall/internal/graph/id.go` |
| P2.2 | Node and edge types, label sets, sites, chains, owners; coverage facts | `recall/internal/graph/` |
| P2.3 | Discovery for every language: modules and packages per app folder, `go.work`, `package.json` workspaces, `pyproject.toml`, `Cargo.toml` workspaces; the skip rules (`.gitignore`, nested worktrees such as `.claude/`, `vendor/`, `node_modules/`, `.venv/`, `target/`, `testdata/`, generated and minified files, 256 KB, `exclude`) | `recall/internal/discover/` |
| P2.4 | Go: `go list -e -deps -export` with `GOTOOLCHAIN=local`, `GOPROXY=off`, the cgo retry; the cache key | `recall/internal/golang/list.go` |
| P2.5 | Go: the package loop with up to 4 workers, `nice`, `GOMAXPROCS` capped, a byte-bounded import LRU (its bound measured on Kubernetes) | `recall/internal/golang/check.go`, `lru.go` |
| P2.6 | Go: extraction (definitions, references, calls, `implements`, `reads_env`, routes, logging and error literals, undefined names); build-constrained files; hazards | `recall/internal/golang/` |
| P2.7 | The stack: reading `stack.json`, secrets dropped from `env_files`, app ownership by entry-point reachability, run facts and test commands | `recall/internal/stack/` |
| P2.8 | Dependencies, Go first: `go list -m -json`, module cache paths, the machine-wide cache; the interface the packs fill for lockfiles | `recall/internal/deps/` |
| P2.9 | Storage: the snapshot, `graph.db` with FTS5 (split identifiers, `porter` for prose), existing prose indexed, the per-module cache; the SQLite driver chosen | `recall/internal/graph/snapshot.go`, `recall/internal/store/`, `recall/internal/prose/` |
| P2.10 | The writer queue, passes, publish, generations, refreshes; incremental updates (surface hash without positions, importers outward, added names, config-file owners, deletions); the wake catch-up | `recall/internal/index/`, `recall/internal/feed/` |
| P2.11 | Tests and budgets: the Go oracle (`gopls references` on a sample, a type check per GOOS), id stability, incremental equals full, determinism; bench entries for serving RSS and indexing peak | `recall/internal/*/…_test.go`, `test/bench/` |

Exit measures:

- The Go oracle bar met on the net/http set and doze-aws.
- Incremental equals full after the scripted edits.
- Measured on doze-aws with tests:
  - indexing peak ≤ 300 MB;
  - full index ≤ 5 s with a warm build cache;
  - save to a fresh generation ≤ 200 ms p95;
  - wake with nothing changed ≤ 50 ms;
  - serving RSS ≤ 80 MB.
- Byte-identical snapshots across two builds.
- The `.claude/` worktree copies are skipped.

## P3: the language-pack machinery

Contract: [graph.md](graph.md) §4.6–§4.7; the rules in
[context-system.md](context-system.md) §4.3 and §6.

| Unit | Work | Files |
|---|---|---|
| P3.1 | The syntax layer: gotreesitter pinned, a grammar subset (TS, TSX, JS, Python, Rust), parse → run queries → release in one goroutine, 2–4 workers, the size and minified caps, a per-file time budget, our own parity corpus against C tree-sitter in CI | `recall/internal/syntax/` |
| P3.2 | The pack framework: what a pack declares (globs, root markers, queries, resolver, id rules, catalogue rows, server), and how the index runs a pack's three steps | `recall/internal/lang/pack.go` |
| P3.3 | The language-server client: initialize, answering server requests, per-server readiness rules, the canary, pipelined `definition` requests for doubtful sites, a site cache keyed by (file hash, site, server version), the memory ceiling and one restart | `recall/internal/lsp/` |
| P3.4 | Server lifecycle through P1.6–P1.7: added per app and language, one at a time per stack, held while doubtful sites remain plus 2 minutes, then released to sleep | `recall/internal/lsp/lifecycle.go` |
| P3.5 | The binary-size check: recall with SQLite, x/tools, go-pgquery and the five grammars (TS, TSX, JS, Python, Rust), measured and budgeted | `Makefile`, `test/bench/` |

Exit measures:

- A fixture pack with one grammar indexes a small corpus.
- A fake server that never reports readiness, one that answers early, and one
  that returns empty references each end in the right state: ready, waited
  for, or degraded.
- The parity corpus passes against C tree-sitter.

## P4: the join

Contract: [graph.md](graph.md) §4.3.

| Unit | Work | Files |
|---|---|---|
| P4.1 | The catalogue engine, language-neutral: row format, the five shapes (argument, literal, option, output, identity), matched over each language's facts | `recall/internal/join/catalogue.go` |
| P4.2 | The value resolver: one interface, a `go/types` implementation (constants, env reads, concatenation, `Same`, `OneOf`, config struct summaries, envconfig and caarlos0/env), and a syntax-level implementation the packs use (literals, module constants, env reads, same-file concatenation) | `recall/internal/join/resolve.go`, `resolve_go.go`, `resolve_syntax.go` |
| P4.3 | Client-to-module binding; the join with the hand-off; `undeclared` resources; `doubtful` candidates; unresolved-site facts | `recall/internal/join/` |
| P4.4 | The AWS generator: Smithy models and the Service Authorization Reference pinned by commit, one resource-argument table emitted in every SDK's call shape (Go v2, JS v3, boto3, Rust), about 10 overrides; rows committed | `recall/tools/catalogue-aws/`, `recall/internal/*/catalogue/aws.tsv` |
| P4.5 | Go rows: franz-go, Sarama, segmentio, confluent-kafka-go (cgo, so `go list -export` needs a C toolchain, and without one its users fall back to syntax); `database/sql`, pgx, sqlx, sqlc; ServeMux, chi, gin, echo; `net/http` clients; logging | `recall/internal/golang/catalogue/` |
| P4.6 | SQL through `wasilibs/go-pgquery`; schema from migrations and sqlc files. HTTP: route tables, client path templates, segment matching | `recall/internal/join/sql.go`, `http.go` |
| P4.7 | Tests: catalogue conformance against doze-aws; the shop join | `recall/internal/join/*_test.go` |

Exit measures:

- On `examples/shop`, every edge is found with the expected label and
  chain, with no unresolved sites:
  - `api —sends→ topic.orders` (literal shape);
  - `worker —receives→ topic.orders` (option shape);
  - `worker —receives→ queue.orders` (output shape and the name match).
- The AWS rows agree with doze-aws.
- P2's budgets still hold.

## P5: the TypeScript/JavaScript pack

Contract: [graph.md](graph.md) §4.6.

| Unit | Work | Files |
|---|---|---|
| P5.1 | Queries for TS, TSX, JS, JSX: definitions, references, imports and exports, literals, `process.env` reads, call sites | `recall/internal/lang/typescript/queries/` |
| P5.2 | Resolution: ES and CommonJS imports, `tsconfig` `paths` and `baseUrl`, `package.json` `exports` and workspaces, re-exports; `scoped` or `doubtful` | `recall/internal/lang/typescript/resolve.go` |
| P5.3 | Ids: `ts:` + package name + path + export chain; default exports, declaration merging, overloads | `recall/internal/lang/typescript/id.go` |
| P5.4 | The server: TypeScript 7 `tsc --lsp --stdio` pinned (9 MB), never a project-local `tsserver`; its readiness rule (requests block until the project loads), root from the nearest `tsconfig.json` | `recall/internal/lang/typescript/server.go` |
| P5.5 | Hazards: dynamic property access, `eval`, `require` of computed paths, callbacks stored and called later | `recall/internal/lang/typescript/hazards.go` |
| P5.6 | Catalogue rows: AWS SDK v3 (generated), KafkaJS and `@confluentinc/kafka-javascript`, `pg` and Prisma, `fetch`, axios, Express, Fastify, Hono, and Next.js file-based routes (the app and pages directories); lockfile versions for dependencies | `recall/internal/lang/typescript/catalogue/`, `recall/internal/deps/` |
| P5.7 | The oracle corpus: precision ≥ 0.98 for `exact` and `scoped` edges against the server's own answers on a sample; incremental equals full | `recall/internal/lang/typescript/testdata/` |

Exit measures:

- The oracle bar met on hono and on the eval stack's TS services.
- A Go → topic → TypeScript consumer edge on the eval stack is found with its
  chain.
- With the server unavailable (fake failure), the TS apps index at
  `scoped`/`doubtful`, and answers say partial with the reason.
- Budgets: the tree-sitter pass ≤ 300 MB at 2 workers; the server's peak
  recorded.

## P6: answers and the retrieval eval

Contract: [answers.md](answers.md); [eval.md](eval.md) §3, §6 (E0).

| Unit | Work | Files |
|---|---|---|
| P6.1 | Sessions: which stack (per P0.1), generic instructions, the tool registry with annotations and `alwaysLoad`; dropping the in-memory graph after a quiet hour | `recall/internal/serve/mcp.go`, `session.go`, `instructions.go` |
| P6.2 | Parse, seed, resolve; freshen | `anchor.go`, `freshen.go` |
| P6.3 | Expand per tool, rank, pack (levels, the estimator, caps, cursors with generations) | `expand.go`, `rank.go`, `pack.go` |
| P6.4 | Render: text and shared `structuredContent`, quoting, redaction, paths, completeness lines | `render.go`, `complete.go` |
| P6.5 | The six tools; `impact` on a diff; `diagnose` from the journal and log store | `recall/internal/serve/tools/`, `diff.go` |
| P6.6 | The Queries log | `querylog.go` |
| P6.7 | E0: the retrieval eval on `parcel`'s first 10 tasks (format and checks from P0.4), the budget sweep, `doze-recall-eval`'s CI in report-only mode | `recall/eval/retrieval/`; tasks in `doze-recall-eval` |

Exit measures:

- Each tool answers in Claude Code on the eval stack's first slice.
- `impact` on a Go function whose consumer is a TypeScript app says
  complete, and says partial with the reason when a build-constrained caller
  is added.
- On the 10 E0 tasks, no "complete" answer misses a required site.
- Identical bytes for every tool except `diagnose`.
- The retrieval eval reports in CI.

## P7: the pilot (E1)

Contract: [eval.md](eval.md).

| Unit | Work | Who |
|---|---|---|
| T.1 | `parcel`'s E1 slice: v0 of the six Go and TypeScript apps and their `doze.hcl` (doze-aws, Kafka, Postgres), then 80–120 feature commits that bring in the trap list; unit tests run with no services; 40–50k hand-written lines and 25+ cross-app links (36+ once the E2 apps join), each listed in the plan before building; an independent audit against the targets | building agents (Opus; Codex for two apps when available), from P0.4 |
| T.2 | 20 cross-app tasks, at least half crossing Go ↔ TypeScript, with required sites, hidden tests and the four validity checks, each reviewed by a second agent, 5 read by the owner; the first 10 feed E0 | authoring and reviewing agents, from T.1 (tasks need `parcel` commits as their base) and P0.4 |
| P7.1 | The harness: worktrees, an idle daemon with a fresh `DOZE_HOME` and nothing started, fresh configuration homes, A0's removals and leak scan, waiting on usage windows, the results layout; the analysis script written before the runs | recall's builder |
| P7.2 | 80 runs: Claude Code on `claude-sonnet-5-5`, A0 and A2, 2 runs each | the subscription |
| P7.3 | The report: the primary measure with its interval, the secondaries, the measured variance, runs per usage window, transcripts read | recall's builder |

Exit measures: the report is written, and V1 is decided from it.

## P8: the Python pack

Contract: [graph.md](graph.md) §4.6.

| Unit | Work | Files |
|---|---|---|
| P8.1 | Queries; resolution of absolute and relative imports, packages, `__init__` re-exports, `self.` and class scope | `recall/internal/lang/python/` |
| P8.2 | Ids: `py:` + distribution or package name + module path + qualified name; `@overload`, properties | `id.go` |
| P8.3 | The server: ty pinned (12 MB), pyrefly as the alternate; the root at the `pyproject.toml`; the venv when one exists; its readiness rule | `server.go` |
| P8.4 | Hazards: `getattr`/`setattr` with computed names, `__getattr__`, `importlib`, monkeypatching | `hazards.go` |
| P8.5 | Catalogue rows: boto3 client and resource calls (generated), kafka-python and confluent-kafka, psycopg, SQLAlchemy tables, requests and httpx, FastAPI, Flask and Django routes; lockfile versions | `catalogue/`, `recall/internal/deps/` |
| P8.6 | The oracle corpus and incremental equals full | `testdata/` |

Exit measures:

- The oracle bar met on pydantic and on a small fixture stack with a Python consumer (E2's corpus is chosen after V1).
- A cross-app edge through a queue into a Python consumer is found.

## P9: the Rust pack

Contract: [graph.md](graph.md) §4.6.

| Unit | Work | Files |
|---|---|---|
| P9.1 | Queries; `use` paths, modules, crates in a workspace | `recall/internal/lang/rust/` |
| P9.2 | Ids: `rs:` + crate + module path + item; trait impls | `id.go` |
| P9.3 | The server: rust-analyzer pinned (14 MB), build scripts and proc macros off, `rust-project.json` written by recall when cargo is absent, readiness by its progress and quiescence | `server.go`, `rustproject.go` |
| P9.4 | Hazards: trait objects across apps, macros the server can't expand | `hazards.go` |
| P9.5 | Catalogue rows: aws-sdk-rust (generated), rdkafka, sqlx and tokio-postgres, reqwest, Axum and Actix routes; `Cargo.lock` versions | `catalogue/`, `recall/internal/deps/` |
| P9.6 | The oracle corpus and incremental equals full | `testdata/` |

Exit measures:

- The oracle bar met on ripgrep and on a small fixture stack with a Rust service.
- With no cargo on the machine, the generated project file still gives
  references.

## P10: the dash and what people add

Contract: [context.md](context.md); the screens as tabs in recall's screen
([fit.md](fit.md) §5).

| Unit | Work | Files |
|---|---|---|
| P10.1 | A tab component for the dash (there is none; `internal/dash` is about 11.6k lines); recall's screen: Overview (levels per app and language, skipped and degraded reasons), Queries, Connect | `internal/dash/` |
| P10.2 | Notes: parsing, the full-id rule, staleness against the `checked` commit from git, orphans and re-anchor candidates | `recall/internal/notes/` |
| P10.3 | The glossary and `topology.yaml` decisions: parsing, per-entry validation, drift; `proposals.json` | `recall/internal/notes/` |
| P10.4 | Writes on keypress only, atomic and mtime-checked; the Notes, Review and Topology tabs | `recall/internal/notes/write.go`, `internal/dash/` |
| P10.5 | `doze recall check`: `cmd/doze` runs `doze-recall check`; the hand-off built in-process; the checks and exit codes | `cmd/doze/recall.go`, `recall/cmd/doze-recall/` |
| P10.6 | The parity exemption for all of recall's dash actions (R9) | `cmd/doze/parity_test.go` |
| P10.7 | Notes in answers: the Notes section, quoting, stale notes to Unconfirmed | `recall/internal/serve/` |

Exit measures:

- A note on a queue appears in `brief` and `impact` answers that touch it.
  It goes to Unconfirmed when the queue's declaration changes, and stays put
  for unrelated edits.
- A fresh clone sees the same staleness.
- `doze recall check` fails on a bad id and on drift, and passes with a
  partial build listed. The doze binary's dependencies are unchanged.
- Screenshots of every tab compared with the design boards before the phase
  is called done.

## P11: generation

Contract: [generation.md](generation.md).

| Unit | Work | Files |
|---|---|---|
| P11.1 | The job format, keys and cache; scope preview; stop; per-stack consent | `recall/internal/generate/job.go` |
| P11.2 | The Claude Code runner: headless, `--tools ""`, recall's MCP only, `--json-schema`, `--model` pinned | `runner_claude.go` |
| P11.6 | Codex and Cursor runners, each added once the owner has the plan and it passes the contract test (R95) | `runner_codex.go`, `runner_cursor.go` |
| P11.3 | The contract test, and a hostile fixture in `testdata/hostile/` whose comment tells the agent to write a file and run a command | `contract_test.go` |
| P11.4 | Jobs: edge verdicts (most useful on the Python and JavaScript doubtful sites), glossary rows, note checks | `recall/internal/generate/jobs/` |
| P11.5 | The Generate tab; the agent picker in Connect honouring `agents`; proposals into Review | `internal/dash/` |

Exit measures:

- A verdict on a fixture's unresolved site passes its checks and waits in
  Review.
- The contract test passes for Claude Code.
- The hostile fixture changes nothing the runner can do.

## After v1

Not planned in units yet:

- E2, if V1 calls for it, with Python and Rust services in the eval stack;
- `watch` on the change feed;
- observed calls;
- Java, C#, PHP, Ruby and Kotlin at syntax level (a grammar and queries
  each, a few days per language), and at full precision only where a server
  needs no JVM or .NET install;
- per-resource attributes on the aws and kafka plugins (O11);
- the Go rows cut from P4 (confluent-kafka-go, viper, koanf, resty, SaaS
  SDKs);
- bulk re-anchor after a module rename;
- Windows verified, once doze has Windows CI.
