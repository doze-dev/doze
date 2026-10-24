# doze recall

Status: first draft of the plan, 2026-10-24, revised after an independent
review against the code. Nothing here is implemented yet, apart from
reserving the `recall` block in doze (`4b1baf9`).

## What recall is

An agent should never pay twice to learn the same thing about a project.

Every agent session starts cold today. It lists folders, greps, and reads the
same large files again to work out what each app does, how the apps connect,
how to run things and where code lives. The next session does it all again.
Recall keeps what has been learned and hands it to the next session cheaply.

Recall is its own binary, `doze-recall`, like doze-aws and doze-kafka, with a
different purpose. It is used in two ways:

- **In a doze stack,** declared with `recall {}`. doze runs it as an instance
  of the stack, an MCP server that knows the whole stack because doze tells
  it (§3.1).
- **On its own,** for one project with no doze: a command-line tool that
  prints the project's tree and knowledge. There's no MCP server and no
  stack map (§3.2).

It gives agents two kinds of context:

| | What | Written by | How it stays right |
|---|---|---|---|
| **Facts** (§1) | The stack map; the tree of each app | Generated, never by AI | Checked against the files on every call |
| **Knowledge** (§2) | What apps do, their routes and consumers, flows across apps, conventions, gotchas | Agents, as they work; people, when they want | Each piece cites its files, and says which have changed since |

## 1. Facts

### 1.1 The stack map

In a stack only. doze writes `.doze/recall/stack.json` on every config load,
and recall reads it, so recall never parses HCL and the two can't disagree
about the stack. It holds:

- **Apps:** name, folder, entrypoint (the path in `run`), port, how to see
  the logs (`doze logs <app>`), and the tasks it depends on.
- **Wiring:** each env key and where its value comes from:
  - a reference, for example `DATABASE_URL ← module.db.url` or
    `ORDERS_QUEUE ← module.cloud.queue.orders.url`;
  - a literal, recorded as the literal, never matched to a resource by name;
  - an `env_file`, recorded as "from `<file>`".

  References through `local.x` and inside strings
  (`"http://${app.api.address}"`) are recorded too.
- **Resources:** each database, queue, topic and bucket, and the apps whose
  env refers to it.
- **Order:** dependencies and boot order.

What doze needs for this:

- **Env provenance.** Once the config is evaluated, doze no longer knows
  which reference fed which env key. The walk to find them already exists
  (`referencedTraversals`, `internal/config/eval.go`); keep its result per
  key.
- **References for resources inside modules.** Today an app can only name a
  queue or topic by string (`ORDERS_QUEUE = "orders"` in
  `examples/shop/doze.hcl`), so nothing links it to the declared queue.
  doze-aws and doze-kafka implement the existing `Attributer` interface to
  publish:
  - `module.<name>.queue.<q>.url` and `.name`;
  - `module.<name>.topic.<t>.name`;
  - `module.<name>.bucket.<b>.name`.

  Then apps can refer to them, and provenance captures the wiring. This is
  also better for doze users on its own: a renamed queue is caught at load
  time. The shop example moves to these references. `Attributer` only feeds
  references, so it can't change what plan, apply or prune do.

### 1.2 App trees

A compact tree an agent can use instead of `ls`, `grep` and opening files to
find its way around. An app's tree covers its `dir`. Apps with no `dir` share
the project tree, with each app's entrypoint marked (the shop's three apps
all run from the root). On its own, the tree covers the project.

- **Folders,** collapsed to a few levels with file counts.
- **Files that matter,** each marked by role:
  - entrypoints: from `run` in a stack; otherwise `package main`,
    `package.json` `main`, `__main__.py`, a Rust `main.rs` or `[[bin]]`;
  - tests, migrations and config, by name and folder conventions;
  - generated files, by their banner (`Code generated … DO NOT EDIT`,
    `@generated`), shown collapsed.
- **Symbols per file:** the functions, types and methods each file defines,
  as one-line signatures.
- **Env keys per file,** in a stack: which files mention each env key that
  `doze.hcl` sets for the app, found by plain text match on the key's name,
  and joined to the wiring: `ORDERS_QUEUE` appears in
  `internal/jobs/worker.go`, and it's the `orders` queue in `module.cloud`.
  A text match finds `os.Getenv("X")`, `process.env.X` and config-library
  tags alike, with no per-language or per-library rules.
- **Ordered to fit a budget:** entrypoints first, then exported symbols,
  then the rest by folder depth. What doesn't fit is listed by folder, and
  the agent can ask for one folder, or one file's outline (its role,
  symbols and env keys), never its full contents.

The tree holds only what's the same in every codebase. Routes, handlers,
consumers and producers are left to agents (§2): there are too many
frameworks and clients to keep rules for, and an agent recognizes them at a
glance once it's reading the file.

### 1.3 Staying current

Every call checks the files on disk:

1. List files with `git ls-files` plus untracked files that aren't ignored,
   or, outside git, walk the folder honouring `.gitignore`.
2. Compare each file's size and modification time with the cache, and hash
   only the files where either changed.
3. Re-parse only files whose hash changed.
4. Answer from the cache.

So answers match the files as they are at that moment, with no file
watcher. The first call on a large app takes seconds; later calls take
milliseconds. In a stack, recall re-reads `stack.json` when doze rewrites it.

The cache is machine state: the instance's data folder in a stack, and
`~/.doze-recall/<hash of the project's path>/` on its own.

### 1.4 Parsing

Tree-sitter, through the official Go binding
(`github.com/tree-sitter/go-tree-sitter`), with the official grammars for Go,
TypeScript and TSX, JavaScript, Python and Rust. Each grammar has a
`tags.scm` query marking its definitions. The Go grammar modules don't
export these files, so recall vendors each one, pinned to its grammar's
version. TypeScript's and TSX's build on JavaScript's. A signature is the
definition node's text up to its body.

The binding uses cgo, so `doze-recall` is the one doze binary that isn't
pure Go. It's built natively for each platform doze supports (macOS arm64,
Linux arm64 and amd64), on one runner per platform, where every other
module is cross-compiled from one Linux runner.

## 2. Knowledge

### 2.1 What it is

Small markdown files in `.recall/`, in the project root (beside `doze.hcl` in
a stack), committed with the code. There's one file per subject, and the
subject's type is its folder:

```
.recall/
  apps/billing.md          app:billing     what it does, its routes and consumers, its gotchas
  flows/checkout.md        flow:checkout   a flow across apps
  notes/money-in-cents.md  note:money-in-cents   a convention or gotcha
```

Each file has a short header. `sources` lists the files it was written from,
with their content hashes at the time. `written` is the hash of the body
recall wrote:

```markdown
---
about: app:billing
sources:
  apps/billing/cmd/billing/main.go: 3f9a…
  apps/billing/internal/http/routes.go: 81c2…
  apps/billing/internal/queue/invoices.go: 0d4e…
written: 7c1d…
---

Billing turns paid orders into invoices and sends them.

Routes: POST /invoices (routes.go:22), GET /invoices/{id} (routes.go:31).
Consumes `orders.paid` from Kafka (queue/invoices.go:14) …
```

### 2.2 How it gets written

Agents write it while they work, without a separate run or extra cost:

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

`remember` takes a subject (`app:billing`, `flow:checkout`,
`note:money-in-cents`), the text, and the files it cites, and checks them:

- every cited file exists; recall records the current hashes itself;
- the text stays under a size cap;
- it matches none of a fixed set of secret patterns (private keys, common
  token formats);
- it replaces the subject's file rather than appending, so files don't grow
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

What a person writes wins. If a file's body no longer matches its `written`
hash, a person edited it. That check needs nothing but the file, so it
works on every clone. Then:

- agents still read it, and it's still flagged when its cited files change;
- `remember` won't overwrite it. It answers "billing.md was edited by a
  person: tell them what you'd change instead."

## 3. Running

`doze-recall` is one binary. doze runs it through the plugin protocol, and a
person runs it as a command-line tool. Both use the same code for trees and
knowledge.

### 3.1 In a doze stack

```hcl
recall {}
```

One per stack. doze turns it into an instance of type `recall` named
`recall`. That name is then taken: an app, service or module called `recall`
is refused. Like modules, it has a conventional port that's used when none
is given. `port` is its only key, for when that port clashes in a stack
without `.doze` names.

- **Fetched like a module.** doze fetches `doze-recall` from the signed
  registry and pins it in `doze.lock`, like doze-aws and doze-kafka.
- **An instance of the stack.** It appears in `doze status` and
  `doze logs recall` like any other.
- **Wakes when an agent connects,** through doze's proxy listener for
  recall.
- **Stays awake while any agent session is connected,** and sleeps after the
  stack's `idle_timeout` once the last one disconnects.
- **Told about the stack by doze.** A new `ProjectDir` field on
  `engine.Instance` in doze-sdk tells it where the project is, so it can find
  `doze.hcl`, `.doze/recall/stack.json` and `.recall/`.

One recall instance serves every agent session on the stack, so a note one
agent saves is in the next answer to any other.

The connect line:

```
claude mcp add recall -- doze recall mcp
```

`doze recall mcp` is a small stdio bridge:

1. It finds the stack from the working folder.
2. It starts the daemon if it isn't running, the way other doze commands do.
3. It opens one TCP connection to recall's proxy listener for the length of
   the agent session, which wakes recall.
4. It relays MCP messages both ways.

Recall serves one MCP session per connection, using the same
newline-delimited JSON-RPC that MCP's stdio transport uses, so there's no
HTTP and no session IDs to lose. The agent's config holds no host or port.

`recall {}` moves from `futureBlocks` to `reservedBlocks` in
`internal/config/load.go`. Without the block, doze writes no `stack.json`
and runs no recall.

### 3.2 On its own

A command-line tool, with no MCP server:

```
doze-recall tree [path]                            the project's tree (§1.2), or one folder or file's outline
doze-recall notes [subject]                        the knowledge in .recall/, each piece marked fresh or stale
doze-recall remember <subject> --sources <files>   save a piece, text on stdin
```

Output is markdown, capped like the MCP answers (§4), so a person can paste
it into a prompt and an agent can run it as a shell command. Writes to
`.recall/` take a file lock, since several sessions may run it at once. It
installs with one script, like doze-aws.

Knowledge is the same `.recall/` folder in both modes, so a project that
starts on its own keeps every note when it joins a doze stack.

## 4. The MCP tools

In a stack only.

| Tool | Returns |
|---|---|
| `stack` | The stack map, plus the list of flow and note pieces |
| `app <name> [path]` | The app's tree, or one folder or file's outline. Also how to run it and see its logs, what it's wired to, its knowledge piece, and the pieces that cite files in its folder |
| `remember <subject> <text> <sources>` | Saves or updates a piece of knowledge (§2.2) |

Every answer has a size cap of a few thousand tokens and says what was cut
and how to ask for it. Every knowledge item in an answer is marked fresh, or
lists the cited files that changed since.

The MCP server's instructions, which clients pass to the agent on connect,
say: call `stack` first, call `app` before exploring an app, and call
`remember` when you've learned something a later session would want. No
agent file (AGENTS.md, CLAUDE.md) needs editing.

## 5. Eval

The question: with recall, do agents finish the same tasks with fewer tokens
and tool calls, and keep doing so session after session?

- **Stacks:** two open-source multi-service projects that doze can run with
  its existing modules, each with a `doze.hcl` written for it. Hatchet,
  which can run on Postgres alone, is the first candidate; the second is
  chosen in S7. Both are public, so models may have seen them in training.
  The comparison is between arms on the same tasks, so that affects every
  arm alike.
- **Tasks:** 10 scored tasks per stack, written by agents, in the
  `doze-recall-eval/checks` format: a base commit, a reference patch, and
  hidden tests that the reference passes 5 times out of 5. The harness
  brings the stack up with doze before running the tests.
- **Three arms,** all with the stack running in doze:
  - **A0:** no recall.
  - **A1:** recall with no knowledge yet.
  - **A2:** recall with the `.recall/` written by sessions on 5 separate,
    unscored warm-up tasks per stack.

  A1 against A0 measures what the facts alone save. A2 against A1 measures
  "doesn't repeat itself".
- **Measures,** counted from the session transcripts: tokens, tool calls,
  wall time, task success, and how often agents called `remember` when
  asked to.
- **Size:** 10 tasks × 3 arms × 3 repeats × 2 stacks is 180 scored runs,
  plus 10 warm-up sessions, on the Claude Code subscription with the model
  pinned.

## 6. Steps

Working days for one person.

| Step | What | Days |
|---|---|---|
| **S0** spike | Vendored `tags.scm` on a real app in each of the five languages: are the definitions and signatures good enough, and how fast? Binary size with the five grammars. A native cgo build for each platform. A stub MCP server with `remember`: does Claude Code save a note when an answer asks it to? | 4–6 |
| **S1** stack map in doze | Env provenance; `Attributer` references for queues, topics and buckets in doze-aws and doze-kafka; the shop example; writing `stack.json` | 7–11 |
| **S2** recall in the stack | `doze-recall` with the plugin protocol and MCP over one connection per session, `stack`, the server instructions, answer caps. In doze: `recall {}` as an instance, its conventional port, the name rule, `doze.lock`, `doze recall mcp`. In doze-sdk: `ProjectDir`, and modtool building cgo natively per platform and merging the registry index | 10–15 |
| **S3** Go trees | File listing and cache, roles, symbols, generated files, env-key mentions, ordering and budget; `app` | 7–10 |
| **S4** knowledge | The `.recall/` format, `remember` and its checks, staleness, hand-edit detection, the prompts in answers | 6–10 |
| **S5** on its own | `tree`, `notes` and `remember` as commands, the write lock, the install script | 2–4 |
| **S6** more languages | Trees for TypeScript and TSX, JavaScript, Python and Rust | 6–10 |
| **S7** eval | Choosing the second stack, both `doze.hcl` files, 20 scored and 10 warm-up tasks with reference patches and hidden tests, the runs, the write-up | 12–18 |

Total 54–84 days, about 11–17 weeks. S1 and S2 can ship first: the stack map
over MCP needs no parsing.

If S0 shows agents don't call `remember` when asked, the design changes
before S4, because knowledge would then need its own way to be written.

## 7. Decisions

| Decision | Why |
|---|---|
| Knowledge is committed in `.recall/` | One person's sessions pay for everyone's, and it's reviewed with the code. A team that doesn't want it can ignore the folder in git |
| Routes, handlers and consumers are knowledge, not generated | Too many frameworks to keep rules for; agents recognize them at a glance |
| The stack map comes only from `doze.hcl` | Never guessed: a literal env value is shown as a literal. On its own, recall has no stack map |
| aws and kafka publish references for their resources | It's how the map can link a queue to its apps without guessing, and it lets doze catch a wrong queue name at load time |
| Env keys found by text match on the names doze.hcl sets | Works the same in every language and config library |
| No file watcher | Checking sizes and times on each call takes milliseconds and can't miss a change |
| MCP only in a stack; on its own, a command-line tool | A server needs something to run it and wake it, which is what doze provides. Alone, printing on demand is enough |
| One connect line, the stdio bridge, one connection per session | No host or port in the agent's config, no `.doze` names needed, and recall can't sleep in the middle of a session |
| `recall {}` takes only `port` | Exclusions come from `.gitignore`, and knowledge always lives in `.recall/` |
| `stack` doesn't report live state (up, asleep, crashing) | It would tie recall to doze's control API. The map tells agents to use `doze status` and `doze logs` |
| Official tree-sitter binding, with cgo | The reference implementation, fastest, and used by most of the ecosystem. The cost is a native build per platform for one binary |
| No AGENTS.md edits | MCP server instructions reach the agent on connect |
