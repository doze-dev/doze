# doze recall

Status: first draft of the plan, 2026-10-24. Nothing here is implemented
yet, apart from reserving the `recall` block in doze (`4b1baf9`).

## What recall is

An agent should never pay twice to learn the same thing about a project.

Every agent session starts cold today. It lists folders, greps, and reads the
same large files again to work out what each app does, how the apps connect,
how to run things and where code lives. The next session does it all again.
Recall keeps what has been learned and hands it to the next session cheaply,
over MCP.

Recall is its own binary, `doze-recall`, like doze-aws and doze-kafka, with a
different purpose. It runs in two ways:

- **In a doze stack,** declared with `recall {}`. doze runs it as part of the
  stack, and it knows the whole stack because doze tells it (§3.1).
- **On its own,** for one project with no doze: trees and knowledge for that
  project, with no stack map (§3.2).

It gives agents two kinds of context:

| | What | Written by | How it stays right |
|---|---|---|---|
| **Facts** (§1) | The stack map; a tree of each app | Generated, never by AI | Checked against the files on every call |
| **Knowledge** (§2) | What apps do, their routes and consumers, flows across apps, conventions, gotchas | Agents, as they work; people, when they want | Each piece cites its files, and says which have changed since |

## 1. Facts

### 1.1 The stack map

In a stack only. doze writes `.doze/recall/stack.json` on every config load,
and recall reads it, so recall never parses HCL and the two can't disagree
about the stack. It holds:

- **Apps:** name, folder, language, port, run and test commands, and how to
  see the logs (`doze logs <app>`).
- **Wiring:** each env key and where its value comes from, for example
  `orders: DATABASE_URL ← postgres.orders.url` and
  `SHIPMENTS_TOPIC ← kafka.events.topic.shipments`.
- **Resources:** each database, queue, topic and bucket, and the apps wired
  to it.
- **Order:** dependencies and boot order.

Two things doze needs for this:

- **Env provenance.** doze loses which attribute fed which env key once the
  config is evaluated. It keeps that map by walking each env expression's
  references.
- **Resources inside modules.** The queues, topics and buckets declared inside
  the aws and kafka modules are opaque to doze core. A read-only `Declared`
  capability in doze-sdk, implemented by doze-aws and doze-kafka, returns
  them. It isn't part of plan or sync, so it can't change what those modules
  converge.

### 1.2 App trees

For each app (or, on its own, for the project), a compact tree an agent can
use instead of `ls`, `grep` and opening files to find its way around:

- **Folders,** collapsed to a few levels with file counts.
- **Files that matter,** each marked by role:
  - entrypoints: `package main`, `package.json` `main` and scripts,
    `__main__`, a Rust `main.rs` or `[[bin]]`;
  - tests, migrations and config, by name and folder conventions;
  - generated files, by their banner (`Code generated … DO NOT EDIT`,
    `@generated`), shown collapsed.
- **Symbols per file:** the functions, types and methods each file defines,
  as signatures.
- **Env keys each file reads,** joined to the stack map in a stack:
  `SHIPMENTS_TOPIC` is read in `internal/events/publish.go`, and it's the
  `shipments` topic, which dispatch is also wired to.
- **Ranked to fit a budget.** A large app doesn't fit in a prompt. Files near
  entrypoints and the most-referenced symbols come first, and the agent can
  ask for one folder or file in full.

The tree holds only what's the same in every codebase. Routes, handlers,
consumers and producers are left to agents (§2): there are too many
frameworks and clients to keep rules for, and an agent recognizes them at a
glance once it's reading the file.

### 1.3 Staying current

Every call checks the files on disk:

1. List files with `git ls-files`, plus untracked files that aren't ignored.
2. Compare each file's size, modification time and content hash with the
   cache.
3. Re-parse only what changed.
4. Answer from the cache.

So answers always match the files as they are at that moment, with no file
watcher. The first call on a large app takes seconds; later calls take
milliseconds. In a stack, recall also rebuilds the cache when it wakes,
before the first question, and re-reads `stack.json` when doze rewrites it.

The cache is machine state: doze's data folder for the instance in a stack,
`~/.doze-recall/` on its own.

### 1.4 Parsing

Tree-sitter, through the official Go binding
(`github.com/tree-sitter/go-tree-sitter`), with the official grammars for Go,
TypeScript and TSX, JavaScript, Python and Rust. Each grammar ships a
`tags.scm` query that names its definitions and references, which is what
the symbols and the ranking need.

The binding uses cgo, so `doze-recall` is the one doze binary that isn't pure
Go. It's released from a matrix of native runners (macOS arm64 and amd64,
Linux arm64 and amd64, Windows amd64) instead of one cross-compiling Linux
runner. doze-aws's module workflow already runs a matrix of operating
systems.

## 2. Knowledge

### 2.1 What it is

Small markdown files in `.recall/`, in the project root (beside `doze.hcl` in
a stack), committed with the code, one file per subject:

```
.recall/
  apps/billing.md          what billing does, its routes and consumers, its gotchas
  apps/orders.md
  flows/checkout.md        a flow across apps
  notes/money-in-cents.md  a convention or gotcha that spans apps
```

Each file has a short header listing the files it was written from, with
their content hashes at the time:

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

### 2.2 How it gets written

Agents write it while they work, without a separate run or bill:

1. An agent asks recall about `billing`.
2. Recall returns the facts, and the knowledge piece if there is one.
3. If there's no piece, or files it cites have changed since it was written,
   the answer says so:
   - "No notes on billing yet. You'll be reading its code anyway: when you've
     understood it, save what you learned with `remember`."
   - "Written against an older `routes.go` and `invoices.go`. Check those two
     and update this if it's wrong."
4. The agent has to read that code for its own task anyway, so writing it
   down costs a few hundred tokens. The next session gets it almost for
   free.

The cost is paid once per change, inside sessions people are already
running. When a file changes, only the pieces citing it are affected, and
the answer names exactly which files to re-check.

`remember` checks what it's given:

- every cited file exists; recall records the current hashes itself;
- the piece stays under a size cap;
- it contains nothing that looks like a secret;
- it replaces its subject's file rather than appending, so files don't grow
  without bound.

### 2.3 People in the loop

Recall never blocks, never asks and never notifies. People step in only in
ways they already work:

- **Say it in chat.** "Remember that billing must never call dispatch
  directly." The agent calls `remember`. This is expected to be the main way
  people correct things.
- **Edit the file.** The pieces are plain markdown in the repo.
- **See it in review.** A knowledge change lands in the same pull request as
  the code change behind it. Files are small, one subject each, with stable
  ordering and no timestamps, so diffs are short and easy to skip.

What a person writes wins. Recall remembers the hash of what it last wrote to
each file. If a file's contents differ from that, a person edited it, so:

- agents still read it, and it's still flagged when its cited files change;
- `remember` won't overwrite it. It answers "billing.md was edited by a
  person: tell them what you'd change instead."

## 3. Running

### 3.1 In a doze stack

```hcl
recall {}
```

One per stack. Its only key is `port`, needed when the stack doesn't use
`.doze` names, the same rule every doze instance follows (doze never picks a
port itself). doze runs it the way it runs the aws engine:

- **Fetched like a module.** doze fetches `doze-recall` from the signed
  registry and pins it, like doze-aws and doze-kafka.
- **An instance of the stack,** at `recall.<stack>.doze` or its port. It
  appears in `doze status` and `doze logs recall` like any other.
- **Wakes when an agent connects,** through doze's proxy. Waking is quick:
  the binary is small and the cache is on disk.
- **Sleeps after the stack's `idle_timeout`** once no agent is connected, and
  costs nothing asleep.
- **Told about the stack by doze,** through `stack.json` and the project root
  doze passes it.

That's how it's continuous: it's awake while any agent is connected, and one
server serves every session on the stack, so a note one agent saves is there
for the next question from any other.

The connect line:

```
claude mcp add recall -- doze recall mcp
```

`doze recall mcp` is a small stdio bridge. It finds the stack from the
working folder, connects to recall's address through doze, so waking and
idle work as for every instance, and relays MCP. The agent's config never
holds a host or port, so it keeps working if they change. Like any stack
instance, recall needs the daemon. If the daemon isn't running, the bridge
says so and suggests `doze up`.

`recall {}` moves from `futureBlocks` to `reservedBlocks` in
`internal/config/load.go`. Without the block, doze writes no `stack.json`
and runs no recall.

### 3.2 On its own

```
cd my-project
claude mcp add recall -- doze-recall
```

The agent starts it over stdio for the length of the session. It serves the
project's tree and knowledge, with no stack map. It installs with one
script, like doze-aws.

Knowledge is the same `.recall/` folder in both modes, so a project that
starts on its own keeps every note when it joins a doze stack.

## 4. The tools

| Tool | Returns |
|---|---|
| `stack` | In a stack: the stack map, and the flow pieces |
| `app <name> [path]` | The app's tree, or one folder or file in full; how to run, test and see its logs; what it's wired to; its knowledge piece, and the notes that mention it. On its own, `app` takes no name and covers the project |
| `remember <subject> <text> <sources>` | Saves or updates a piece of knowledge (§2.2) |

Every answer has a size cap of a few thousand tokens and says what was cut
and how to ask for it. Every knowledge item in an answer is marked fresh, or
lists the cited files that changed since.

The MCP server's instructions, which clients pass to the agent on connect,
say: call `stack` first, call `app` before exploring an app, and call
`remember` when you've learned something a later session would want. So no
agent file (AGENTS.md, CLAUDE.md) needs editing.

## 5. Eval

The question: with recall, do agents finish the same tasks with fewer tokens
and tool calls, and keep doing so session after session?

- **Stacks:** two real open-source multi-service projects, Jitsu and
  Hatchet, each with a `doze.hcl` written for it.
- **Tasks:** about 10 per stack, written by agents, in the task format and
  with the checks in `doze-recall-eval/checks`. Each task is scored by its
  tests.
- **Three arms:**
  - **A0:** no recall.
  - **A1:** recall with no knowledge yet.
  - **A2:** recall after sessions on *other* tasks have written knowledge.

  A1 against A0 measures the facts. A2 against A1 measures "doesn't repeat
  itself".
- **Measures:** tokens, tool calls, wall time, task success, and how often
  agents called `remember` when asked to.
- **Size:** 10 tasks × 3 arms × 3 repeats × 2 stacks is 180 runs, on a
  subscription, with the model pinned.

## 6. Steps

Working days for one person.

| Step | What | Days |
|---|---|---|
| **S0** spike | Tree-sitter's `tags.scm` output for the five languages on a real app: is it good enough for the tree, and how fast? A cgo release build on the runner matrix. A stub MCP server with `remember`: does Claude Code save a note when an answer asks it to? | 3–5 |
| **S1** stack map in doze | Env provenance; `Declared` in doze-sdk, doze-aws and doze-kafka; writing `stack.json` | 6–9 |
| **S2** recall in the stack | The `doze-recall` MCP server with `stack`; its engine plugin; `recall {}` and its `port` in doze; `doze recall mcp`; release and registry publishing | 8–12 |
| **S3** Go trees | File listing and cache, roles, symbols, generated files, the env-key join, ranking and budget; `app` | 7–10 |
| **S4** knowledge | The `.recall/` format, `remember` and its checks, staleness, hand-edit detection, the prompts in answers | 6–10 |
| **S5** on its own | The stdio mode, the install script | 2–4 |
| **S6** more languages | Trees for TypeScript and TSX, JavaScript, Python and Rust | 6–10 |
| **S7** eval | `doze.hcl` for both stacks, 20 tasks, 180 runs, the write-up | 8–12 |

Total 46–72 days, about 9–15 weeks. S1 and S2 can ship first: the stack map
over MCP needs no parsing.

If S0 shows agents don't call `remember` when asked, the design changes
before S4, because knowledge would then need its own way to be written.

## 7. Decisions

| Decision | Why |
|---|---|
| Knowledge is committed in `.recall/` | One person's sessions pay for everyone's, and it's reviewed with the code. A team that doesn't want it can ignore the folder in git |
| Routes, handlers and consumers are knowledge, not generated | Too many frameworks to keep rules for; agents recognize them at a glance |
| No watcher | Checking the cache on each call takes milliseconds and can't miss a change |
| The stack map comes only from `doze.hcl` | Never guessed. On its own, recall has no stack map |
| One connect line, the stdio bridge | No host or port in the agent's config, no `.doze` names required, and the bridge can say when the daemon is down |
| `recall {}` takes only `port` | Exclusions come from `.gitignore`, and knowledge always lives in `.recall/` |
| `stack` doesn't report live state (up, asleep, crashing) | It would tie recall to doze's control API. The map tells agents to use `doze status` and `doze logs` |
| Official tree-sitter binding, with cgo | The reference implementation, fastest, and used by most of the ecosystem. The cost is a release matrix for one binary |
| No AGENTS.md edits | MCP server instructions reach the agent on connect |
