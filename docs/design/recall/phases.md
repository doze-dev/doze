# Phases

Status: plan, 2026-10-20; revised the same day after an independent review,
and again when v1 grew from Go to five languages (R76). Restructured
2026-10-21 when recall became independent of doze (R103): recall's own base
comes first, and doze's integration is the last phase. Nothing is built.

The build order for recall v1, in units small enough to review one at a time,
in the style of [../next/phases.md](../next/phases.md). Phases are numbered
P0–P12, so they don't collide with the decision ids (R1…) in
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
| P0 | Stdio inside agents' sandboxes tried; a throwaway `impact` spike; the eval's groundwork | 3 to 5 | none |
| P1 | Recall's base: the repository, the shim and background process, the watcher, finding the root and apps, `status`, a release | 22 to 32 | P0 |
| P2 | The graph core and Go | 30 to 45 | P1 |
| P3 | The language-pack machinery: syntax layer, pack framework, language-server client and supervision | 18 to 29 | P2 |
| P4 | The join: catalogue engine, value resolvers, Go rules, inferred resources, the declared-stack file | 25 to 36 | P2 |
| P5 | The TypeScript/JavaScript pack | 25 to 35 | P3, P4 |
| P6 | Answers and the retrieval eval (E0) | 18 to 26 | P4, P5; the first 10 eval tasks (T) |
| P7 | The pilot (E1), on code alone | 10 to 15 | P6; 20 eval tasks (T) |
| P8 | The Python pack | 20 to 30 | P5 |
| P9 | The Rust pack | 20 to 30 | P5 |
| P10 | What people add: notes, glossary, decisions, `doze-recall review` and `check` | 20 to 28 | P6 |
| P11 | Generation | 10 to 15 | P10 |
| P12 | doze as an input: the stack file from `doze.hcl`, `Declared`, the pinned copy and `doze recall`, the dash screen | 30 to 44 | P10, P11 (the Generate tab); the stack-file format from P4 |
| | **Total** | **251 to 370** | |
| T | `parcel` built by agents (E1 slice, then the Python and Rust apps for E2) and tasks written by agents | agent time: about 4–6 weeks to the E1 slice, 6–11 to E2's stack; owner supervision about 5 to 8 days to E1 (the stack slice and 20 tasks), about 25 to 35 days to E2 (at a day per 20 tasks, plus the stack's audits) | none |

The total grew by about 12 days over the plan where doze hosted recall: recall needs its
own review command and local API, and doze's dash becomes a client of it
rather than sharing code.

P8, P9 and P10 can go in any order after the pilot starts, and P12 follows
P10. If E2 runs, its doze arms need P12. The suggested order is Python, then
P10, then Rust, then P12: Python is the more common language, and review
makes notes usable before the last pack.

## Milestones

| Milestone | Phases | What a user gets |
|---|---|---|
| M1 | P1–P6 | Anyone installs `doze-recall`, adds it to Claude Code, and asks `impact`, `brief`, `trace`, `find`, `topology` and `diagnose` about a repo of Go and TypeScript/JavaScript apps, with cited answers across apps that say whether they're complete. No doze needed |
| M2 | P7 | The pilot's result, and decision V1: how strong a claim v1 ships on |
| M3 | P8–P10 | Python and Rust apps indexed; notes, glossary and decisions; `doze-recall review` and `doze-recall check` |
| M4 | P11 | Generate: edge verdicts, glossary rows and note checks through the person's own agent CLI, Claude Code first |
| M5 | P12 | doze users get declared links, the dash's recall screen, and `doze recall` with a pinned copy |

doze-recall releases from M1. doze itself is untouched until P12, so
nothing about recall can break a doze release before then. Until a
language's pack ships, its apps are still found by discovery, and answers
say `partial: <app> is not indexed`.

## P0: try the risky parts first

| Unit | Work |
|---|---|
| P0.1 | **Stdio in sandboxes, tried** (O9): `doze-recall serve` started by Claude Code inside its sandbox, by Codex, and by a cloud agent. Can the shim start a background process, and reach its socket in `~/.doze-recall/run/`? If a sandbox forbids it, the shim serves in-process for that session instead (one writer still holds through the lock) |
| P0.2 | **The spike:** `go/types` over `examples/shop` with no stack file, inferred resources by hand, and an `impact` answer for the order event across api and worker, printed to stdout. Thrown away afterwards |
| P0.3 | Settled: V4 is our own stack, `parcel` ([eval.md](eval.md) §2). Create the private `parcel` and `doze-recall-eval` repositories; measure the realism targets on Jitsu and Hatchet clones; write the trap list; fix the task format and the four validity checks as a bun script in `doze-recall-eval`, so T can start before P6. The building agents start T.1 |
| P0.4 | O5: the machine-wide cache under `~/.doze-recall`, accepted or kept in the project |

Exit measures:

- Each question has an answer and a date in [decisions.md](decisions.md).
- The spike's `impact` lists the shop's cross-app consumers of the order
  event.

## P1: recall's base

Contract: [running.md](running.md).

| Unit | Work | Files |
|---|---|---|
| P1.1 | The `doze-recall` repository: module skeleton, CI on macOS, Linux and Windows, goreleaser, `install.sh` checked on macOS and Linux | `doze-recall/` |
| P1.2 | The stdio shim and the background process: one per root, Unix socket (0700, peer uid checked), `flock` and stale-socket recovery, the version handshake, idle exit after 5 minutes; a named pipe on Windows | `doze-recall/internal/shim/`, `doze-recall/internal/bgproc/` |
| P1.3 | MCP over stdio with one placeholder tool; the local API's skeleton (status, versioned) | `doze-recall/internal/serve/`, `doze-recall/internal/bgproc/api.go` |
| P1.4 | The watcher: FSEvents through purego on macOS with a saved event id per root, inotify on Linux, git to reconcile, polling on Windows; the wake catch-up's input | `doze-recall/internal/feed/` |
| P1.5 | The root and the apps: root rules and refusals, discovery from manifests in all five languages (as text, no scripts), app names, `.env` files with the secret rules, `.recall/recall.toml` and accepting `roots` | `doze-recall/internal/inputs/` |
| P1.6 | `status.json`; `doze-recall status` (reads it without starting anything) and `connect` | `doze-recall/internal/status/`, `doze-recall/internal/cli/` |
| P1.7 | The first release | `doze-recall/` |

Exit measures:

- Claude Code starts `doze-recall serve` and gets the placeholder answer.
  Two sessions on one root share one background process; nothing runs 5
  minutes after the last closes.
- On `testdata/shop/` (copied from doze's `examples/shop`) and on `parcel` in the plain-repo setting, discovery
  finds every app, and refuses a home folder.
- Edits made while recall wasn't running are all reported when it next
  starts, on macOS and Linux.

## P2: the graph core and Go

Contract: [graph.md](graph.md) §1–§3, §4.2, §4.4, §5–§9.

| Unit | Work | Files |
|---|---|---|
| P2.1 | Ids: the grammar, quoting, generics to origin, `init` ordinals, build-tag duplicates; the per-language id rules for the packs written down (graph.md §4.6) | `doze-recall/internal/graph/id.go` |
| P2.2 | Node and edge types, label sets, sites, chains, owners; coverage facts | `doze-recall/internal/graph/` |
| P2.3 | Discovery for every language: modules and packages per app folder, `go.work`, `package.json` workspaces, `pyproject.toml`, `Cargo.toml` workspaces; the skip rules (`.gitignore`, nested worktrees such as `.claude/`, `vendor/`, `node_modules/`, `.venv/`, `target/`, `testdata/`, generated and minified files, 256 KB, `exclude`) | `doze-recall/internal/discover/` |
| P2.4 | Go: `go list -e -deps -export` with `GOTOOLCHAIN=local`, `GOPROXY=off`, the cgo retry; the cache key | `doze-recall/internal/golang/list.go` |
| P2.5 | Go: the package loop with up to 4 workers, `nice`, `GOMAXPROCS` capped, a byte-bounded import LRU (its bound measured on Kubernetes) | `doze-recall/internal/golang/check.go`, `lru.go` |
| P2.6 | Go: extraction (definitions, references, calls, `implements`, `reads_env`, routes, logging and error literals, undefined names); build-constrained files; hazards | `doze-recall/internal/golang/` |
| P2.7 | Apps from P1.5: app ownership by entry-point reachability, run facts and test commands from manifests | `doze-recall/internal/inputs/` |
| P2.8 | Dependencies, Go first: `go list -m -json`, module cache paths, the machine-wide cache; the interface the packs fill for lockfiles | `doze-recall/internal/deps/` |
| P2.9 | Storage: the snapshot, `graph.db` with FTS5 (split identifiers, `porter` for prose), existing prose indexed, the per-module cache; the SQLite driver chosen | `doze-recall/internal/graph/snapshot.go`, `doze-recall/internal/store/`, `doze-recall/internal/prose/` |
| P2.10 | The writer queue, passes, publish, generations, refreshes; incremental updates (surface hash without positions, importers outward, added names, config-file owners, deletions); the wake catch-up from P1.4's watcher | `doze-recall/internal/index/` |
| P2.11 | Tests and budgets: the Go oracle (`gopls references` on a sample, a type check per GOOS), id stability, incremental equals full, determinism; bench entries for serving RSS and indexing peak | `doze-recall/internal/*/…_test.go`, `test/bench/` |

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
| P3.1 | The syntax layer: gotreesitter pinned, a grammar subset (TS, TSX, JS, Python, Rust), parse → run queries → release in one goroutine, 2–4 workers, the size and minified caps, a per-file time budget, our own parity corpus against C tree-sitter in CI | `doze-recall/internal/syntax/` |
| P3.2 | The pack framework: what a pack declares (globs, root markers, queries, resolver, id rules, catalogue rows, server), and how the index runs a pack's three steps | `doze-recall/internal/lang/pack.go` |
| P3.3 | The language-server client: initialize, answering server requests, per-server readiness rules, the canary, pipelined `definition` requests for doubtful sites, a site cache keyed by (file hash, site, server version), the memory ceiling and one restart | `doze-recall/internal/lsp/` |
| P3.4 | Server supervision as recall's child processes: the compiled-in `{url, sha256}` table, `DOZE_RECALL_MIRROR` (and `DOZE_MIRROR`), the archives mirrored in doze-binaries releases, the gunzip-to-one-file fallback, the cache in recall's home; started per app and language, one at a time per machine, held while doubtful sites remain plus 2 minutes, then stopped; logs and `status.json` entries | `doze-recall/internal/lsp/lifecycle.go`, `doze-recall/internal/lsp/fetch.go` |
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
| P4.1 | The catalogue engine, language-neutral: row format, the five shapes (argument, literal, option, output, identity), matched over each language's facts | `doze-recall/internal/join/catalogue.go` |
| P4.2 | The value resolver: one interface, a `go/types` implementation (constants, env reads, concatenation, `Same`, `OneOf`, config struct summaries, envconfig and caarlos0/env), and a syntax-level implementation the packs use (literals, module constants, env reads, same-file concatenation) | `doze-recall/internal/join/resolve.go`, `resolve_go.go`, `resolve_syntax.go` |
| P4.3 | Client-to-module binding; the join with declared resources; `undeclared` resources; `doubtful` candidates; unresolved-site facts | `doze-recall/internal/join/` |
| P4.4 | Inferred resources when there is no stack file: ids, `scoped` at best, the coverage line, `doubtful` links through a shared env key name, mapping to declared resources when a file appears | `doze-recall/internal/join/inferred.go` |
| P4.5 | The declared-stack file: its JSON Schema and version, the reader, `sources` hashes and staleness, `logs` for `diagnose`; tested with hand-written files until P12 writes real ones | `doze-recall/internal/inputs/stackfile.go`, `doze-recall/schema/stack.schema.json` |
| P4.6 | The AWS generator: Smithy models and the Service Authorization Reference pinned by commit, one resource-argument table emitted in every SDK's call shape (Go v2, JS v3, boto3, Rust), about 10 overrides; rows committed | `doze-recall/tools/catalogue-aws/`, `doze-recall/internal/*/catalogue/aws.tsv` |
| P4.7 | Go rows: franz-go, Sarama, segmentio, confluent-kafka-go (cgo, so `go list -export` needs a C toolchain, and without one its users fall back to syntax); `database/sql`, pgx, sqlx, sqlc; ServeMux, chi, gin, echo; `net/http` clients; logging | `doze-recall/internal/golang/catalogue/` |
| P4.8 | SQL through `wasilibs/go-pgquery`; schema from migrations and sqlc files. HTTP: route tables, client path templates, segment matching | `doze-recall/internal/join/sql.go`, `http.go` |
| P4.9 | Tests: catalogue conformance against a recorded doze-aws fixture; the shop join on `testdata/shop/` (copied from doze's `examples/shop`, with a hand-written stack file) | `doze-recall/internal/join/*_test.go` |

Exit measures:

- On `examples/shop` with a hand-written stack file, every edge is found
  with the expected label and chain, with no unresolved sites:
  - `api —sends→ topic.orders` (literal shape);
  - `worker —receives→ topic.orders` (option shape);
  - `worker —receives→ queue.orders` (output shape and the name match).
- Without the stack file, the same three links appear through inferred
  resources, labelled `scoped`.
- The AWS rows agree with doze-aws.
- P2's budgets still hold.

## P5: the TypeScript/JavaScript pack

Contract: [graph.md](graph.md) §4.6.

| Unit | Work | Files |
|---|---|---|
| P5.1 | Queries for TS, TSX, JS, JSX: definitions, references, imports and exports, literals, `process.env` reads, call sites | `doze-recall/internal/lang/typescript/queries/` |
| P5.2 | Resolution: ES and CommonJS imports, `tsconfig` `paths` and `baseUrl`, `package.json` `exports` and workspaces, re-exports; `scoped` or `doubtful` | `doze-recall/internal/lang/typescript/resolve.go` |
| P5.3 | Ids: `ts:` + package name + path + export chain; default exports, declaration merging, overloads | `doze-recall/internal/lang/typescript/id.go` |
| P5.4 | The server: TypeScript 7 `tsc --lsp --stdio` pinned (9 MB), never a project-local `tsserver`; its readiness rule (requests block until the project loads), root from the nearest `tsconfig.json` | `doze-recall/internal/lang/typescript/server.go` |
| P5.5 | Hazards: dynamic property access, `eval`, `require` of computed paths, callbacks stored and called later | `doze-recall/internal/lang/typescript/hazards.go` |
| P5.6 | Catalogue rows: AWS SDK v3 (generated), KafkaJS and `@confluentinc/kafka-javascript`, `pg` and Prisma, `fetch`, axios, Express, Fastify, Hono, and Next.js file-based routes (the app and pages directories); lockfile versions for dependencies | `doze-recall/internal/lang/typescript/catalogue/`, `doze-recall/internal/deps/` |
| P5.7 | The oracle corpus: precision ≥ 0.98 for `exact` and `scoped` edges against the server's own answers on a sample; incremental equals full | `doze-recall/internal/lang/typescript/testdata/` |

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
| P6.1 | Sessions on the shim's root, generic instructions, the tool registry with annotations and `alwaysLoad`; dropping the in-memory graph after a quiet hour | `doze-recall/internal/serve/mcp.go`, `session.go`, `instructions.go` |
| P6.2 | Parse, seed, resolve; freshen | `anchor.go`, `freshen.go` |
| P6.3 | Expand per tool, rank, pack (levels, the estimator, caps, cursors with generations) | `expand.go`, `rank.go`, `pack.go` |
| P6.4 | Render: text and shared `structuredContent`, quoting, redaction, paths, completeness lines | `render.go`, `complete.go` |
| P6.5 | The six tools; `impact` on a diff; `diagnose` from the error text, and from the logs a stack file points at | `doze-recall/internal/serve/tools/`, `diff.go` |
| P6.6 | The Queries log | `querylog.go` |
| P6.7 | E0: the retrieval eval on `parcel`'s first 10 tasks (format and checks from P0.3), the budget sweep, `doze-recall-eval`'s CI in report-only mode | `doze-recall/eval/retrieval/`; tasks in `doze-recall-eval` |

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
| T.1 | `parcel`'s E1 slice: v0 of the six Go and TypeScript apps and their `doze.hcl` (doze-aws, Kafka, Postgres), then 80–120 feature commits that bring in the trap list; unit tests run with no services; 40–50k hand-written lines and 25+ cross-app links (36+ once the E2 apps join), each listed in the plan before building; an independent audit against the targets | building agents (Opus; Codex for two apps when available), from P0.3 |
| T.2 | 20 cross-app tasks, at least half crossing Go ↔ TypeScript, with required sites, hidden tests and the four validity checks, each reviewed by a second agent, 5 read by the owner; the first 10 feed E0 | authoring and reviewing agents, from T.1 (tasks need `parcel` commits as their base) and P0.3 |
| P7.1 | The harness: worktrees in the plain-repo setting (`doze.hcl` and `.doze/` removed, a `.env` per app generated from the resolved config), a fresh `~/.doze-recall` per run, fresh configuration homes, A0's removals and leak scan, waiting on usage windows, the results layout; the analysis script written before the runs | recall's builder |
| P7.2 | 80 runs: Claude Code on `claude-sonnet-5-5`, A0 and A2, 2 runs each | the subscription |
| P7.3 | The report: the primary measure with its interval, the secondaries, the measured variance, runs per usage window, transcripts read | recall's builder |

Exit measures: the report is written, and V1 is decided from it.

## P8: the Python pack

Contract: [graph.md](graph.md) §4.6.

| Unit | Work | Files |
|---|---|---|
| P8.1 | Queries; resolution of absolute and relative imports, packages, `__init__` re-exports, `self.` and class scope | `doze-recall/internal/lang/python/` |
| P8.2 | Ids: `py:` + distribution or package name + module path + qualified name; `@overload`, properties | `id.go` |
| P8.3 | The server: ty pinned (12 MB), pyrefly as the alternate; the root at the `pyproject.toml`; the venv when one exists; its readiness rule | `server.go` |
| P8.4 | Hazards: `getattr`/`setattr` with computed names, `__getattr__`, `importlib`, monkeypatching | `hazards.go` |
| P8.5 | Catalogue rows: boto3 client and resource calls (generated), kafka-python and confluent-kafka, psycopg, SQLAlchemy tables, requests and httpx, FastAPI, Flask and Django routes; lockfile versions | `catalogue/`, `doze-recall/internal/deps/` |
| P8.6 | The oracle corpus and incremental equals full | `testdata/` |

Exit measures:

- The oracle bar met on pydantic and on a small fixture stack with a Python consumer (E2's corpus is chosen after V1).
- A cross-app edge through a queue into a Python consumer is found.

## P9: the Rust pack

Contract: [graph.md](graph.md) §4.6.

| Unit | Work | Files |
|---|---|---|
| P9.1 | Queries; `use` paths, modules, crates in a workspace | `doze-recall/internal/lang/rust/` |
| P9.2 | Ids: `rs:` + crate + module path + item; trait impls | `id.go` |
| P9.3 | The server: rust-analyzer pinned (14 MB), build scripts and proc macros off, `rust-project.json` written by recall when cargo is absent, readiness by its progress and quiescence | `server.go`, `rustproject.go` |
| P9.4 | Hazards: trait objects across apps, macros the server can't expand | `hazards.go` |
| P9.5 | Catalogue rows: aws-sdk-rust (generated), rdkafka, sqlx and tokio-postgres, reqwest, Axum and Actix routes; `Cargo.lock` versions | `catalogue/`, `doze-recall/internal/deps/` |
| P9.6 | The oracle corpus and incremental equals full | `testdata/` |

Exit measures:

- The oracle bar met on ripgrep and on a small fixture stack with a Rust service.
- With no cargo on the machine, the generated project file still gives
  references.

## P10: what people add

Contract: [context.md](context.md); [running.md](running.md) §7.

| Unit | Work | Files |
|---|---|---|
| P10.1 | Notes: parsing, the full-id rule, staleness against the `checked` commit from git, orphans and re-anchor candidates | `doze-recall/internal/notes/` |
| P10.2 | The glossary and `topology.yaml` decisions: parsing, per-entry validation, drift; `proposals.json` | `doze-recall/internal/notes/` |
| P10.3 | Writes on keypress only, atomic and mtime-checked, behind the local API so the dash can use them later | `doze-recall/internal/notes/write.go`, `doze-recall/internal/bgproc/api.go` |
| P10.4 | `doze-recall review`: one list and prompt for everything waiting, with the dash's keys | `doze-recall/internal/cli/review.go` |
| P10.5 | `doze-recall check`: the checks and exit codes, on code alone or with a stack file | `doze-recall/cmd/doze-recall/`, `doze-recall/internal/check/` |
| P10.6 | Notes in answers: the Notes section, quoting, stale notes to Unconfirmed | `doze-recall/internal/serve/` |

Exit measures:

- A note on a queue appears in `brief` and `impact` answers that touch it.
  It goes to Unconfirmed when the queue's code changes, and stays put for
  unrelated edits.
- A fresh clone sees the same staleness.
- `doze-recall check` fails on a bad id and on drift, and passes with a
  partial build listed.

## P11: generation

Contract: [generation.md](generation.md).

| Unit | Work | Files |
|---|---|---|
| P11.1 | The job format, keys and cache; scope preview; stop; per-stack consent | `doze-recall/internal/generate/job.go` |
| P11.2 | The Claude Code runner: headless, `--tools ""`, recall's MCP only, `--json-schema`, `--model` pinned | `runner_claude.go` |
| P11.6 | Codex and Cursor runners, each added once the owner has the plan and it passes the contract test (R95) | `runner_codex.go`, `runner_cursor.go` |
| P11.3 | The contract test, and a hostile fixture in `testdata/hostile/` whose comment tells the agent to write a file and run a command | `contract_test.go` |
| P11.4 | Jobs: edge verdicts (most useful on the Python and JavaScript doubtful sites), glossary rows, note checks | `doze-recall/internal/generate/jobs/` |
| P11.5 | `doze-recall generate`: `--agent`, remembered in `~/.doze-recall/config.toml`, honouring `agents`; proposals into `review` | `doze-recall/internal/cli/generate.go` |

Exit measures:

- A verdict on a fixture's unresolved site passes its checks and waits in
  `review`.
- The contract test passes for Claude Code.
- The hostile fixture changes nothing the runner can do.

## P12: doze as an input

Contract: [fit.md](fit.md); [graph.md](graph.md) §4.1.

| Unit | Work | Files |
|---|---|---|
| P12.1 | The stack file, built in `internal/config` so the daemon and `doze recall stack` both write it, to `.doze/recall/stack.json`: each env key's `ref` (captured while `evaluate` walks traversals), `env_files`, `blocks` fingerprints, tasks, `sources` with hashes (`doze.hcl`, `doze.lock`, env files), `partial` for modules whose plugins aren't fetched, plain-text log paths per app; checked against doze-recall's JSON Schema | `internal/config/eval.go`, `fingerprint.go`, new `stackfile.go`, `internal/daemon/` |
| P12.2 | The `Declared` capability: read-only, never called by plan or sync, in the SDK, the plugin protocol and the host adapter. `Admin.Resources` exists but needs the instance awake; `Declared` is computed from config | `doze-sdk/engine/driver.go`, `doze-sdk/plugin/proto/engine.proto`, `doze-sdk/plugin/adapter.go`, `doze-sdk/plugin/server.go` |
| P12.3 | `Declared` in doze-aws (resources with URLs and ARNs, plus the operations it implements) and doze-kafka (topics, groups); releases: the SDK tagged, both plugins released against it, doze's pins moved | `doze-aws/module/`, `doze-kafka/module/`, `doze-sdk`, `go.mod` |
| P12.4 | `recall` in `reservedBlocks`; the name reserved; the `recall {}` block (no keys) | `internal/config/load.go`, new `internal/config/recall.go` |
| P12.5 | The pinned copy: version and SHA-256 compiled in, downloaded and checked on the first `recall {}` (through `DOZE_MIRROR`), never written to `doze.lock`; `doze recall <args>` writes the stack file if stale and runs it; `doze recall check`; the parity exemption | `cmd/doze/recall.go`, `internal/runtime/`, `cmd/doze/parity_test.go` |
| P12.6 | The dash: the header strip from `doze-recall status --json`; a tab component (there is none; `internal/dash` is about 11.6k lines); recall's screen with Overview, Notes, Review, Topology, Queries, Connect and Generate, each a client of the local API | `internal/dash/` |

Exit measures:

- `.doze/recall/stack.json` for `examples/shop` lists every env key with
  its `ref` or value, the declared queues and topics, and doze-aws's
  operations, and validates against the schema.
- With it, the shop's links turn from inferred to declared, and notes on
  inferred ids are offered for re-anchoring in Review.
- doze downloads the pinned release on the first `recall {}`, checks it,
  and leaves `doze.lock` untouched. doze's idle bench is unchanged.
- Screenshots of every tab compared with the design boards before the phase
  is called done.

## After v1

Not planned in units yet:

- E2, if V1 calls for it, with Python and Rust services in the eval stack;
- observed calls;
- Java, C#, PHP, Ruby and Kotlin at syntax level (a grammar and queries
  each, a few days per language), and at full precision only where a server
  needs no JVM or .NET install;
- per-resource attributes on the aws and kafka plugins (O11);
- the Go rows cut from P4 (confluent-kafka-go, viper, koanf, resty, SaaS
  SDKs);
- bulk re-anchor after a module rename;
