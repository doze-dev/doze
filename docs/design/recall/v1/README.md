# doze recall, first design (parked)

Status: parked 2026-10-24, replaced by [../README.md](../README.md). This was
a code graph for systems of services: language servers, a background process,
links inferred from code, review, 251–370 days of work. It's kept for its
research and because parts may return if the lighter design shows they are
needed. Nothing here is implemented.

Recall, `doze-recall`, is a code graph for systems of services: one picture
of the whole system, built from the code and what people add, served over
MCP to Claude Code, Cursor, Codex or anything else that speaks it. It is
independent of doze. Where a project uses doze, `doze.hcl` adds declared
resources and wiring, and the dash shows recall's screen
([running.md](running.md), [fit.md](fit.md)).

The first exploration lives in the Doze Recall design record
(https://claude.ai/artifact/Eyoyd1TVhFubAqyYQrp214, private to its owner). It
is exhaustive and got much right; these documents start again from the ground,
keep what holds up, and cut what makes recall harder to build or to use.

## How to read this

| Doc | What it answers | State |
|---|---|---|
| [fit.md](fit.md) | How doze feeds recall: the declared-stack file, the pinned copy and `doze recall`, the dash screen | rewritten 2026-10-21 |
| [running.md](running.md) | How recall runs on its own: inputs, the root and apps, inferred resources, the stdio shim and background process, its watcher, review, release, the eval's settings | written |
| [freshness.md](freshness.md) | How the graph and context stay current, and how staleness is shown when they are not | written |
| [generation.md](generation.md) | How the person's own agent CLI (Claude Code, Codex, Cursor…) improves the context, and why it can't make it worse | written; scope narrows after context-system.md §10 |
| [context-system.md](context-system.md) | What agents need from the context by evidence, how doze builds it, budgets, and decisions C1–C9 | decided |
| [research/](research/README.md) | The seven reports and bench programs behind context-system.md and the eval stack; covers the prior art | written |
| [graph.md](graph.md) | Ids, nodes, edges, labels, the declared-stack file, Go extraction, the join, language packs and servers, incremental updates, storage, tests, code layout | written, revised for five languages |
| [answers.md](answers.md) | The six MCP tools, how an answer is built (anchor, expand, rank, pack, render), completeness, errors, adoption | written |
| [context.md](context.md) | Notes, the glossary, `topology.yaml` decisions, proposals, when recall writes, `doze-recall check`, `review` and the dash tabs | written, revised after review |
| [eval.md](eval.md) | How we prove agents do better with recall: questions and ship rules, stacks, task families and format, arms, measures, retrieval eval, cost, order | written |
| [decisions.md](decisions.md) | Every decision, open question and rejection, keeping the design record's ids | written |
| [phases.md](phases.md) | The build order P0–P12: units, files, exit measures and day ranges | written |

## The problem

Every agent session starts cold. It greps, guesses how services connect,
invents library APIs, and misses the change in `api` that breaks `worker`. The
next session pays the same cost again.

Most code graphs stop at symbols and references inside one codebase. What
an agent misses is between services: who produces and who consumes a topic,
a queue, a table or a route, across languages. Recall joins every app's code
at once, and a declared stack where there is one, to give an agent the view a
senior engineer on the team has.

## Three layers, one answer

```
            what an agent receives: one ranked answer, fitted to a budget,
            every item with its evidence and how fresh it is
                                  ▲
   ┌──────────────────────────────┴──────────────────────────────┐
   │ 3. proposed   the person's agent CLI fills gaps and writes   │  may lag, always labeled
   │               flows, contracts, glossary          generation.md
   ├──────────────────────────────────────────────────────────────┤
   │ 2. told       notes, corrections, approvals, committed       │  pinned, flagged on drift
   │               by people                            context.md
   ├──────────────────────────────────────────────────────────────┤
   │ 1. proven     code + declarations + the joins between them   │  current within seconds
   │               (who sends to which queue, which route a       │
   │               client calls)                          graph.md
   └──────────────────────────────────────────────────────────────┘
```

Higher layers never outrank lower ones where they disagree: a human note beats
a generated file, compiler facts beat both. The graph and context layers are
the product. The dash screens, the map and the agent runner are ways in and
out of it.

Of the three parts inside it (what is true, what people know, what to hand
the agent for this task), the third is easy to underrate. A correct graph
dumped raw does not make an agent better. Ranking and packing are designed
as carefully as extraction (answers.md).

## Principles

- **Local, and an aid to whatever agent people choose.** Recall indexes on
  the person's machine and serves over stdio to the agent that started it. Generation drives their own
  local agent CLI with their own login: Claude Code first, then each other
  CLI once it passes the contract test. Doze never generates code itself and
  never ties anyone to one tool: Claude Code, Cursor, Codex or any MCP
  client gets the same context. The eval measures it inside those real
  tools.
- **Simple to use, or nobody uses it.** The whole surface a person meets:
  install one binary, paste one connect command, and optionally run
  `generate` and `review`. No config needed, no query language, a handful of
  tools for agents.
- **Proven first.** Layer 1 alone has to be worth using. AI only adds.
- **Nothing silently stale.** Every answer item says whether it is current
  or stale since which commit (freshness.md).
- **No AI when serving.** Answers come only from data recall already built:
  fast, free, the same every time.
- **Asleep by default.** Recall costs nothing until an agent or a person calls
  it: no process runs with no session open.
- **No edge rather than a wrong edge.** A doubtful link is labeled or sent to
  review, never presented as fact.
- **Build only what the eval rewards.** The retrieval eval runs from the
  first phase and the with-and-without agent eval from its first pilot
  ([eval.md](eval.md)); a layer stays if it moves the numbers.

## Carried over from the design record

Kept: not built on doze-sdk; serving never calls AI; no embeddings and no local models; doze never edits
agent files; task-shaped tools with `file:line` evidence; human notes anchored
to the graph; `topology.yaml` with its ownership rules; generation through the
person's own agent login; the eval.

Changed:

- **The stack join matters more than beating every tool on every language.**
  v1 covers the five languages most doze stacks mix, done well: Go,
  TypeScript, JavaScript, Python and Rust. It doesn't spread thin over many.
- **Generation does checkable work only** (decision C5). Edge verdicts on
  join sites code couldn't resolve, the glossary that bridges plain words to
  code names for search, and checks on stale notes. Flows, contracts and
  conventions wait until the eval shows they help, because the only
  independent study of generated repo context found it hurt. Per-symbol
  descriptions are cut.
- **Independent of doze** (R103). Recall is its own repository and binary,
  started by the agent over stdio, with no proxy, address or built-in
  instance. On code alone it finds apps from their manifests and infers
  resources from the names code uses; a declared-stack file, which doze
  writes, adds declared resources and env provenance. A few terminal
  commands (`review`, `generate`, `status`, `connect`, `check`) replace
  "dash only"; under doze the dash offers the same.
- **One connect line for every repo.** The agent starts `doze-recall serve`,
  which finds the root itself ([running.md](running.md) §4).
- **Headless agent CLIs before ACP** for generation (generation.md).

Cut from v1: the web map, observed wire traffic, module facts, per-app
credentials, eight-language oracle corpus, data-flow analysis.

## Languages

v1 covers **Go, TypeScript, JavaScript, Python and Rust** (R76).

- **Go** is exact in-process through `go/types`.
- **TypeScript/JavaScript** (one pack), **Python** and **Rust** are language
  packs: syntax plus our own resolution, made precise by a pinned
  single-binary language server (tsc 7, ty, rust-analyzer). Recall downloads
  each one and runs it as a child process that stops when idle.
- **Java, C#, PHP, Ruby and Kotlin** can follow after v1 at syntax or scoped
  level ([graph.md](graph.md) §4.6, §10).

The pilot runs once Go and TypeScript/JavaScript work, and Python and Rust
are built while its runs wait ([phases.md](phases.md)).

## Where precision comes from beyond Go (decided, C1)

The design record proposed SCIP indexers per language, shipped through
doze-binaries. Running language servers on demand was the alternative:

| | SCIP indexer | Language server on demand |
|---|---|---|
| Shape | Batch: index the whole project, write a `.scip` file | Query: ask about one reference at a time |
| Needs | A successful build with dependencies installed | Works on partial and broken code |
| Protocol | A different CLI per language | One protocol for every language |
| Good at | A complete precise graph | Settling only the references the syntax layer marks doubtful |
| Cost | Minutes on large repos; doze distributes and pins the indexers | Startup, and gigabytes of memory for tsserver and rust-analyzer on big repos |

Settled by the research ([context-system.md](context-system.md) §4.3):

- No SCIP indexers. Go needs neither (`go/types` runs in-process).
- For other languages, syntax plus our own resolution gives breadth.
- A pinned, single-binary language server that recall downloads settles the
  doubtful references. It runs as recall's child process and stops when idle.

Don't use servers from the project or from `PATH`:

- A project's own `tsserver` runs that project's code.
- Servers usually live inside editor extensions anyway. This machine has no
  `gopls` on `PATH` despite being a Go machine.
