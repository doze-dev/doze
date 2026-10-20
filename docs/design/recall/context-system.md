# The graph and context system

Status: decided 2026-10-20 (C1–C9 as recommended in §10). Built on six
research reports in [research/](research/README.md). The graph contract is
[graph.md](graph.md). Nothing here is implemented.

Recall is worth running only if what it tells an agent is complete where it
says it is complete, current, and small. The research changes what "the
graph" has to be. Finding the right file is mostly solved. Agents fail by
changing one site and missing the others, by using the wrong version of a
library, and by trusting context that is out of date. This document says what
the context must hold to fix that, how doze builds it with what it already
has, and what a person sees while it happens: one line, no installs, nothing
running when nobody asks.

## 1. What agents need, by evidence

Ranked by how directly the evidence supports it. Each row says what recall
holds to meet it. Sources and numbers are in
[research/1](research/1-agent-context-needs.md) and
[research/6](research/6-answer-assembly.md).

| # | Need | Evidence | Strength | Recall holds |
|---|---|---|---|---|
| N1 | **Every site a change touches, and whether that list is complete** | Incomplete propagation is the largest failure class everywhere it is measured: 53% of failed runs in one taxonomy; top cause in DepBench, where 61.8% of failed runs passed the visible tests first | strong need, medium proof that context fixes it | Callers in code and tests, consumers in other apps, SQL migrations, `doze.hcl` wiring and other apps' env, for any symbol, route, queue, topic, table or env var, with a **complete / partial (why)** flag decided by the rules in §4.8. Mentions in other files (fixtures, docs) come as the exact grep to run |
| N2 | **The library version actually pinned** | Version-pinned tasks pass 48–51%; docs for the right version add up to 10 points; an 8 KB index of pointers to version-matched docs took one eval from 53% to 100% (vendor) | strong | Each app's pinned dependency versions and where their source sits on disk (module cache, `node_modules`, venv); each module's engine version and what doze's emulator really implements |
| N3 | **How to run and check each app** | Tooling named in context is used (1.6 uses per task against under 0.01); agents stop at "looks done" without a check | medium-strong | Run command, what must be awake, ports and env var names from the stack; a test command derived from manifests and tasks (§4.1); how to exercise a path across apps |
| N4 | **Links between services** | Cross-repo retrieval gains are about 2.5× single-repo (vendor); no benchmark covers changes across a declared topology | medium, and recall's own eval must prove it | The stack join: producer → queue → consumer, client → route, app → table, env var → reading code |
| N5 | **Source lines with every location** | In a small practitioner pilot, two lines of source per location raised rename success from 0.67 to 0.83 and cut follow-up reads from 15 to 3 | medium | A short snippet beside each `file:line` |
| N6 | **What people know that code doesn't say** | Human-written context files help on average (+4 points), but not measurably for Claude Code; bloated ones get ignored | medium-weak | Notes anchored to graph nodes, shown only with their node, so they never bloat |
| N7 | **What is failing right now** | Agents name the right service from telemetry and still miss the cause; nothing measured on code tasks | weak-medium | Crashes, restarts and recent errors from the journal and log store, tied to code |

## 2. What to leave out

Wrong context steers agents. Models adopt wrong retrieved content over their
own correct knowledge more than 60% of the time, and there is no evidence that
a label saying "possibly stale" makes them check.

| Leave out | Evidence |
|---|---|
| Overviews, per-file and per-symbol summaries | Repository overviews "not effective"; LLM-generated context files lowered success and raised cost 20–23%; on Claude Code's own exclude list |
| Doubtful or stale items mixed in with facts | Over-trust of wrong context (above); labels inside the text help little |
| Large answers | A 1-hop graph (about 2.3k tokens) kept RepoGraph's full gain; 2 hops (about 10.5k) scored below no graph at all; one distractor measurably hurts |
| "Already sent, see earlier" | Clients compact and clear earlier tool results without telling the server |
| Competing with grep on "where is X" in a small repo | Code search tools lowered reward below 400k lines (vendor); in a small pilot, agents picked grep over an LSP tool 94–100% of the time for localization, and forcing the LSP tool first cut success |
| "Always call recall first" in instructions | "MUST invoke first" wording made agents anchor on it and miss project context |

The rule that comes out of this: **answers have two parts.** What recall can
vouch for, and a separate trailing "Unconfirmed" section for everything else
(doubtful edges, agent proposals, stale notes), each with the check to run.
Nothing unconfirmed sits on the main path of an answer.

## 3. The shape

```
 doze.hcl, resolved ──┐
 (via the daemon)     │      ┌──────────────── facts ─────────────────┐
                      ├─────►│ stack     apps, modules, resources,    │
 app source ──────────┤      │           env wiring, run, versions    │
 (change feed)        │      │ code      symbols, refs, calls, routes │
                      │      │           (Go exact; others scoped,    │
 language servers ────┤      │            doubtful settled by server) │──► graph ──► answers
 (hidden doze         │      │ join      code ↔ declared resources    │   (in       (six tools,
  services, on demand)│      │ observed  calls doze's servers saw     │    memory)   no AI)
                      │      │ deps      pinned versions, source path │
 doze-aws, doze-kafka ┤      │ told      notes, topology approvals    │
 recorders, journal   │      │ proposed  agent verdicts, glossary     │
                      │      └────────────────────────────────────────┘
 people, agent CLIs ──┘
```

Every fact records where it came from and which files own it, so a change to
any owner invalidates exactly the facts that depend on it.

### Labels an answer can carry

The README's three layers map onto these: proven is `exact`, `scoped` and
`observed`; told is `told`; proposed is `proposed`.

| Label | Means | Where it appears |
|---|---|---|
| `exact` | a compiler, a language server or `doze.hcl` alone says so | main sections |
| `scoped` | our own analysis proved it: imports and scope, or a value traced through config to the stack | main sections |
| `observed` | doze's own servers saw this app make this call | main sections |
| `told` | a person wrote or approved it, and its anchor has not changed | main sections |
| `doubtful` | a name matched but nothing proved which target | Unconfirmed |
| `proposed` | an agent suggested it and no person has approved it | Unconfirmed |
| `stale` | a note or generated file whose anchor changed since it was checked | Unconfirmed |

Agents see two buckets. The finer labels are for people in the dash and for
the eval.

## 4. Where each kind of fact comes from

### 4.1 The stack

From the daemon's hand-off ([fit.md](fit.md) §3): apps, modules, the
resources nested in them, every reference with the env var it lands in, run
commands, ports, `dir`, engine versions.

Two gaps found in the code ([research/5](research/5-doze-capabilities.md) §5):

- **Which attribute fed which env key is lost after evaluation.**
  `InstanceDecl.Refs` keeps "api references module.db", not "api's
  `DATABASE_URL` is `module.db.url`". Capturing it means walking each env
  expression's traversals during `evaluate`, a small change in
  `internal/config/eval.go`.
- **The queues, topics and buckets inside the aws and kafka modules are
  opaque to doze core**, because plugins decode their own bodies.
  `Inventory.Objects` looks like the fix but isn't. Both plugins converge, so
  answering it would make `doze plan` and `sync` diff queues and topics, and
  removing one would fail. Recall needs a separate read-only `Declared`
  capability ([graph.md](graph.md) §4.1).

Without the second fix the join can't name `queue "jobs"`, so it is the first
thing to build.

`doze.hcl` has no test command, so N3's "how to check it" is derived. It comes
from the app's manifests (`go test ./...` for a Go module, the `test` script
in `package.json`, pytest configuration) and from `task` blocks that the app
waits on. When none of these gives a command, recall leaves it out rather than
guess one.

### 4.2 Code: Go

Go is exact from day one, using the gopls design: per-package type checking
against compiler export data, not loading the whole program at once. On
doze-aws with tests, the two approaches produce the same 577k edges at very
different cost ([research/3](research/3-breadth-speed-memory.md) §2):

| Approach | Peak memory | Time |
|---|---|---|
| `go/packages`, everything at once | 2.2 GB | 1.5 s |
| one package at a time, deps from export data | 86–114 MB | 1.5–2.2 s |
| re-check one package after a save | — | median 1 ms, worst 42 ms (126 ms on Kubernetes) |

A bounded cache of imported packages is the one setting that matters: on the
Kubernetes main module it was the difference between 430 MB and 1.6 GB. A cold
Go build cache costs 5–30 s once, run at low priority.

`go list -export` can fetch modules or a whole toolchain, which would be an
install by the back door. Recall runs it with `GOTOOLCHAIN=local` and
`GOPROXY=off`. A package whose dependencies are not on disk falls back to
syntax, and the Overview says which ones and why.

### 4.3 Code: other languages

v1 covers Go plus TypeScript, JavaScript, Python and Rust (R76). The four
besides Go go through three steps, so each works the first time it is seen
and precision arrives where it is cheap
([research/2](research/2-precision-sources.md)):

1. **Syntax for every language.** gotreesitter (pure Go, 206 grammars) parses
   each file and extracts definitions, references, imports, string literals,
   env reads and call sites. Measured at 2–9× slower than C tree-sitter, which
   is still under 0.5 s per 100k lines of TypeScript per core. It needs care:
   trees must be released right after extraction (keeping them cost 8.5 GB for
   8 MB of source), 2–4 workers, a file-size cap, and only the grammars recall
   uses compiled in.
2. **Our own resolution** through imports and scope labels each reference
   `scoped` or `doubtful`. Syntax alone can't settle much: 52–79% of
   references in the measured repos point at a name declared more than once.
3. **A language server settles the doubtful ones**, by asking for the
   definition at each doubtful site. It is pinned, downloaded by doze and run
   as a hidden service that sleeps (§5).

| In v1 | Languages | Precision source | Download |
|---|---|---|---|
| first | Go | `go/types` in-process | nothing |
| before the pilot | TypeScript, JavaScript | TypeScript 7 native `tsc --lsp`, no Node | 9 MB |
| during the pilot | Python | ty (pyrefly as the alternate) | 12 MB |
| during the pilot | Rust | rust-analyzer, build scripts and proc macros off; doze writes a `rust-project.json` when cargo is absent | 14 MB |

Later, when a real stack asks: PHP, C and C++, C#, Swift, Java, Kotlin, Ruby.
The per-language findings are in research/2 §7.

SCIP indexers are out: most need Node or a JVM and a successful build, and
several have stalled. scip-python's last real commit was 2025-09.
scip-typescript is pinned to TypeScript 6, because TypeScript 7 has no
compiler API yet. If a repo already produces an `index.scip` in CI, recall
reads it, because the format itself is healthy.

What was measured with no project dependencies installed, asking for the
references of every symbol:

| Server | Repo | Symbols asked | References found | Time | Peak memory |
|---|---|---|---|---|---|
| tsc 7 | hono | 9.5k | 14.8k | 20 s | 724 MB |
| ty | pydantic | 17k | 41.5k | 9.5 s | 593 MB |
| rust-analyzer + generated project file | ripgrep | 3.5k | 13.2k | 1.9 s | 125 MB |

These are bulk runs over every symbol. Settling only doubtful sites asks far
fewer questions, at under 1 ms each once the server has loaded.

### 4.4 The stack join

The join is the part no other tool can build, because the hardest half is
declared. Published tools spend most of their effort working out which config
value points at which resource. In doze, `doze.hcl` says so, and static
analysis only has to carry a call's argument back to a literal or an env read
inside one app ([research/4](research/4-stack-join.md)).

```
SendMessage at worker/jobs.go:41
  QueueUrl = cfg.JobsQueue                     value resolver
  cfg.JobsQueue ← env JOBS_QUEUE_URL           config rule (envconfig tag)
  JOBS_QUEUE_URL ← module.cloud:queue.jobs     the stack hand-off
⇒ worker —sends→ queue.jobs   scoped, with the whole chain as evidence
```

Three parts:

- **An operation catalogue** in CodeQL's row format: package, receiver,
  method, which argument names the resource, and the direction (send,
  receive, read, write, call).
  - The AWS rows are generated from AWS's Smithy models (pinned by commit;
    doze-aws only caches them, gitignored) plus AWS's Service Authorization
    Reference. The generated rows are committed. That covers 72% of
    resource-scoped operations and every common data-plane call except four,
    which take about 10 hand-written rows.
  - doze-aws decodes every request it serves, which checks the catalogue for
    free.
  - Hand-written rows cover the Kafka clients (four for Go), SQL drivers,
    HTTP routers and clients, and about 50 external SaaS SDKs. Redis rows come
    from the server's own key specs.
- **A value resolver** over a small domain: constant, env read, concatenation,
  one of several, unknown. Config libraries get their own rules (envconfig,
  caarlos0/env, viper, koanf). Unknown sites are listed, never guessed.
- **The join** with the stack hand-off. An env literal like
  `ORDERS_QUEUE = "orders"` that doesn't reference the block is matched by
  name against that module's declared resources. It is still `scoped`, and
  the evidence says the match was by name.

SQL tables come from sqlc files, literal SQL at modelled call sites, and ORM
conventions. They are parsed with `wasilibs/go-pgquery`, the real Postgres
grammar in pure Go. HTTP links match a client's resolved path template
against the target app's route table, segment by segment. When two routes
match, both are kept as candidates.

Expected accuracy, estimated by the report and to be replaced by the eval:
precision around 0.95 and recall 0.8–0.9 for queue, topic and table edges in
Go. The best published static tool gets 0.92 precision and 0.83 recall on
Java. An LLM exploring the repo alone gets F1 around 0.6.

### 4.5 Observed calls

doze runs the servers apps talk to, so seeing calls is mostly already paid
for:

- doze-aws parses every request's signature and knows the operation and
  resource.
- doze-kafka has an `Observer` that records client id, topics and groups.
- The Postgres proxy filter already reads the startup packet.

Three things are missing, and they are more work than the research first
suggested:

- **Which app made the call.** Nothing records it today. doze does not inject
  AWS credentials into the apps it runs: the `AWS_ACCESS_KEY_ID=doze` in
  `internal/endpoints/endpoints.go:158` only reaches `doze env` output
  (`cmd/doze/env.go:19`). Apps get the daemon's env, then `env_file`, then
  `env {}` (`engine/process/process.go:198-215`), so each app's key is
  whatever its author set. There are three ways to attribute calls, each with
  limits:
  - doze injects a per-app key id where the app hasn't set one. That is new
    env injection, with precedence rules against `env {}`.
  - `PGAPPNAME=<app>`, which loses to an `application_name` in the URL.
  - A source-port → pid lookup over supervised apps. This covers Kafka and
    HTTP between apps, but fails for apps in Docker on macOS.
- **Somewhere to keep observations.** doze-aws keeps 500 calls in memory.
  doze-kafka records only when its console is on. Observations made while
  recall sleeps are lost, so the recorders would need a small persisted feed
  of rows.
- **The Postgres statement tee** described in research/4 §3.2.

Observed edges do two jobs:

- **They confirm what code claims.** Static and observed together give
  precision near 1.0 on the edges that ran.
- **They measure recall on the person's own stack, with no labelling.** An
  edge seen on the wire but not found in code is a measured miss of the
  static layer, which feeds the eval and the Review tab.

When both sources speak:

| Static | Observed | Shown as |
|---|---|---|
| resolved | same resource | `exact` + `observed` |
| resolved | not seen yet | `exact` (it may not have run in this session) |
| two candidates | only one seen | the seen one confirmed; the other `doubtful` |
| nothing | seen | `observed`, with "source not found", which is a gap to fix |
| resolved to A | saw B | conflict, flagged in Review, never merged |

With those costs, observation is worth building only after v1 has shown that
the static join is used (§10, C4).

### 4.6 Dependencies

For each app:

- every direct dependency with its pinned version, from go.mod, lockfiles and
  `Cargo.lock`;
- the path of its source on disk when it is there: the Go module cache,
  `node_modules`, the venv, the cargo registry.

Recall points at it and never copies it, the same way the 8 KB pointer index
worked. Facts about a dependency are keyed `module@version` and cached
machine-wide. They never change for that version, so a save never revisits
them. For doze modules, recall also shows the engine version and what doze's
emulator does not implement.

### 4.7 Told and proposed

- **Notes and topology approvals** are committed markdown and YAML, and
  anchored to graph nodes (context.md, planned).
- **Agent proposals** arrive through generation ([generation.md](generation.md)).

The research narrows what generation should make; see the decisions in §10.

### 4.8 When `impact` may say "complete"

N1 is the top need, so the flag has to mean something. An `impact` answer is
**complete** only when all of these hold. Otherwise it is **partial**, and it
lists each reason that failed, with the grep that would cover the gap.

1. **Every app that could reach the target is indexed for the edge kinds
   involved.** A Go caller set needs the apps that import the package. A
   queue needs every app wired to its module. An app at syntax level only,
   or with a degraded server, makes the answer partial:
   `partial: web is indexed at syntax level`.
2. **No `doubtful` edge points at the target**, and no unresolved call site
   exists for an operation that could name it. If worker has a
   `SendMessage` whose queue recall couldn't resolve, every queue's
   `impact` is partial and names that site.
3. **No hazard can reach it**: reflection, dynamic dispatch through an
   interface whose implementations cross apps, method values passed around,
   names built from strings at run time. These are listed, never treated as
   "no callers".
4. **The index is caught up.** No pending changes in the involved apps,
   including new untracked files. The wake catch-up reads untracked,
   non-ignored files too (`git status -uall`, or FSEvents history). Until it
   finishes, answers say `partial: catching up (12 files)`.
5. **Non-code mentions are out of scope by design.** Fixtures, docs and
   config files that mention the name are not parsed. The answer always
   ends with the exact grep for them: `rg -w OrderCreated -g '!*.{go,ts,tsx,js,jsx,py,rs}'`.

Complete is a claim recall must be able to defend, so the eval checks it: on
every task where recall said complete, any site the reference patch touched
that recall did not list is counted as a false claim.

## 5. Using what doze already has

This is where recall feels light: everything it needs comes up when needed
and goes back to sleep.

| Need | doze machinery | New work |
|---|---|---|
| Recall itself sleeps until called | The proxy boots on connect and holds while connected | A hidden built-in instance ([fit.md](fit.md) §2) |
| Language servers start only when there is work | Synthetic service instances get crash backoff, logs, the journal and idle sleep for free (`config/runnable.go:305`) | A `hidden` flag so helpers stay out of the dash, `doze up` and `doze env`; `doze __lspbridge`, a hidden subcommand that listens on the service's port and pipes to the server's stdio (servers speak stdio, the proxy wakes on a connection); a `recall-` name prefix reserved. Recall adds each helper at run time through the control socket's `add`, once it knows an app's language. A helper block needs only the app's folder, so the live path's lack of sibling references doesn't matter |
| No installs | `__fetch` streams, checks a sha256 and caches under `~/.doze/downloads`; tsc 7's archive unpacks today | Recall's helpers must not use the service `download` path as it is: on first fetch it records the URL in the person's committed `doze.lock` (`runtime/runtime.go:347-354`). Recall passes its own compiled-in `{url, sha256}` table to `__fetch --sha` and records nothing. The archives are mirrored into doze-binaries releases so `DOZE_MIRROR` and offline mirrors work, which plain download URLs don't today. A gunzip-to-one-file fallback (about 15 lines) is needed because rust-analyzer ships `.gz`, not `.tar.gz` |
| Edits while asleep are caught | — | macOS: FSEvents through purego, no cgo. Measured: it replays every change since a saved event id in 13 ms. Linux: `git status -uall` for tracked and new untracked files, plus a stat walk for folders outside git (43 ms for 31k files) |
| Observed calls (after v1) | doze-aws and doze-kafka recorders | App identity, a persisted feed of recorder rows, and the Postgres tee (§4.5) |
| `diagnose` | Journal (`run/journal.jsonl`) and log store, readable from disk even while the stack sleeps | Stack-trace parsing and tying frames to code |
| Stale-since-which-commit | — | A few git helpers; doze has none |

A language server's life:

```
first index of a TS app ─► doze fetches tsc 7 (9 MB, once) ─► service recall-ts-web wakes
  ─► recall asks it about doubtful sites only ─► recall closes its connection
  ─► idle 5 min ─► sleeps (0 MB)
a save in web ─► recall re-asks only that file's doubtful sites, in the background
an agent's question lands on an unsettled site ─► answered now, the site in Unconfirmed
  ─► settling it is queued; the next answer has it
```

Answers never wait on a server. Recall holds the bridge connection only for a
batch of questions and then closes it, so the server's own idle timer can put
it to sleep. Waking it again costs a project reload: seconds, in the
background. Servers run one at a time per stack, so the memory peak is one
server, not one per app.

## 6. Not tedious, not fragile

**What a person does:** adds `recall {}` and pastes one connect line. That is
all.

- Recall builds its first index once, in the background at low priority,
  when the block first appears, because the person has just asked for it.
- After that it runs only when called: an agent session connects, or the
  person opens recall in the dash. Each wake starts with a catch-up of what
  changed while it slept, which is milliseconds when little did.
- `doze up` doesn't start recall or its servers.
- While an agent session stays connected, recall stays awake (the proxy holds
  it). "Asleep" means no session is open.
- The dash header shows one line while recall works (`recall ◐ indexing 2/4`)
  and while it downloads a server (`recall ◐ fetching TypeScript support,
  9 MB`).

**What never happens:**

- A language runtime installed (no Node, no JVM, no Python).
- Project code run to index it. Recall uses only pinned servers it
  downloaded, never one from `node_modules`. Rust build scripts and proc
  macros are off, and Gradle or MSBuild imports are off unless the person opts
  in. Serena shipped a fix in October 2026 because opening a repo ran that
  repo's `tsserver`.
- An answer that waits on a language server or an agent.
- A write to the person's files outside `recall/` and `.recall/`: no
  `doze.lock` entries for recall's own downloads, no git config changes.
- An empty answer that looks authoritative. If recall is asleep, half-built
  or broken, it says so in one line and the agent carries on with grep.

**Guards that catch silent failure.** Each of these was reproduced in the
research:

| Failure seen | Guard |
|---|---|
| rust-analyzer without cargo returns no references and no error | A **canary check** after load: three symbols the syntax layer saw used across files. Zero references for all three marks the server degraded for that app, which falls back to syntax with honest labels |
| Querying rust-analyzer before it is ready loses 78% of references | A readiness rule per server, kept in a quirks table. Four of five servers never report progress, so there is no generic rule |
| pyrefly started one directory too deep finds 4.3× fewer references | doze picks the root: the nearest `tsconfig.json`, `pyproject.toml` or `Cargo.toml` per app |
| Servers ask the client questions and stall if unanswered | Answer `workspace/configuration` and capability requests with defaults |
| Memory blow-ups on big repos | A ceiling (2 GB): past it, stop and switch to doubtful-in-changed-files-only |
| Crashes | One restart with backoff, then degraded for the pass |
| Repo copies counted as code (doze-aws has 12,274 `.go` files under `.claude/` worktrees) | Respect `.gitignore`, skip nested worktrees, `node_modules`, `vendor`, generated and minified files |

Degraded is visible but quiet. The Overview shows `web  ts  syntax (server
degraded: see logs)`, and answers about `web` say "partial: syntax level".

## 7. Performance budgets

From the measurements in [research/3](research/3-breadth-speed-memory.md), to
be enforced in `test/bench` the way the daemon's are:

| State | Budget |
|---|---|
| Asleep (no agent session open) | 0 MB: no recall process, no server; one saved event id per root |
| Awake, serving, up to 1M edges | ≤ 80 MB (flat in-memory graph, about 40 bytes per edge; doze-aws's 577k edges took 17 MB) |
| Indexing, recall process, stacks up to ~250k lines | ≤ 300 MB. Go and tree-sitter passes run one after the other, never together. Go: by package with a bounded import cache (86–114 MB measured on doze-aws). Tree-sitter: 2 workers (155–276 MB measured). Larger stacks keep the budget by using fewer workers and taking longer (serial Kubernetes measured 430 MB, so the import cache bound has to be tuned there) |
| Indexing, a language server | one at a time; typically 0.1–0.7 GB; stopped above 2 GB; stopped 2 minutes after recall releases it, then the service sleeps |
| A save to an up-to-date answer | ≤ 100 ms p95 (syntax languages), ≤ 200 ms p95 (Go, including importers) |
| Wake with nothing changed | ≤ 50 ms |
| First index, ~250k-line stack, warm Go cache | ≤ 5 s for the proven layer; server refinement continues in the background, and answers improve from `doubtful` to `exact` as it lands |

Two simplifications fall out of these numbers:

- **No Zoekt.** Indexing Kubernetes used 951 MB, and literal code search is
  grep's job. Symbol and prose search use SQLite FTS5, with identifiers split
  into a shadow column (`createOrder` also indexed as `create order`), so no
  custom tokenizer is needed.
- **The graph is loaded on wake from a flat snapshot**, tens of milliseconds
  for a million edges, not rebuilt from SQLite. SQLite keeps search, notes
  and the query log.

## 8. Answers

Answer assembly is designed as carefully as extraction. A correct graph handed
over raw makes agents worse. The full pipeline, example schemas and the
reasoning are in [research/6](research/6-answer-assembly.md) §5. In short:

```
parse anchors ─► seed ─► ambiguous? ask ─► freshen cited files ─► expand ─► rank ─► pack ─► render
```

- **Anchors:**
  1. Exact names first: paths, symbols, routes, and the stack's own closed
     vocabulary of app, queue, topic, table and env names.
  2. Then BM25 over split identifiers.
  3. Then the glossary.

  Two close candidates in different apps come back as a question with the
  follow-up call for each, not a guess.

  Plain-language tasks that name nothing ("refunds are emailed twice") are
  the weak spot. Plain BM25 scores about half what embeddings do on code
  search benchmarks. Without the generated glossary, recall bridges the
  vocabulary only through comments, docs, route paths and the stack's
  names.

  The bar, from research/6: file recall@5 of about 80% from task text alone.
  If the eval shows recall below it without a glossary, the glossary stops
  being optional.
- **Expand:** one hop in code by default. Edges across services (route,
  queue, topic, table, env) don't use up the hop budget, so `impact` follows
  a change to its consumers, bounded at 3 such joins.
- **Rank:** personalized PageRank from the anchors, as Aider does, over
  symbols and stack nodes. Weighted by edge type and label; private names,
  tests and names defined many times count less.
- **Pack:** a hard token ceiling. Items are shown as richly as the budget
  allows; overflow becomes a counted "more" line with the exact call that
  gets it.
- **Render:**
  1. the answer in one sentence;
  2. the vouched-for sections;
  3. **Unconfirmed**;
  4. **Next**: two or three calls;
  5. a footer with generation, commit and counts.

| Tool | Answers | Default / cap (tokens) |
|---|---|---|
| `brief` | Starting a task: where it lives, what it touches in the stack, notes, how to run and check it, pinned versions of what it uses | 1,500 / 6,000 |
| `impact` | Changing something, or the current diff: every site, across apps, complete or partial and why | 1,500 / 6,000 |
| `trace` | How a request or message flows, in order, with side effects | 1,500 / 6,000 |
| `find` | Where a name is defined or declared anywhere in the stack (not literal text: that is grep) | 600 / 3,000 |
| `topology` | Which apps and resources exist and who talks to whom | 1,000 / 6,000 |
| `diagnose` | An error, a failing route or a crashed app: likely origin, path, live state | 1,500 / 6,000 |

`review` folds into `impact` with a diff.

**Adoption is designed, because agents don't call tools unprompted.**

- The server instructions carry the triggers in their first 512 characters,
  which Codex asks to be self-contained. Claude Code cuts instructions at
  4,096.
- Tool descriptions start with the words an agent would search for, and
  include one example input each.
- `brief` alone is always loaded.
- Every tool is marked read-only, so Codex runs it without prompting.
- The optional AGENTS.md lines from the Connect tab matter more than they
  look: subagents never see MCP instructions.
- The eval measures adoption per client.

**Instead of skipping what a session already received:** answers are small
enough to repeat whole. An agent-held `since` token, which would return
unchanged items as one-line stubs, was considered and left out of v1: it needs
item history across generations ([answers.md](answers.md) §7).

## 9. Corrections to the docs already written

Fixed in place already, because they were errors:

- freshness.md said FSEvents needs cgo. It works through purego.
- freshness.md said recall skips what a session already received. Answers
  are now repeated whole.
- freshness.md served stale notes and generated files beside facts. They now
  go to Unconfirmed.
- freshness.md loaded the graph from `graph.db`. It now loads a flat
  snapshot.
- The README's line about `tsserver` from `node_modules` broke §6's rule.

Applied after the decisions, on 2026-10-20:

- fit.md §7–§8: the helper pieces, the `Declared` capability, env provenance, and
  the budget.
- README.md: languages and precision.
- generation.md: jobs narrowed per C5.

## 10. Decisions

All nine taken as recommended on 2026-10-20.

| # | Decision | Taken | Why |
|---|---|---|---|
| C1 | Precision beyond Go | Syntax + own resolver for all languages; pinned single-binary servers for TS/JS, Python and Rust; no SCIP indexers | The only route with no installs and no build; measured on real repos |
| C2 | Language servers as hidden doze services (with `__lspbridge`) or as recall's own child processes | **Hidden services** | Wake on demand, idle sleep, crash backoff and logs come free; the bridge is small |
| C3 | When indexing runs | **Once in the background when `recall {}` first appears; afterwards only when recall is woken**, starting with a catch-up | An agent's first question never meets a cold index, and nothing runs at `doze up` or while nobody asks |
| C4 | Observed calls | **After v1**, once the eval shows the static join is used. Then app identity, a persisted recorder feed and the Postgres tee | Valuable (precision near 1.0, a free recall measure), but doze injects no per-app identity today and the recorders forget; it is real work, not a few lines |
| C5 | Generation's jobs | **Edge verdicts on unresolved join sites, the glossary (the vocabulary bridge for search, since there are no embeddings), note checks.** Flows, contracts and conventions only once the eval shows they help | The only independent study of generated repo context found it hurt; verdicts and glossary entries can be checked |
| C6 | Change feed | **FSEvents (purego) on macOS, inotify on Linux, git to reconcile**, owned by the daemon; move `watch` onto it too | Zero cost while asleep, exact replay on wake; polling a large tree costs about 4% of a core |
| C7 | Search | **FTS5 with split identifiers; no Zoekt** | Literal search is grep's job; Zoekt's indexing peak is too heavy |
| C8 | Languages | **All five in v1:** Go, then TypeScript/JavaScript before the pilot, Python and Rust during it (revised 2026-10-20) | Most doze stacks mix languages; a Go-only recall would say "partial" on the very cross-app questions it exists for. The pilot still comes early |
| C9 | The pasted AGENTS.md lines | **Keep "doze never edits agent files", but make the Connect tab's snippet prominent** and measure its effect | Present context gets used far more than tools that must be chosen; subagents see only files |

Still open, not blocking these:

- Whether agents in sandboxes (Claude Code's sandbox, cloud agents) can reach
  a `.doze` name or loopback. This decides recall's address ([fit.md](fit.md),
  Open).
- Whether recall drops its in-memory graph when a session stays open but
  idle for a long time. Reloading a million edges from the snapshot takes
  tens of milliseconds, so dropping it is cheap.

[graph.md](graph.md) writes up §3–§4 and §6–§7 as the contract; answers.md
will do the same for §8.
