# doze recall

Status: design, 2026-10-24. Replaces the first design, now parked in
[v1/](v1/README.md). Nothing here is implemented yet.

## What recall is for

An agent should never pay twice to learn the same thing about a stack.

Every agent session starts cold today. It lists folders, greps, and reads the
same large files again to work out what each app does, how the apps connect,
how to run things and where code lives. The next session does it all again.
Recall keeps what has been learned and hands it to the next session cheaply,
over MCP.

Recall is its own binary, `doze-recall`, the way doze-aws and doze-kafka are,
with a different purpose. It runs in two ways (§3):

- **In a doze stack,** declared with `recall {}` in `doze.hcl`. It's part of
  the stack: doze runs it, wakes it when an agent connects and puts it to
  sleep when idle, like any other instance. It knows the whole stack, because
  doze tells it.
- **On its own,** for one project with no doze: trees and knowledge for that
  project, with no stack map. This is for people who just want context for a
  codebase and know nothing about doze stacks.

The stack's shape always comes from `doze.hcl`, never from guessing. With no
`doze.hcl`, there's no stack map.

## What agents keep re-learning, and who supplies it

| What the agent keeps re-learning | Supplied by |
|---|---|
| Which apps exist, how they're wired, which queues, topics, buckets and databases sit between them | **facts**: from `doze.hcl` |
| How to run, test and see the logs of each app | **facts**: from `doze.hcl` and doze |
| Where things are in an app: folders, entrypoints, files, the main functions and types | **facts**: from the code |
| Which files read which env key, so which code touches which resource | **facts**: env keys doze knows, matched in the code |
| Routes, handlers, consumers, producers | **knowledge**: written by agents |
| What an app or module does; flows across apps ("checkout: storefront → orders → billing") | **knowledge** |
| Conventions and gotchas ("amounts are in cents", "billing is never called synchronously") | **knowledge** |

There are two layers:

- **Facts** are generated, exact, and never written by AI. Recall builds them
  when it's asked, so they are always current.
- **Knowledge** is written by agents as a side effect of their work, and
  edited by people when they want to. It is saved, cites the files it came
  from, and says when those files have changed since.

## 1. Facts

### 1.1 The stack map

In a stack only. doze writes it to `.doze/recall/stack.json` on every config
load, and `doze-recall` reads it, so recall never parses HCL and the two can't
disagree about what the stack is. It holds:

- **Apps:** name, folder, language, port, run and test commands, where the
  logs are (`doze logs <app>`).
- **Wiring:** each env key and where its value comes from, for example
  `orders: DATABASE_URL ← postgres.orders.url` and
  `SHIPMENTS_TOPIC ← kafka.topic.shipments`.
- **Resources:** each queue, topic, bucket and database, and the apps wired
  to it.
- **Order:** dependencies and boot order.

Two gaps in doze to close, both doze-side:

- **Env provenance.** Which attribute fed which env key is lost after
  evaluation. Keep the map by walking each env expression's traversals.
- **Resources inside modules.** The queues and topics declared inside the
  aws and kafka modules are opaque to doze core. A read-only `Declared`
  capability in doze-sdk, implemented by doze-aws and doze-kafka, returns
  them. It is not part of plan or sync, so it can't change what they converge.

### 1.2 App trees

For each app, a compact tree an agent can use instead of `ls`, `grep` and
reading files to orient itself:

- **Folders,** collapsed to a few levels with file counts.
- **Files that matter,** each marked by role:
  - entrypoints: `package main`, `package.json` `main` and scripts,
    `__main__`, a Rust `main.rs` or `[[bin]]`;
  - tests;
  - migrations, by folder and naming convention;
  - generated files, by banner (`Code generated … DO NOT EDIT`,
    `@generated`), shown collapsed;
  - config.
- **Symbols per file:** the functions, types and methods each file defines,
  as signatures.
- **Env keys** each file reads, joined to the stack map: `SHIPMENTS_TOPIC` is
  read in `internal/events/publish.go`, and it's the `shipments` topic, which
  dispatch is also wired to.
- **Ranked to fit a budget** in the style of Aider's repo map: a large app
  doesn't fit a prompt, so the most-referenced symbols and the files near
  entrypoints come first, and the agent can ask for one folder or file in
  full.

The generator does only what's the same in every codebase. It does not try
to find routes, handlers or consumers. There are too many frameworks to keep
correct (chi, gin, Express, Fastify, Next.js, FastAPI, Flask, axum, actix,
four Kafka clients, SQS pollers…), and an agent recognizes them in seconds
once it is looking at the file. Those go into knowledge (§2).

### 1.3 Staying current

Every answer is checked against the files on disk when it's asked:

1. List files with `git ls-files` plus untracked files not ignored.
2. Compare each file's size, mtime and content hash with the cache.
3. Re-parse only what changed.
4. Build the answer from the cache.

That alone keeps answers correct, in both modes. In a stack, recall is also
awake for as long as an agent is connected (§3.1), so it can do the work
before it's asked:

- on waking, it reconciles the cache in the background, so the first answer
  doesn't wait;
- while awake, it watches the project's files and re-parses on save;
- it re-reads `stack.json` when doze rewrites it.

The watcher is only a head start. If it misses something, step 2 still
catches it on the next call. The cache is machine state, under
`~/.doze-recall/` (alone) or doze's data folder for the instance (in a
stack).

### 1.4 Parsing

`doze-recall` is a separate binary, so cgo is allowed in principle. But every
doze binary today is pure Go and fully static (`CGO_ENABLED=0` in doze's and
doze-aws's `.goreleaser.yaml`), cross-compiled for every platform from one
Linux runner, and downloaded by doze as one file with nothing else to
install. Keeping recall the same keeps releases and installs the same.
Checked 2026-10-24:

| Option | What it is | For | Against |
|---|---|---|---|
| **gotreesitter** (`github.com/odvcencio/gotreesitter`, MIT) | Tree-sitter's runtime rewritten in pure Go; loads the same parse tables | No cgo, so it builds and ships like every other doze binary. 206 grammars. Full query language with predicates. Every benchmark fixture is checked against the C runtime's trees | About 3–5× slower than C on a full parse (geomean 4.8× on Go files). Released very often (v0.56 by Oct 2026), with a small maintainer base. Binary size not published |
| **wazero** | Tree-sitter compiled to a WASI module, run in Go's pure-Go wasm runtime | No cgo, and the official C code | No maintained library: the one example found (`vista-forge/m-parse`) is a one-grammar spike that needs an emscripten build and a hand-written C shim because wasm can't pass tree-sitter's structs. Modules aren't reentrant, so parsing is serialized. We'd own the build of the runtime and every grammar |
| **Official binding** (`github.com/tree-sitter/go-tree-sitter`) | cgo | Fastest, the reference, used by most of the ecosystem | cgo: needs a C toolchain per platform, native runners (or zig) for macOS, Linux and Windows releases, and links libc dynamically unless extra work is done. The only doze binary built that way |
| **Universal Ctags** | A separate C binary that lists symbols for 100+ languages | Zero parsing code of ours | Another binary to ship; coarser output |

**Choice: gotreesitter, if the spike (S0) holds.** A 4.8× slower parse
matters little here: the cache means only changed files are parsed, and a
first pass over a 10k-file app still takes seconds when spread across
cores. The spike measures binary size with Go, TypeScript and TSX, Python
and Rust, parse time and memory on a real app, and how good the `tags.scm`
output is. If it fails, the fallback is the official binding, accepting a
cgo release for this one binary. wazero isn't worth owning.

## 2. Knowledge

### 2.1 What it is

Small markdown files in `.recall/` beside `doze.hcl`, committed with the
code, one file per subject:

```
.recall/
  apps/billing.md          what billing does, its routes and consumers, its gotchas
  apps/orders.md
  flows/checkout.md        a flow across apps
  notes/money-in-cents.md  a convention or gotcha that spans apps
```

Each file has a short header:

```markdown
---
about: app billing
sources:
  apps/billing/cmd/billing/main.go: 3f9a…
  apps/billing/internal/http/routes.go: 81c2…
  apps/billing/internal/queue/invoices.go: 0d4e…
---

Billing turns paid orders into invoices and sends them.

Routes: POST /invoices (routes.go:22), GET /invoices/{id} (routes.go:31).
Consumes `orders.paid` from Kafka (queue/invoices.go:14) …
```

`sources` is the list of files the piece was written from, with their content
hashes when it was written.

### 2.2 How it gets written

Agents write it while they work, without a separate run or bill:

1. An agent asks recall about `billing`.
2. Recall returns the facts and the knowledge piece, if one exists.
3. If no piece exists, or files it cites have changed since it was written,
   the answer says so: "No notes on billing yet. You'll be reading its code
   anyway: when you've understood it, save what you learned with
   `remember`." Or: "Written against an older `routes.go` and `invoices.go`.
   Check those two and update this if it's wrong."
4. The agent has to read that code for its own task anyway, so writing it
   down costs a few hundred tokens. The next session gets it for almost
   nothing.

The cost is paid once per change, inside sessions people are already
running. When a cited file changes, only the pieces citing it are stale, and
the answer names exactly which files to re-check rather than saying the
whole piece is untrustworthy.

`remember` checks what it's given:

- every cited file exists, and recall records its current hash itself;
- the piece stays under a size cap;
- it doesn't contain anything that looks like a secret;
- it lands in the file for its subject, replacing that file's contents
  rather than appending, so files don't grow without bound.

Optionally, `doze recall enrich` fills the gaps in one go for a team that
wants everything written on day one. It runs the person's own agent CLI
headless (`claude -p`, `codex exec`) on each app with no piece yet. It uses
their subscription, needs no API key, and nothing depends on it.

### 2.3 People in the loop, without the annoyance

The rule is that recall never blocks, never asks, and never notifies.
People step in only in ways they already work:

- **Say it in chat.** "Remember that billing must never call dispatch
  directly." The agent calls `remember`. This is expected to be the main way
  people correct things.
- **Edit the file.** The pieces are plain markdown in the repo, so a person
  can open one in their editor and fix it.
- **See it in review.** Knowledge changes appear in the same pull request as
  the code change that prompted them, so a reviewer sees "routes.go changed,
  and billing.md was updated to match" in one place. Files stay small, one
  subject each, with stable order and no timestamps, so diffs are short and
  easy to skip.

Human words win, with no ceremony. Recall remembers the hash of what it last
wrote to each file. If a file's contents differ from that, someone edited it
by hand, so recall treats it as the person's:

- agents may still read it and flag it as possibly stale;
- `remember` won't overwrite it, and returns "billing.md was written by a
  person: tell them what you'd change instead".

Nothing to approve, no queue, no badges. The dash may later show a quiet
list of recently written pieces, for anyone curious.

## 3. Running

### 3.1 In a doze stack

```hcl
recall {}
```

One per stack. It takes no keys at first. doze runs it the way it runs the
aws engine:

- **Fetched like a module.** doze fetches `doze-recall` from the signed
  registry and pins it, the same way it fetches doze-aws and doze-kafka.
- **An instance of the stack.** It's served at `recall.<stack>.doze`, or a
  port in non-domains mode, and appears in `doze status`, `doze logs recall`
  and the dash, like every other instance.
- **Wakes on connect.** The proxy starts it when an agent connects. Waking is
  quick: the binary is small, and the cache is on disk.
- **Sleeps when idle.** It stays awake while agents are using it, and sleeps
  after the stack's `idle_timeout`. Asleep, it costs nothing.
- **Told about the stack by doze.** doze rewrites `.doze/recall/stack.json`
  on every config load and passes recall its path and the project root.

That is how it's continuous: it's awake for as long as any agent is
connected, watching files and keeping its cache warm (§1.3), and it sleeps
when nothing needs it. It's one server for every agent session on the stack,
so two sessions see the same notes as soon as either writes one.

MCP is served over HTTP, the transport MCP uses for a server that's already
running. Two ways to connect, both written by `doze recall connect`:

```
claude mcp add --transport http recall http://recall.shop.doze/mcp
claude mcp add recall -- doze recall mcp
```

The first is native HTTP. The second is a small stdio bridge for clients
without HTTP support or a machine without `.doze` names: it finds the stack
from the working folder and forwards to the instance through doze. Like any
stack instance, recall needs the daemon (`doze up`). If the daemon isn't
running, the bridge says so in one line.

`recall {}` moves from `futureBlocks` to `reservedBlocks` (`4b1baf9`
reserved it). Without the block, doze writes no `stack.json` and runs no
recall.

### 3.2 On its own

```
cd my-project
claude mcp add recall -- doze-recall
```

For one project, with no doze and no stack. The agent starts it over stdio
for the length of its session. It serves trees, `find` and knowledge for the
folder it runs in, with no stack map and no `stack` tool. It watches files
while the session is open, the cache lives in `~/.doze-recall/`, and it
installs with one script, like doze-aws.

Knowledge is the same `.recall/` folder in both modes. A project that starts
alone and later joins a doze stack keeps every note.

### 3.3 Tools

Four tools (`stack` only in a stack):

| Tool | Returns |
|---|---|
| `stack` | The stack map, plus the flow pieces |
| `app <name> [path]` | The app's tree (or one folder or file in full), how to run and test it, what it's wired to, its knowledge piece and the notes that mention it |
| `find <term>` | Where a symbol, env key, topic, queue, bucket or route name appears, with file and line, joined to the stack map |
| `remember` | Saves or updates a piece of knowledge (§2.2) |

Every answer has a default size cap (a few thousand tokens) and says what was
cut and how to ask for it. Every knowledge item in an answer carries its
staleness: fresh, or the list of cited files changed since.

doze's `init` adds one line to AGENTS.md or CLAUDE.md saying that recall is
there and to ask it first. The server's instructions tell the agent to call `stack` at the start, `app`
before exploring an app, and `remember` when it has learned something a
later session would want. Whether agents follow that reliably is the
biggest unknown; S0 checks it early and the eval measures it.

## 4. What recall is not

No language servers, call graph or impact analysis. No links between apps
guessed from names: the wiring comes from `doze.hcl`, and the rest is
knowledge that cites its files. No process running when no agent is
connected. No review workflow. On its own, no stack map built by guessing
which apps talk to which: that needs a `doze.hcl`.

If agents keep missing something these pieces don't cover, that's the signal
to add more, starting with the cheapest thing that fixes it.

## 5. Eval

The question: with recall, do agents finish the same tasks with fewer tokens
and tool calls, and do they keep doing so session after session?

- **Stacks:** two real open-source multi-service projects with a `doze.hcl`
  written for them. Jitsu and Hatchet were measured in v1's research.
- **Tasks:** about 10 per stack, using the task format and checks already in
  `doze-recall-eval/checks`.
- **Three arms:**
  - **A0:** no recall.
  - **A1:** recall cold, with facts only.
  - **A2:** recall warm, after earlier sessions on *other* tasks have
    written knowledge.

  A2 against A1 measures "doesn't repeat itself" directly.
- **Measures:** tokens, tool calls, wall time, task success by the task's
  tests, and how often agents called `remember` when asked to.
- **Size:** 10 tasks × 3 arms × 3 repeats × 2 stacks is 180 runs, on a
  subscription.

`parcel` isn't needed and is parked.

## 6. Order of work

Estimates are working days for one person.

| Step | What | Days |
|---|---|---|
| **S0** spike | gotreesitter on Go, TypeScript, Python and Rust: binary growth, parse time on a real app, quality of `tags.scm` output. A stub MCP server with `remember`: does Claude Code save a note when an answer asks it to? | 3–5 |
| **S1** stack map | Env provenance in config; `Declared` in doze-sdk, doze-aws and doze-kafka; writing `stack.json` | 6–9 |
| **S2** recall in the stack | `doze-recall` repo: the MCP server over HTTP and stdio with `stack`; the engine plugin; `recall {}` in doze, the instance and its host, `doze recall mcp` and `connect`; registry publishing | 8–12 |
| **S3** Go trees | File listing and cache, the watcher while awake, roles, symbols, generated files, env-key join, ranking and budget; `app` | 8–12 |
| **S4** find | Symbols, env keys, declared resource names | 4–6 |
| **S5** knowledge | The `.recall/` format, `remember` and its checks, staleness, hand-edit detection, the nudges in answers, the AGENTS.md line | 6–10 |
| **S6** on its own | The standalone mode (§3.2), install script, release | 2–4 |
| **S7** more languages | TypeScript and TSX, Python, Rust trees | 6–10 |
| **S8** eval | Two stacks' `doze.hcl`, 20 tasks, 180 runs, the write-up | 8–12 |
| **S9** optional | `doze recall enrich`, a dash view | 4–8 |

S0–S8 total 51–80 days, roughly 10–16 weeks. S1 and S2 are useful to ship
first: a stack map over MCP needs no parsing.

## 7. Open

- **Where knowledge lives.** Committed in `.recall/` (proposed: one person's
  sessions pay for everyone's), or kept per machine for teams that don't want
  it in the repo. A setting could allow both.
- **Live state in `stack`.** In a stack, recall could also say which
  instances are up, asleep or crashing, from doze's control API. Useful for
  "why is this failing", but it couples recall to a doze API. Later, if
  agents ask for it.
- **Keys on `recall {}`.** None at first. Candidates if needed: which
  folders to leave out, and where knowledge lives.
- **Agents writing knowledge reliably.** If S0 shows they don't call
  `remember` when asked, `enrich` becomes the main path rather than an
  extra.

## Sources for §1.4

- gotreesitter: https://github.com/odvcencio/gotreesitter, BENCH.md
- m-parse (wazero spike): https://pkg.go.dev/github.com/vista-forge/m-parse
- CodeGraph and Graphify, the closest prior art for app trees:
  https://github.com/colbymchenry/codegraph,
  https://github.com/Graphify-Labs/graphify
