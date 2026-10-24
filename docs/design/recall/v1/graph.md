# Contract: the graph

Status: design, 2026-10-20; revised the same day after an independent review
against the code, and when v1 grew to five languages (R76). Decisions C1–C9
in [context-system.md](context-system.md) §10 are taken as recommended.
Nothing here is implemented.

The graph is everything recall knows about a stack, as facts with evidence.
It holds what the code and a declared-stack file prove, and the points where people's
notes and agents' proposals attach. Answers are built from it and nothing else
([answers.md](answers.md)).

**v1 covers Go, TypeScript, JavaScript, Python and Rust.** Go is exact
in-process through `go/types` (§4.2). The others are language packs, made
precise by pinned language servers (§4.6–§4.7). Sections 1–9 are the v1
contract. Section 10 describes what comes after v1 (observed calls, more
languages), so v1's shapes leave room for it.

Examples in §1–§4.2 use Go, because its rules are the most exact; every pack
fills the same shapes.

## 1. Ids

Ids must survive what happens to real code: an app being renamed, the same
repo used in two stacks, several apps sharing one Go module (the shop example's
three apps have no `dir` and share one module), and edits inside a file.
So **code ids are keyed by the code's own name, never by the app.** Stack ids
are keyed by the stack file's names, or by inferred names when there is no
file, because that is what they are.

```
id        = kind ":" body
go symbol = "go:" importpath "." name                  go:github.com/doze-dev/doze/examples/shop/internal/order.Create
          | "go:" importpath ".(*" type ")." method    go:github.com/doze-dev/doze/examples/shop/internal/order.(*Service).Create
          | "go:" importpath "." type "." method       go:example.com/x/list.List.Len
package   = "pkg:go:" importpath
file      = "file:" moduleid "/" relpath               file:go:github.com/doze-dev/doze/examples/shop/cmd/worker/main.go
app       = "app." name                                app.worker
module    = "module." name                             module.cloud
resource  = "module." name ":" kind "." rname          module.cloud:queue.orders
inferred  = "inferred:" family ":" kind "." rname    inferred:kafka:topic.order.events   (no stack file)
column    = resource "." column                        module.db:table.orders.total
env var   = "app." name ":env." key                    app.worker:env.ORDERS_QUEUE
route     = "app." name ":route " quoted               app.api:route "POST /orders"
external  = "external:" vendor                         external:stripe
dependency= "dep:go:" modulepath "@" version           dep:go:github.com/twmb/franz-go@v1.18.0
dep symbol= dependency ":" name                        dep:go:github.com/twmb/franz-go@v1.18.0:kgo.(*Client).ProduceSync
```

Rules:

- **Generics** use the origin object: `(*List[T]).Push` and every
  instantiation map to `go:…/list.(*List).Push`, with no type parameters in
  the id.
- **`init` functions** add the file and an ordinal: `go:…/order.init#create.go:1`.
- **Build-tagged duplicates** of one name (a `_linux.go` and a `_darwin.go`
  `func open`) are one node with several definition sites.
- **Test packages** keep their own import path (`…/order_test`).
- **Quoting:** route paths and other free text are double-quoted, with `\"`
  and `\\` escaped. Paths in ids always use `/`, on every platform.
- **Stability:** an id survives edits inside its file and moves of its
  declaration within the package. It does not survive a rename or a move to
  another package. Notes anchored to a vanished id become orphans and are
  offered a re-anchor by name and signature (context.md, planned).

**Which app owns code** is a fact, not part of the id:

- An app uses a package if the package is reachable through imports from
  the app's entry point.
- The entry point comes from the run command (`go run ./cmd/api` gives
  `./cmd/api`).
- When the run command names no Go package, it is every main package under
  the app's `dir`.
- A shared package used by api and worker is one node with two `uses` edges.
  `impact` reports its callers by app.

A Go module is indexed once per machine for each content state and reused by
every stack that contains it (§7).

## 2. Nodes and edges

Node kinds: app, module, resource, column, env var, route, external, package,
file, symbol, dependency, dependency symbol (ids in §1).

| Edge | From → to | From |
|---|---|---|
| `contains` | package → file → symbol | code |
| `uses` | app → package | entry-point reachability |
| `imports` | package → package or dependency | code |
| `references` | symbol → symbol | `types.Info.Uses` that are not calls |
| `calls` | symbol → symbol | call expressions; calls through an interface go to the interface method |
| `implements` | type → interface | method sets, for every interface declared in the workspace |
| `reads_env` | symbol → env var | `os.Getenv`, `os.LookupEnv`, config tags |
| `wires` | env var → module attribute or resource | the stack file, or `.env` files |
| `sends`, `receives`, `reads`, `writes` | symbol (or a client) → resource | the join |
| `serves` | symbol → route | route registration |
| `requests` | symbol → route | client call matched to a route |
| `calls_external` | symbol → external | SDK import or host |

Tests aren't a separate edge kind. They are `calls` from test files, and the
file node knows it is a test.

Every edge carries:

- **labels**, a set: `exact`, `scoped`, `observed`, `told`, `doubtful`,
  `proposed`. An edge proven by code and later seen on the wire carries both
  `scoped` and `observed`. `doubtful` and `proposed` never combine with a
  proving label.
- **site**: `file:line:col` of the use.
- **chain**: for join edges, the resolution steps, each with its own site.
- **source**: extractor and version (`go@1`, `join/aws@2026-09`).
- **owners**: what it was derived from (§6).

## 3. Coverage facts

`impact` may say "complete" only under the rules in
[context-system.md](context-system.md) §4.8. The graph stores what those rules
read:

| Fact | Per | Example |
|---|---|---|
| level | app | `worker: go exact` |
| skipped | package or file | `3 packages: dependencies not downloaded`; `2 files: build-constrained for windows, syntax only` |
| unresolved site | catalogue call site | `worker/main.go:69 ReceiveMessage: queue unresolved` |
| hazard | symbol | reflection by name (`reflect.Value.MethodByName`, `Call`) in a package that can reach the target's package; method values stored in maps or fields; names built from strings at run time |
| dependency dispatch | method | `ServeHTTP` is called by `net/http` through `http.Handler`. This is listed in answers, and doesn't make an answer partial, because the caller is outside the stack |
| pending | app | files changed but not yet re-indexed |

An unresolved site makes a resource's `impact` partial only when it could
name that resource. It must be the same operation family, on a client bound
to the same module. A partially resolved value whose known parts can't match
the resource (`Concat("billing-", ?)` against queue `orders`) doesn't count.

Hazards are found syntactically. They mark where a caller could hide; they
don't prove one exists. Data-flow analysis is out of v1 (README).

## 4. Where facts come from

### 4.1 The declared-stack file

An optional input ([running.md](running.md) §1). With none, recall builds the
same app, env and run nodes from manifests and `.env` files, and infers
resources from code (§2–§3 there).

The format is recall's, published with a JSON Schema and a version. doze is
its first producer: it writes `.doze/recall/stack.json` on every config load
and on `doze recall stack` ([fit.md](fit.md) §2). Recall reads it on each
wake and whenever the file changes, and never parses HCL.

```json
{
  "contract": 1,
  "producer": "doze 0.9.0",
  "stack": "shop",
  "sources": [{"path": "doze.hcl", "sha256": "…"}, {"path": "doze.lock", "sha256": "…"}, {"path": "worker.env", "sha256": "…"}],
  "partial": [],
  "logs": {"worker": ["/Users/…/.doze/projects/shop-1a2b/logs/worker/current.log"]},
  "blocks": {"app.worker": "sha256:…", "module.cloud": "sha256:…"},
  "apps": [{
    "name": "worker", "dir": ".", "run": "go run ./cmd/worker",
    "env": [
      {"key": "AWS_ENDPOINT", "ref": "module.cloud.url"},
      {"key": "ORDERS_QUEUE", "value": "orders"},
      {"key": "KAFKA_BROKERS", "ref": "module.events.address"}
    ],
    "env_files": ["./worker.env"],
    "waits_on": ["task.migrate"]
  }],
  "modules": [{
    "name": "cloud", "type": "aws", "version": "…",
    "resources": [{"kind": "queue", "name": "orders", "attrs": {"url": "http://aws.shop.doze/000000000000/orders"}}]
  }],
  "tasks": [{"name": "migrate", "run": "…", "inputs": ["migrations/**"]}]
}
```

`blocks` are the per-block fingerprints doze already computes
(`internal/config/fingerprint.go`). They let derived facts be owned by a
block without recall parsing HCL.

Paths in the file are relative to the folder holding the file's `.doze/`
(or the file itself, for `stack` in `recall.toml`), which need not be the
git root: recall looks for `.doze/recall/stack.json` from the working
directory up to the root.

- **`sources`** lets recall see the file go stale: when one of them changes,
  the declared facts are `stale` until the producer writes again. It can't
  capture the process environment the producer saw; values that came from
  it are marked so.
- **`partial`** lists what the producer couldn't include (a module whose
  plugin isn't fetched), so answers say partial instead of treating its
  resources as absent.
- **`logs`** lists plain-text log files per app, for `diagnose`'s recent log.
  Recall reads the last lines and nothing else of the producer's formats.

Gaps in doze to fill, as its producer, from the code:

- **`ref` per env key.** Which reference fed which key is lost after
  `evaluate`. Capture it by walking each env expression's traversals
  (`internal/config/eval.go`). A template like `"http://${app.api.address}"`
  gives one `ref` per traversal and keeps `value` for the literal parts.
- **`resources` of the aws and kafka modules.** Plugins decode their own
  bodies, so core doesn't know their queues and topics. `Inventory.Objects`
  is **not** the way: both plugins implement `Converge`, so the host treats
  them as structural (`doze-sdk/plugin/adapter.go:84-104`). Answering
  `Objects` would make `doze plan` and `sync` start diffing queues, and
  removing one would fail at `Prune`. Recall needs a separate, read-only
  capability (working name `Declared`) that returns declared resources with
  the attributes doze mints (URL, ARN). plan and sync never call it. It is a
  small SDK addition plus one method in each plugin.
- **`env_files`.** Recall reads the keys and the values it needs for
  matching (resource names, hosts). It drops values of keys that look like
  secrets (`*_KEY`, `*_SECRET`, `*_TOKEN`, `*_PASSWORD`), the password part
  of any URL with userinfo (`postgres://user:pass@…`), and strings in known
  key formats, before anything is stored. Env a producer inherits from its
  own process is not included.

**Run facts.** Each app node also carries what `brief`'s "Run and check" needs:

- the run command and what must be awake, from the stack file, else the
  manifest;
- a test command derived from the app's code: `go test` over the packages
  the app uses, and `task` blocks it waits on. When nothing gives one, the
  fact is absent, never guessed.

From the stack file recall builds app, module, resource, env var and `wires`
nodes and edges.

- A `wires` edge from a `ref` is `exact`.
- An env literal that names a declared resource without referencing it
  (`ORDERS_QUEUE = "orders"`) gets a `wires` edge labelled `scoped`, with
  "matched by name" in its chain.
- When two modules declare a resource of that kind and name, the edge is
  `doubtful`, to each.

Today every queue and topic in the shop example is wired this way. Neither
plugin exposes per-resource attributes (`module.cloud.queue.orders.url` can't
be referenced). Adding those attributes would make the wiring `exact`, and
would help people too, but recall doesn't depend on it.

### 4.2 Go

The gopls loop, without the editor
([research/3](research/3-breadth-speed-memory.md) §2).

**Modules.** Every Go module under the apps' folders, or listed in a
`go.work`, is one unit. It is indexed once, whichever apps use it.

**Metadata:**

```
go list -e -deps -export -json ./...
```

- It runs with `GOTOOLCHAIN=local` and `GOPROXY=off`, so it never downloads
  a toolchain or a module.
- The cache key is the hash of `go.mod`, `go.sum` and `go.work`, plus the
  set of files and their build tags.
- `-e` keeps one broken package from failing the module.
- If the module needs a newer Go than the local one, the whole module falls
  back to syntax, with "needs Go 1.28; installed 1.27".
- If export data can't be built because cgo has no C toolchain (common on
  Windows), recall retries with `CGO_ENABLED=0` and marks the cgo files
  skipped.

**Checking.** Workspace packages are checked in dependency order by up to 4
workers:

1. Parse the package.
2. Type-check it with `go/types` against compiler export data.
3. Extract its facts.
4. Drop its syntax and `types.Info` before the worker takes the next
   package.

Imported `*types.Package` values sit in an LRU bounded by bytes. That bound is
the one setting that decides memory: the measurements ranged from 86 MB
(serial, unshared) to 1.6 GB (an unbounded shared map on Kubernetes).

**Extracted:**

- Definitions with spans and signatures.
- `references` and `calls` from `types.Info.Uses` and call expressions.
- `implements` for every interface declared in the workspace. Calls through
  an interface land on the interface method, and `impact` on a concrete
  method includes callers of every workspace interface method it implements.
  Those callers are labelled `scoped`, because the call may dispatch
  elsewhere.
- `reads_env` and the string constants at catalogue call sites.
- Route registrations.
- String literals passed to logging calls and error constructors (`log.*`,
  `slog.*`, `fmt.Errorf`, `errors.New`, and the catalogue's logging rows),
  for `diagnose` to match log lines and errors to code.
- Hazards (§3).

**Build constraints.** `go/types` sees one GOOS and GOARCH, the host's. Files
excluded by build tags are parsed with `go/parser`. Their references are
matched by name within the package and labelled `doubtful`, and the package
gets a skipped fact. `impact` is partial only if such a file mentions the
target's name.

**Undefined names.** When type checking reports an undefined name or
selector, which is what a caller of a removed or renamed declaration looks
like, the use is kept as a `doubtful` reference to the id the name used to
have, from the previous generation's facts. `impact` on a diff lists these as
the callers a rename broke ([answers.md](answers.md) §3.9).

**Labels.** Go edges from type checking are `exact`. A package whose
dependencies are not on disk is extracted by `go/parser` alone and marked
skipped.

### 4.3 The join

The join turns call sites into resource edges.

**The operation catalogue** has one row per operation that touches a
resource. A row has a **shape**, because real clients name resources in more
than one way:

| Shape | Matches | Example row |
|---|---|---|
| `arg` | a method argument or a field of an argument struct | `aws.sqs  *sqs.Client  SendMessage  arg.QueueUrl  send  queue` |
| `literal` | a composite literal of a type, wherever it flows into a listed method | `kafka  kgo.Record{Topic}  → (*kgo.Client).Produce,ProduceSync  send  topic` |
| `option` | an option function given when a client is built; the client then carries it | `kafka  kgo.ConsumeTopics(...)  → kgo.NewClient  receive  topic` |
| `output` | a result field that names the same resource as an argument | `aws.sqs  GetQueueUrl  arg.QueueName → out.QueueUrl  same` |
| `identity` | a wrapper that returns its argument | `aws.String`, `aws.ToString` |

The shop worker needs `output`: it calls
`GetQueueUrl(&sqs.GetQueueUrlInput{QueueName: aws.String(queue)})` and passes
`url.QueueUrl` to `ReceiveMessage` (`examples/shop/cmd/worker/main.go:63-69`).
It needs `option` for `kgo.ConsumeTopics("orders")`. The api's producer needs
`literal`: `ProduceSync` with a `kgo.Record{Topic: …}`.

Where rows come from:

- **AWS rows** are generated by `doze-recall/tools/catalogue-aws` from AWS's Smithy
  models and Service Authorization Reference, plus an override file of about
  10 rows. The models are not vendored anywhere: `doze-aws/.audit-models` is
  a gitignored cache. So the tool fetches models pinned by commit, and the
  generated rows are committed to recall. Regenerating is a deliberate step,
  not part of every build. CI checks the rows against a recorded fixture of
  doze-aws's request decoding, committed to doze-recall and regenerated by
  a tool when doze-aws changes.
- **Kafka** (franz-go, Sarama, segmentio, confluent-kafka-go), **SQL**
  (`database/sql`, pgx, sqlx, sqlc), **HTTP** (ServeMux patterns, chi, gin,
  echo; `net/http`, resty clients), **logging** and **external SDK** rows are
  hand-written in `doze-recall/internal/golang/catalogue/`.

**Which module a client talks to.**

- If the stack has one module of that kind, that's it.
- Otherwise recall resolves the client's endpoint (for AWS, the
  `BaseEndpoint` or endpoint resolver option, usually from `AWS_ENDPOINT`)
  through the value resolver to a `wires` edge.
- Unresolved, every candidate module gets a `doubtful` edge.

**The value resolver** finds what the resource argument is:

| Value | Example |
|---|---|
| `Const(s)` | `"orders"`, `const Q = "orders"`, through `identity` wrappers |
| `Env(name)` | `os.Getenv("ORDERS_QUEUE")`, a field filled by an envconfig tag |
| `Concat(parts)` | `base + "/orders/" + id`, `fmt.Sprintf` |
| `Same(site)` | an `output` row's result, naming what its input named |
| `OneOf(values)` | a parameter whose callers pass different values |
| `Unknown` | read from a file, a database or a flag at run time; a response field with no `output` row |

It runs over `go/types`, so constant values are exact. It follows pointers
taken of locals (`&s`) and struct fields assigned in the same function. Config
structs get field-based summaries: a field assigned once from an env read
takes that read's value wherever it is used. Config libraries have rules:
envconfig, caarlos0/env, viper, koanf. Values crossing function boundaries
take the union of their callers' values (`OneOf`).

**The join** carries `Env(name)` through the app's `wires` edges, and matches
`Const` values against declared resource names and the attributes doze mints
(queue URLs, ARNs, from the stack file).

| Value resolves to | Edge |
|---|---|
| one declared resource | `scoped`, with the whole chain as evidence |
| a name no module declares | an `undeclared` resource node, `scoped` edge, and a Review item: used in code, missing from the stack file |
| anything, when there is no stack file | an `inferred` resource, `scoped` at best, never `exact` ([running.md](running.md) §3) |
| several candidates | one `doubtful` edge per candidate |
| `Unknown` | no edge; an unresolved-site fact (§3) |

**SQL** goes through `wasilibs/go-pgquery`, the Postgres grammar in pure Go,
to tables and, where the schema is known, columns. The schema comes from
migrations and sqlc schema files. A live snapshot is taken only if the
database is already awake. **HTTP** `requests` edges match a client's resolved
path template against the target app's routes segment by segment; two
matching routes give two `doubtful` edges.

### 4.4 Dependencies

From `go.mod`, `go.sum` and `go list -m -json`: every direct dependency, its
pinned version, and its directory in the module cache when it is there.
Dependency symbols exist only for symbols the code uses, with signatures from
export data. They are keyed `module@version` and cached machine-wide; a
version's facts never change, so a save never revisits them.

For doze modules the graph holds the engine version and, for doze-aws, the
operations it doesn't implement, so `brief` can warn before an agent builds on
one.

The packs fill the same shape from their lockfiles: `package-lock.json`,
`pnpm-lock.yaml` and `yarn.lock`; `uv.lock`, `poetry.lock` and
`requirements*.txt`; `Cargo.lock`. Each dependency gets the path of its
source on disk when it is there (`node_modules`, the venv, the cargo
registry). Dependency symbols come from the language server's answers about
symbols the code uses.

### 4.6 Language packs

TypeScript/JavaScript, Python and Rust are packs. A pack declares:

| Part | What it is |
|---|---|
| globs and root markers | which files it owns, and which file marks a project root (`tsconfig.json` or `package.json`, `pyproject.toml`, `Cargo.toml`) |
| queries | tree-sitter queries for definitions, references, imports and exports, string literals, env reads (`process.env.X`, `os.environ["X"]`, `std::env::var("X")`), call sites, logging and error literals |
| resolver | import and scope resolution: proves a reference's target (`scoped`), or keeps every candidate (`doubtful`) |
| id rules | how its symbols are named (below) |
| hazards | where a caller could hide: dynamic property access, `getattr`/`setattr` with computed names, `__getattr__`, `eval`, computed `require`/`importlib`, trait objects across apps |
| catalogue rows | the join's rules in that language's call shapes (§4.3) |
| server | the pinned language server, its readiness rule and root selection (§4.7) |
| oracle | a corpus where the server's own answers are ground truth for a sample |

**Three steps per file:**

1. **Syntax.** gotreesitter parses, the queries extract facts, and the tree
   is released before the next file. Trees never leave the worker: keeping
   them cost 8.5 GB for 8 MB of source.
   - 2 workers, 4 for a first index the person waits on.
   - Files over 256 KB, minified files and generated files are skipped and
     listed.
   - Each parse has a time budget; a file that hits it is recorded as
     partial.
2. **Resolution.** The pack's resolver labels each reference `scoped` or
   `doubtful`. 52–79% of references in the measured repos point at a name
   declared more than once, so this step alone can't be enough.
3. **Precision.** The doubtful sites go to the language server as
   `textDocument/definition` requests. An answer on a known symbol makes the
   edge `exact`; an answer in a dependency makes a dependency-symbol edge; no
   answer leaves it `doubtful`.

**The value resolver** for packs works at syntax level: literals,
module-level constants, env reads, same-file concatenation. Resource names
are mostly literals or env reads, so this reaches most join sites. What it
can't settle stays an unresolved site, which generation can later propose a
verdict for.

**Ids**, with the same stability rules as Go (§1): keyed by the code's own
package name, never by the app.

| Pack | Id | Example |
|---|---|---|
| TypeScript, JavaScript | `ts:` package name `/` path in package `#` export chain | `ts:@acme/web/src/api/orders.ts#fetchOrders`, `…#default`, `…#OrderService.create` |
| Python | `py:` distribution or package `/` module path `.` qualified name | `py:acme_billing/billing.jobs.receipt.send_receipt` |
| Rust | `rs:` crate `::` module path `::` item | `rs:acme_ledger::ledger::post::post_entry` |

- **TypeScript:** declaration merging gives one node; overloads are one node
  with several signatures; a file outside any package uses its path from the
  stack root.
- **Python:** `@overload` variants are one node.
- **Rust:** trait methods are named by the trait:
  `rs:crate::module::Trait::method`, with each impl a separate node that
  `implements` it.

Only the grammars of the three packs (TypeScript, TSX, JavaScript, Python, Rust) are compiled in, through gotreesitter's
`grammar_subset` tags. Their measured size was 15.2 MB for the Go, TypeScript,
TSX and JavaScript grammars. The real binary, with SQLite, x/tools,
go-pgquery and Python and Rust grammars, is measured in phases.md P3.5.

### 4.7 Language servers

Each pack's server is a child process of recall ([running.md](running.md)
§6). Its binary comes from recall's
compiled-in `{url, sha256}` table, mirrored in doze-binaries releases so
`DOZE_MIRROR` works, and is kept in recall's own cache, so nothing is
written to the person's `doze.lock`.

| Pack | Server | Download | Notes |
|---|---|---|---|
| TypeScript, JavaScript | TypeScript 7 `tsc --lsp --stdio` | 9 MB, no Node | requests block until the project loads |
| Python | ty; pyrefly as the alternate | 12 MB | root at `pyproject.toml`; uses the venv when there is one |
| Rust | rust-analyzer | 14 MB | build scripts and proc macros off; recall writes `rust-project.json` when cargo is absent; ready after its progress ends and it reports quiescent |

Rules every server follows:

| Rule | Why |
|---|---|
| Root chosen by recall: the nearest marker under the app's `dir` | pyrefly started one directory too deep found 4.3× fewer references |
| A readiness rule per server; no generic "wait for progress" | four of five servers measured never report progress; rust-analyzer answers early with 78% of references missing |
| A canary after readiness: three symbols the syntax layer saw used across files must return references | rust-analyzer without cargo returned zero references and no error |
| Server-to-client requests answered with defaults | some servers stall otherwise |
| Never a project-local server; build scripts, proc macros and project imports off | opening a repo must not run its code |
| One server at a time per machine; memory ceiling 2 GB, then stop and settle only changed files' sites | servers measured at 0.1–1.4 GB on medium repos |
| One restart on crash, then the app is degraded for this pass | |

**Lifecycle:**

- Recall owns the server process, and each process serves one LSP
  session.
- Recall holds the connection while it has doubtful sites to settle, plus 2
  minutes.
- Then recall stops the server.
- The next batch starts a fresh server and session in the background, paying
  the project load and canary again.

Answers never wait on a server. A doubtful site an agent asks about is
answered at once under Unconfirmed, and settling it is queued.

A server that fails a rule leaves its app at `scoped`/`doubtful`. The
Overview shows why, and answers about that app say partial.

### 4.8 Pack rules that differ from Go

Go's rules in §3, §6 and answers.md lean on whole-program type checking,
which the packs don't have. Each pack settles these in its phase; the
defaults are:

| Topic | Rule for packs |
|---|---|
| Incremental owners | A pack's config files own every resolved edge in their project: `tsconfig.json` and `package.json` (paths, exports, workspaces), `pyproject.toml`, `Cargo.toml`. A change re-resolves that project. A file's surface is its exported declarations and signatures, as in Go |
| Server answers going stale | A cached definition answer is dropped when the file it points into changes its surface, found through a reverse index from target file to cached sites, not only when the asking file changes. Without this, "incremental equals full" fails |
| Oracle | `exact` edges are the server's answers, so checking them against the server is circular. Each pack's oracle is a hand-checked sample: 50 sites per corpus, with the expected target set by a reviewer agent reading the code and spot-checked by the owner. Both `exact` and `scoped` edges are scored against it |
| Diffs | Removed or renamed declarations are found by diffing declarations between the base and the current file's syntax, with no type checker. References to the old name become `doubtful` edges to the missing id |
| Conditional code | Code under conditions the server doesn't evaluate is parsed and labelled `doubtful`, like Go's build-tagged files: TypeScript export conditions, Python `if TYPE_CHECKING:` and `sys.platform` branches, Rust `#[cfg(...)]` items outside the default features |
| Rust macros | Proc macros are off, so derives, `#[tokio::main]` and `sqlx::query!` stay unexpanded. The catalogue matches macro invocations by syntax (`sqlx::query!("SELECT …")` with literal SQL); what a macro generates is a hazard, not an edge |
| Python receivers | boto3 methods are created at run time, so the server can't resolve them. Rows match `boto3.client("sqs")` and the variable it's bound to, within a file and through module-level clients imported elsewhere. A method call on a receiver of unknown type is a hazard, so `impact` on a Python method is usually partial, while module-level functions can be complete |
| Dependency symbols | Exist only where a definition answer for a doubtful site landed in a dependency. Versions and source paths always come from lockfiles |
| Failure states | `syntax only: node_modules not installed` (the server still runs, with unresolved packages as `any`), `syntax only: server download failed`, `no venv: third-party types unknown`, alongside Go's in §8 |
| Server memory | One recall language server at a time per machine, not only per stack. Measured peaks run higher than the research's medium repos (tsc on hono reached 724 MB); the 2 GB ceiling holds, and the bench records the peak on `parcel` |

### 4.9 Told and proposed

Notes, `topology.yaml` entries and approved verdicts are read from committed
files and attached to nodes by id (context.md, planned). Agent proposals
arrive through generation ([generation.md](generation.md)) and stay `proposed`
until a person approves them. No extractor produces either.

## 5. Indexing

### 5.1 One writer

All index work goes through one queue, served by one writer goroutine:

- **Passes.** Stack, then Go modules, then the join, then publish.
- **Refreshes.** The read-your-own-writes re-index of files an answer is
  about to cite.

Refreshes jump the queue. A pass yields between packages, so a refresh waits
at most for one package check (median 1 ms, worst 126 ms measured). There is
no locking between readers and the writer: answers read an immutable
generation.

### 5.2 Publish

Publish writes a new generation: the snapshot, the SQLite rows and
`status.json`. Answers switch to it atomically.

- Changes the writer applied during the pass, such as refreshes, are part of
  the pass's input, because the pass re-reads file hashes before it extracts
  each package.
- Modules publish as they complete, so a big one doesn't hold back the
  others.

### 5.3 When it runs

Per decision C3: once when recall first runs on a root, then only when recall is
woken (an agent session starts it, or a person runs `review` or opens the dash's recall screen), starting with a
catch-up from the change feed. `doze up` doesn't start it. Work runs at low
priority (`nice` on macOS and Linux, below-normal priority class on Windows)
with `GOMAXPROCS` capped at 4.

### 5.4 What is skipped

- `.gitignore`d paths.
- Nested git worktrees. doze-aws has 12,274 `.go` files under `.claude/`
  worktrees.
- `vendor/`, `testdata/`, and generated files (`// Code generated … DO NOT
  EDIT.`).
- Files over 256 KB.
- The person's `exclude` globs. `include` adds prose folders only.

## 6. Incremental updates

**Files.** A file's facts are owned by the file and keyed by its content hash.
A save re-extracts its package (Go checks packages, not files).

**Surface hash.** Each package has a surface hash over its exported objects'
type strings (`types.ObjectString`), sorted, **with positions stripped**.
Compiler export data includes positions, so hashing it would make every
inserted line re-check the importers.

- If the surface is unchanged, only the package itself is re-checked.
- Otherwise its direct importers are re-checked, outward only while their
  surface hashes keep changing.
- doze-aws: median 1 importer, p90 12, max 31.

**Added names.** When a package gains an exported name, every `doubtful` edge
and unresolved site in its importers that mentions that name is resolved
again. Removing or renaming a name is covered by the surface change.

**Config files** are owners too:

- `go.mod`, `go.sum` or `go.work` changing re-runs `go list` for that module.
- `stack.json` changing re-derives the stack facts and every join edge.

**Join edges** are owned by every input in their chain: each file a step's
site is in, and the `blocks` fingerprint of each `doze.hcl` block the chain
passes through. A change to any owner drops the edge and derives it again.

**Deletions** remove the file's facts and every edge it owned, then follow
the surface rule.

**Equality.** After any series of edits, the incremental graph must equal one
built from scratch (§9).

Each publish bumps the generation number. Answers report it, and cursors carry it.

## 7. Storage

| What | Where | Format |
|---|---|---|
| Graph for traversal | `~/.doze-recall/projects/<root hash>/graph.snap` | Header: magic, format version, generation, stack, commit per repo. Then interned strings, a node table, and a sorted edge array `{from, to, kind, labels, site}` with a reverse permutation and offsets. Measured: doze-aws's 577k edges took 17 MB, about 30 bytes each, without labels or columns; budget 40. Loaded with one read on wake |
| Search, chains, coverage facts, notes index, query log | `~/.doze-recall/projects/<root hash>/graph.db` | SQLite, pure-Go driver (`modernc.org/sqlite` or `ncruces/go-sqlite3`, chosen once FTS5 support and speed are checked) |
| Symbol and prose search | `graph.db`, FTS5 | Identifier columns (name, qualified name, signature) whole and split (`createOrder` → `create order`), unstemmed; prose columns (doc comments, notes, glossary) through FTS5's built-in `porter` tokenizer |
| Per-module facts | `~/.doze-recall/cache/go/` | Keyed by module content state + extractor version. Holds app-independent facts only, so two stacks share them |
| Dependency facts | `~/.doze-recall/cache/deps/` | Keyed `module@version` + extractor version |
| Declared-stack file | `.doze/recall/stack.json`, or `stack` in `recall.toml` | Written by its producer (§4.1) |
| Status | `~/.doze-recall/projects/<root hash>/status.json` | State, counts, level per app, last error; read by `doze-recall status` and doze's dash |

All of it is disposable. A format change rebuilds; nothing is migrated.
Source text is never stored; snippets are read from disk when an answer is
built.

## 8. Failure states

| State | Shown in the Overview | Effect on answers |
|---|---|---|
| Module indexing | `◐ indexing` | previous generation; `partial: indexing <app>` |
| Catching up after wake | `◐ catching up 12 files` | `partial: catching up` |
| Go too old for a module | `syntax only: needs Go 1.28` | that module's edges `doubtful`; `impact` partial |
| Dependencies not downloaded | `skipped 3 packages: run go mod download` | those packages syntax only |
| Pass crashed | `✕ failed · see logs` | previous generation keeps serving; footer says so |
| No index yet | `◐ first index` | one line: recall isn't ready; use grep meanwhile |

## 9. Tests

| Test | What it proves |
|---|---|
| Go oracle | On a sample of symbols (the net/http set from the design record's benchmark, plus doze-aws), callers match an oracle independent of the extractor: `gopls references`, run only in tests, and a type check per GOOS for build-tagged files. `exact` edges must match; `scoped` precision ≥ 0.98 |
| Ids | Renaming an app, adding a stack around the same module, and moving a declaration within a package keep ids; a rename produces an orphan with a re-anchor candidate |
| Catalogue conformance | Every AWS row's resource argument agrees with what doze-aws decodes for the same call, on a recorded fixture |
| Join on the shop fixture (`testdata/shop/`, copied from doze's `examples/shop` with a hand-written stack file) | `api —sends→ topic.orders` (literal shape), `worker —receives→ topic.orders` (option shape), `worker —receives→ queue.orders` through `GetQueueUrl` (output shape) and the `ORDERS_QUEUE` name match, each with the expected label and chain, and no unresolved sites |
| Incremental equals full | After a scripted series of edits (add, rename, delete, move between files, change `go.mod`, change the stack file), the incremental graph equals a full build |
| Budgets | `test/bench`: indexing peak, serving RSS and save-to-fresh latency on doze-aws, against budget.json |
| Determinism | Two full builds of the same tree produce byte-identical snapshots |
| Pack oracles | For TypeScript/JavaScript (hono plus the eval stack's TS services), Python (pydantic plus the eval stack) and Rust (ripgrep plus the eval stack): on a sample, `exact` edges match the server's own answers, and `scoped` precision is ≥ 0.98 |
| Server rules | Each server's readiness rule and canary, against a fixture with no dependencies installed, plus fakes that never report readiness, answer early, or return nothing; each ends ready, waited for, or degraded |
| Syntax parity | Our corpus parses the same with gotreesitter as with C tree-sitter, in CI |
| Windows | The Go loop with cgo off, path normalization in ids. doze-recall's CI runs Windows from P1; the watcher polls there in v1 |

## 10. After v1

These are designed so v1's shapes don't need to change. None of it is built in
v1.

**More languages.** Java, C#, PHP, Ruby and Kotlin as packs at syntax or
scoped level: a grammar, queries, a resolver and catalogue rows each, with no
server unless one exists that needs no JVM or .NET install. Their answers say
`scoped` or `doubtful` honestly, and "complete" is rare for them.

**Observed calls.** Edges labelled `observed`, from doze-aws and doze-kafka
recorders and a Postgres statement tee, once app identity and a persisted
recorder feed exist ([context-system.md](context-system.md) §4.5). The label
set and the chain format already have room for them.

## 11. Code

Recall is its own repository, `doze-dev/doze-recall`
([running.md](running.md) §8), built as `doze-recall`. v1:

```
doze-recall/
  go.mod
  install.sh
  cmd/doze-recall/main.go   serve (the stdio shim), the background process, index, check,
                            and the terminal commands
  internal/
    inputs/     the root, apps from manifests, app names, .env files, recall.toml;
                the declared-stack file: decode, stack nodes, wires edges, run facts
    graph/      ids, node and edge types, labels, coverage facts, the snapshot
    store/      graph.db: schema, FTS5, chains, query log
    prose/      READMEs, ADRs, OpenAPI, AsyncAPI, proto, agent files into search
    index/      the writer queue, passes, publish, generations, incremental updates
    feed/       recall's own watcher, event ids, wake catch-up, pending files
    discover/   modules and packages per app, every language; the skip rules
    golang/     go list, workers, import LRU, extraction, hazards, build-tag files
      catalogue/  Go rows: AWS generated; Kafka, SQL, HTTP, logging hand-written
    syntax/     gotreesitter: parse, run queries, release, limits, parity corpus
    lang/       pack.go, the framework; one folder per pack:
      typescript/ queries, resolver, ids, hazards, server, catalogue, testdata
      python/
      rust/       also rust-project.json generation
    lsp/        client, readiness quirks, canary, batching, site cache, lifecycle
    join/       value domain and resolver, catalogue engine with its five shapes,
                client-to-module binding, the join, SQL, HTTP matching,
                inferred resources when there are no declarations
    deps/       go.mod and module cache, machine-wide cache
    serve/      MCP, sessions, the answer pipeline, tools (answers.md §11)
    notes/      notes, glossary, topology.yaml decisions, proposals, writes
    status/     status.json
    bgproc/     the background process: socket, lock, idle exit, the local API
    shim/       `serve` over stdio
    cli/        review, generate, status, connect
    generate/   jobs, the Claude Code runner, its contract test (after the pilot)
  eval/         the harness and the retrieval eval; tasks live in the private
                doze-recall-eval (eval.md §2)
  tools/
    catalogue-aws/  fetches pinned Smithy models + SAR, writes the AWS rows
```

Changes outside doze-recall for v1, unit by unit in [phases.md](phases.md):

| Where | Change |
|---|---|
| `internal/config/eval.go`, new `stackfile.go` | keep the env key ← reference map from `evaluate`; write the stack file |
| `internal/config/load.go`, new `recall.go` | `recall` in `reservedBlocks`; the reserved name; the `recall {}` block |
| `internal/daemon` | write the stack file on every config load |
| `internal/dash/` | the header strip, a tab component, recall's screen as a client of the local API |
| `cmd/doze` | `doze recall <args>` running the pinned copy, `doze recall stack`, `doze recall check`; the parity exemption in `parity_test.go` |
| doze-binaries | mirrored server archives (tsc 7, ty, pyrefly, rust-analyzer) |
| `test/bench/` | the idle bench, unchanged |
| `cmd/doze`, `internal/runtime/` | the pinned `doze-recall` version and SHA-256, downloaded on the first `recall {}` |
| doze-sdk (`engine/driver.go`, `plugin/proto/engine.proto`, `plugin/adapter.go`, `plugin/server.go`) | the read-only `Declared` capability |
| doze-aws, doze-kafka | implement `Declared`; doze-aws includes its implemented operations |

## 12. Open

- **Generated clients carry cross-app links.** The skip rule above drops
  generated files, but a generated OpenAPI client is often the only place a
  client's route paths appear (parcel's plan has eight storefront → orders
  routes that live only there). Leaning: skip generated files for search
  and symbol listing, but run the join's catalogue over them, or match the
  client against the OpenAPI spec it was generated from. Decide before P5;
  until then, eval tasks on those routes are reported apart.

- Whether interface dispatch across apps (a shared interface, implementations
  in two apps) needs more than `scoped` callers. Measure on the eval stacks.
- The import LRU's byte bound for Kubernetes-sized modules.
- Which pure-Go SQLite driver.
- Whether to add per-resource attributes to the aws and kafka plugins
  (`module.cloud.queue.orders.url`), which would make queue wiring `exact`
  and is useful to people regardless of recall.
