# Decision log

Status: 2026-10-20. Every recall decision in one place: what was decided,
what is still open, and what was rejected, so nothing is reopened by accident.

The ids keep the design record's numbering (R1–R74, O1–O8, X1–X10) so
earlier discussion stays traceable. Decisions made since then are added as
R75 onwards, alongside the series the newer documents use (C1–C9 in
[context-system.md](context-system.md), V1–V4 in [eval.md](eval.md)).

| Status | Means |
|---|---|
| decided | settled; the document named in the last column is the contract |
| changed | decided, but differently from the design record; the row says how |
| open | needs an answer, before the phase named |
| deferred | not in v1; may come back |
| rejected | not doing it; kept so it isn't proposed again |

## Product and shape

| # | Decision | Status | Where |
|---|---|---|---|
| R1 | The facility is called recall | decided | |
| R2 | Recall is core and bundled in every doze release | decided | |
| R3 | Recall is not a module, service, app or task. One per stack, turned on by a `recall {}` block | decided | [fit.md](fit.md) §1 |
| R4 | Not built on doze-sdk; users can't change how it works | decided | |
| R5 | No interactive recall CLI commands; actions run from the dash, MCP is the programmatic surface. **Changed:** one headless exception, `doze recall check`, for CI | changed | [fit.md](fit.md) §6 |
| R6 | Asleep until called, woken through the proxy. **Changed:** the address (`recall.<stack>.doze`, a machine-wide address, or per project) is open, see O1 | changed | [fit.md](fit.md), "Connecting an agent" |
| R7 | The name `recall` is reserved in every stack | decided | [fit.md](fit.md) §1 |
| R8 | Recall's dash screens are designed on their own, not as a per-kind front | decided | |
| R9 | Recall is exempt from "every dash action has a CLI form"; `parity_test.go` knows it | decided | [fit.md](fit.md) §6 |
| R10 | Read-only toward the stack in v1 | decided | |
| R11 | Only `include` and `exclude`, on top of `.gitignore` | decided | [fit.md](fit.md) §1 |
| R12 | Recall shows as a strip in the dash header, not as a row | decided | [fit.md](fit.md) §5 |
| R13 | A separate process, supervised with the existing state machine, journal and logs | decided | [fit.md](fit.md) §2 |
| R14 | A second Go module in the doze repo (`recall/`), its own binary in the same release | decided | [graph.md](graph.md) §11 |

## Serving agents

| # | Decision | Status | Where |
|---|---|---|---|
| R15 | Serving never calls AI | decided | [answers.md](answers.md) |
| R16 | No live vector search | decided | |
| R17 | Doze never edits agent files; the Connect tab shows lines to copy | decided (C9) | [answers.md](answers.md) §10 |
| R18 | Task-shaped tools. **Changed:** six tools, `brief`, `impact`, `trace`, `find`, `topology`, `diagnose`; `review` is `impact` on a diff; `verify` waits behind R10 | changed | [answers.md](answers.md) §2 |
| R19 | Every answer carries `file:line` evidence and fits a budget, with confirmed items apart from the rest. **Changed:** answers have an Unconfirmed section rather than inline confidence; "skip what the session already received" is dropped | changed | [answers.md](answers.md) §3.8, §7 |
| R20 | MCP instructions teach agents the stack. **Changed:** instructions are generic, because they're sent before the server knows the stack; stack names reach agents through `brief` and `topology` | changed | [answers.md](answers.md) §1 |

## Graph and languages

| # | Decision | Status | Where |
|---|---|---|---|
| R21 | Recall supports many languages. **Changed:** v1 is five, done well: Go, TypeScript, JavaScript, Python, Rust (R76); Java, C#, PHP, Ruby and Kotlin later at syntax or scoped level | changed | [graph.md](graph.md) §4.6, §10 |
| R22 | Four precision levels: compiler, SCIP, own resolvers, syntax | rejected; replaced by C1 | [context-system.md](context-system.md) §4.3 |
| R23 | Precise first; no edge rather than a wrong edge. **Changed:** trust is carried by labels (`exact`, `scoped`, `observed`, `told`, `doubtful`, `proposed`) rather than a ranked source list | changed | [graph.md](graph.md) §2 |
| R24 | A language is a pack of data plus small hooks | decided | [graph.md](graph.md) §4.6 |
| R25 | Framework rules form the contract layer, linked to `doze.hcl` | decided, as the operation catalogue | [graph.md](graph.md) §4.3 |
| R26 | Index beyond code. **Changed:** v1 reads `doze.hcl` (through the hand-off), env files and SQL migrations; Dockerfiles, compose, Kubernetes, Terraform, CI and git churn are deferred | changed | [graph.md](graph.md) §4 |
| R27 | Index dependencies at their pinned version, cached machine-wide | decided | [graph.md](graph.md) §4.4 |
| R28 | Oracle tests per language in CI; an eval with and without recall | decided | [graph.md](graph.md) §9, [eval.md](eval.md) |
| R29 | AI may help write extractors and rules while building recall, gated by the oracle tests; it never generates the graph | decided | |
| R52 | Build the referee first. **Changed:** the Go oracle and the retrieval eval come first (E0); a multi-language corpus waits for the packs | changed | [eval.md](eval.md) §8 |
| R53 | Extraction, resolution, ranking and packing are our own Go implementation | decided | |
| R54 | No code copied from other code-graph tools, whatever their licence | decided | |
| R55 | SCIP symbol syntax for ids, a Glean-like layered schema | rejected; replaced by R75 | [graph.md](graph.md) §1 |
| R56 | Personalized PageRank from the question's anchors, packed to a budget | decided | [answers.md](answers.md) §3.6 |
| R57 | `impact` lists hazards; zero callers is never proof | decided | [graph.md](graph.md) §3 |
| R58 | Shipping bar per language. **Changed:** precision ≥ 0.98 for `exact` and `scoped` edges on the pack's oracle corpus, recall reported and not regressing | changed | [graph.md](graph.md) §9 |
| R59 | SCIP indexers as separate pinned programs | rejected (C1) | [context-system.md](context-system.md) §4.3 |
| O8 | Language servers as a precision source | decided (C1): the precision source for the v1 packs, pinned single binaries run as hidden services | [graph.md](graph.md) §4.7 |

## Storage

| # | Decision | Status | Where |
|---|---|---|---|
| R30 | SQLite through a pure-Go driver is the store of record | decided; driver open | [graph.md](graph.md) §7 |
| R31 | Traversal runs on an in-memory graph. **Changed:** loaded from a flat snapshot, not from SQLite | changed | [graph.md](graph.md) §7 |
| R32 | Zoekt for code search, FTS5 for symbols and prose. **Changed:** no Zoekt (C7); FTS5 only, identifiers split, prose stemmed | changed | [graph.md](graph.md) §7 |
| R33 | `graph.db` is a disposable cache; people's decisions live in committed files | decided | [graph.md](graph.md) §7 |
| R34 | One change feed owned by the daemon, shared with `watch`; recall writes `status.json` | decided (C6) | [freshness.md](freshness.md) |

## Generation

| # | Decision | Status | Where |
|---|---|---|---|
| R35 | No local models | decided | |
| R36 | Generation runs through the person's own agent CLI, with their login; never when serving | decided | [generation.md](generation.md) |
| R37 | AI output takes the form of semantic files. **Changed:** v1 jobs are edge verdicts, the glossary and note checks; contracts, flows, conventions and area names wait for the eval (C5) | changed | [generation.md](generation.md) |
| R38 | In-session agent tricks are dropped | decided | |
| R39 | One job format for every generation job | decided | [generation.md](generation.md), "A job, in full" |
| R40 | AI only chooses among candidates code found, cites what recall checks, never outranks proven facts or people | decided | [generation.md](generation.md) |
| R41 | Consent per stack; scope shown first; secrets and excludes never reach the agent; runs can be stopped | decided | [generation.md](generation.md) |
| R42 | From Graphify: prompt-versioned caches and untrusted-source wrapping | decided | [generation.md](generation.md) |
| R48 | Local agents are the only generation runner | decided | |
| R49 | Agents driven through ACP. **Changed:** headless CLI modes first, with the agent's only tools being recall's; ACP only if headless modes prove too uneven | changed | [generation.md](generation.md) |
| R50 | No embeddings anywhere | decided | |
| R51 | Agents in three layers: a `doze.hcl` allowlist, a personal choice, built-in launch paths | decided | [generation.md](generation.md) |

## Human context

| # | Decision | Status | Where |
|---|---|---|---|
| R43 | Notes are markdown with `about` anchors, in `recall/` and `.recall/` | decided | [context.md](context.md) §2 |
| R44 | Existing prose is read, never written | decided | [context.md](context.md) §5 |

## Runtime and watch

| # | Decision | Status | Where |
|---|---|---|---|
| R45 | Observe wire traffic by default | deferred (C4): after v1, once the static join is shown to be used; doze injects no per-app identity today | [context-system.md](context-system.md) §4.5 |
| R46 | `watch` uses the shared change feed | decided (C6), deferred: the move happens after the pilot, once the feed has run under recall | [phases.md](phases.md) |

## Stack and external services

| # | Decision | Status | Where |
|---|---|---|---|
| R60 | Every resource is a first-class node, addressed inside its module | decided | [graph.md](graph.md) §1 |
| R61 | Client rules per SDK and driver find operations and resolve resource names | decided; Go, TypeScript/JavaScript, Python and Rust rows in v1 | [graph.md](graph.md) §4.3 |
| R62 | Modules publish facts through the daemon | deferred; the read-only `Declared` capability (R77) covers declared resources | [graph.md](graph.md) §4.1 |
| R63 | Per-app credentials for attribution | deferred (C4) | |
| R64 | A `resource` semantic file kind | rejected; facts live in the graph and `topology.yaml` | |
| R65 | Services doze doesn't run appear as `external` nodes; secret values never read | decided | [graph.md](graph.md) §1 |
| R66 | A federated graph of per-app shards. **Changed:** the unit is a Go module, indexed once per machine and reused by every stack; one graph per stack | changed | [graph.md](graph.md) §1, §7 |
| R67 | Services publish an interface file for stacks that don't run them | deferred | |
| R68 | Contract drift detection. **Changed:** v1 detects drift of people's decisions (`doze recall check`); field-level drift (event fields, response shapes) is deferred | changed | [context.md](context.md) §5, §8 |
| R69 | External dependencies discovered from code, without doze modules | decided | [graph.md](graph.md) §4.3 |
| R70 | `recall/topology.yaml`, committed and human-readable. **Changed:** it holds only people's decisions (approved, rejected, asserted), keyed by code id; the generated view of every edge is not committed, because rewriting a committed file on every index caused noise, races and merge conflicts | changed | [context.md](context.md) §5 |
| R71 | Ownership rules. **Changed:** recall writes committed files only when a person presses a key in the dash; decisions are never changed by recall; rejections are kept; agent proposals wait in an uncommitted `proposals.json` | changed | [context.md](context.md) §1, §5 |
| R72 | The loop: index, generate on what's unresolved, review as diffs. **Changed:** the web map is deferred | changed | [generation.md](generation.md), [context.md](context.md) |
| R73 | Agents reach recall's files only through MCP; the AGENTS.md lines are safe without doze | decided | [answers.md](answers.md) §10 |
| R74 | Topology screens in the dash. **Changed:** the web map is deferred; approving and editing stay in the dash | changed | [context.md](context.md) §6 |

## Since the design record

| # | Decision | Status | Where |
|---|---|---|---|
| C1 | Precision beyond Go: syntax + own resolver; pinned single-binary language servers; no SCIP indexers | decided | [context-system.md](context-system.md) §10 |
| C2 | Language servers as hidden doze services through `__lspbridge` | decided; in v1 | [graph.md](graph.md) §4.7 |
| C3 | Index once when `recall {}` first appears, then only when woken | decided | [graph.md](graph.md) §5.3 |
| C4 | Observed calls after v1 | decided | [context-system.md](context-system.md) §4.5 |
| C5 | Generation does checkable work only | decided | [generation.md](generation.md) |
| C6 | FSEvents through purego, inotify, git to reconcile; the daemon owns the feed | decided | [freshness.md](freshness.md) |
| C7 | FTS5 only, no Zoekt | decided | [graph.md](graph.md) §7 |
| C8 | All five languages in v1: Go, then TypeScript/JavaScript before the pilot, Python and Rust during it | changed 2026-10-20 (was: one at a time after v1) | [phases.md](phases.md) |
| C9 | Never edit agent files; make the Connect snippet prominent | decided | [answers.md](answers.md) §10 |
| R75 | Code ids are keyed by the code's own name (Go import path), never by app; which app uses a package is a separate fact | decided | [graph.md](graph.md) §1 |
| R76 | v1 covers Go, TypeScript, JavaScript, Python and Rust; observed calls and further languages are designed but not built. **Changed** 2026-10-20 from "Go only": most doze stacks mix languages | changed | [graph.md](graph.md), [phases.md](phases.md) |
| R77 | Declared resources reach recall through a new read-only `Declared` capability, not `Inventory.Objects`, which would make plan and sync diff plugin resources | decided | [graph.md](graph.md) §4.1 |
| R78 | The daemon writes the hand-off to `stack.json` on every config load | decided | [graph.md](graph.md) §4.1 |
| R79 | One writer queue for passes and refreshes; answers read immutable generations | decided | [graph.md](graph.md) §5.1 |
| R80 | The operation catalogue has five shapes: argument, literal, option, output, identity | decided | [graph.md](graph.md) §4.3 |
| R81 | AWS catalogue rows are generated from pinned Smithy models and committed | decided | [graph.md](graph.md) §4.3 |
| R82 | `impact` says complete only under the rules in context-system.md §4.8; it always ends with the grep for non-code mentions | decided | [answers.md](answers.md) §6 |
| R83 | Text recall didn't write is quoted as data in answers; secrets are redacted in answers and the query log | decided | [answers.md](answers.md) §3.8 |
| R84 | Cursors carry their generation; a moved index restarts paging | decided | [answers.md](answers.md) §3.7 |
| R85 | No `since` tokens in v1 | decided | [answers.md](answers.md) §7 |
| R86 | Recall's downloads never touch the person's `doze.lock` | decided | [fit.md](fit.md) §7 |
| R87 | Notes and the glossary use full ids only; short names are flagged and expanded on a keypress, never resolved silently | decided | [context.md](context.md) §3 |
| R88 | Note staleness compares each anchor with its state at the note's `checked` commit, from git, ignoring positions, comments and new callers | decided | [context.md](context.md) §3 |
| R89 | Files are validated per entry; one bad entry never disables a whole file | decided | [context.md](context.md) §4, §5 |
| R90 | `doze recall check` runs the `doze-recall` binary, so recall's dependencies stay out of `doze`; the hand-off is built in `internal/config` so it works without a daemon | decided | [phases.md](phases.md) P1.1, P10.5 |
| R91 | Generation is built after the pilot, since the pilot doesn't use it | decided | [phases.md](phases.md) |
| R92 | The eval stack and tasks are written by agents walled off from recall, starting at P0 | decided (follows V3) | [phases.md](phases.md), [eval.md](eval.md) §3 |
| R93 | Windows polls for changes in v1 and is unverified until doze has Windows CI | decided | [phases.md](phases.md) |
| V1 | How strong a claim v1 ships on | open, decided after E1 | [eval.md](eval.md) §10 |
| V2 | The eval runs on the Claude Code subscription, Sonnet pinned, E1 an 80-run pilot | decided | [eval.md](eval.md) §9 |
| V3 | Eval tasks are written by agents: walled-off sessions, Opus authoring, a reviewer agent, the owner reading a sample | decided | [eval.md](eval.md) §3 |
| V4 | The eval stack is our own, `parcel`, built by agents: six Go and TypeScript apps for E1, a Python and a Rust app for E2, over doze-aws, Kafka and Postgres; sized to stay outside the agent's context (40–50k lines for E1) and to hold E2's tasks (36+ cross-app links); private. Replaces porting Jitsu, decided earlier the same day | decided | [eval.md](eval.md) §2, [research/7](research/7-eval-stack.md) |
| R95 | E0 and E1 run in Claude Code only; Codex and Cursor plans are obtained before E2, and their generation runners (P11) follow once they pass the contract test | decided | [eval.md](eval.md) §4 |
| R94 | No backend runs during the eval. Every task is scored by unit and contract tests with no services; "run and verify" tasks are out; diagnose tasks use recorded logs | decided | [eval.md](eval.md) §2, §3 |

## Open questions

| # | Question | Before | Where |
|---|---|---|---|
| O1 | Recall's address: per stack, machine-wide with routing, or per project; and the address when `domains = false` | P0.1 | [fit.md](fit.md) |
| O4 | Memory ceiling on a big repo. **Partly answered:** budgets measured (≤ 300 MB indexing on doze-aws-sized code); the import LRU bound for Kubernetes-sized modules is still open | P2.5 | [graph.md](graph.md) §12 |
| O5 | Is a machine-wide cache under `~/.doze` acceptable, or must everything stay in the project? | P0.5 | [fit.md](fit.md) |
| O6 | Observation on or off by default | moot until observation exists (C4) | |
| O7 | gotreesitter speed. **Answered:** measured 2–9× slower than C, workable with the rules in context-system.md §4.3 | — | [research/3](research/3-breadth-speed-memory.md) |
| O9 | Can agents in sandboxes (Claude Code's sandbox, cloud agents) reach a `.doze` name or loopback? | P0.2 | [fit.md](fit.md) |
| O10 | Which pure-Go SQLite driver | P2.9 | [graph.md](graph.md) §12 |
| O11 | Per-resource attributes on the aws and kafka plugins, which would make queue wiring `exact` | any time | [graph.md](graph.md) §12 |

## Rejected

| # | What | Why |
|---|---|---|
| X1 | Recall as a doze module | a module sees only its own block |
| X2 | A `doze mcp` stdio command and shim | superseded by serving over HTTP through the proxy |
| X3 | A separate `doze-recall` repo | superseded by R14 |
| X4 | Embeddings with a local model; live vector search | R16, R35, R50 |
| X5 | Any AI call while serving | R15 |
| X6 | AI generating graph edges freely | made-up edges, not deterministic, cost grows with code |
| X7 | A skill as the main way to teach agents, or editing AGENTS.md | R17 |
| X8 | Turso or a graph database as the store | R30; benchmarked slower |
| X9 | API keys and provider calls made by recall itself | local agents use logins people have |
| X10 | Data-flow analysis in v1 | too heavy for v1; hazards are found syntactically instead |
| R47 | Doze credits for people without keys | dropped with X9 |
| R22, R55, R59, R64 | see their rows above | replaced |
