# How recall runs

Status: design, 2026-10-21; replaces the earlier `standalone.md`, which made
doze the main host. Nothing here is implemented. Decided in R96–R103
([decisions.md](decisions.md)).

**doze-recall is a code graph for systems of services, independent of
doze.** It reads a repo (or a few sibling repos), finds the apps in them,
builds the graph and the links between apps, and serves it over MCP to any
agent. It imports nothing from doze or doze-sdk. It lives in its own
repository and is released on its own, like doze-kafka: a Kafka for anyone
that doze happens to run well.

doze is one **input**. When a project has a `doze.hcl` with `recall {}`,
doze writes a declared-stack file that recall reads, and the links recall
reports go from inferred to declared. Anything else could write that file
too.

Where recall should win is not symbols and references, which many code
graphs already serve, but the links between services: who produces and who
consumes a topic, a queue, a table or a route, across Go, TypeScript,
JavaScript, Python and Rust, with people's notes, freshness, and a straight
answer about whether a list is complete. A single small app gets little
from it, and the pitch says so.

## 1. Inputs

| Input | Always? | Gives |
|---|---|---|
| The code under the root (§2) | yes | apps, symbols, calls, routes, every resource name the code uses, run and test commands from manifests |
| `.env` files in app folders | when present | env values for the join |
| A declared-stack file ([graph.md](graph.md) §4.1) | optional | declared apps, resources with their attributes, env wiring with provenance, run facts, log locations for `diagnose` |
| `.recall/` files people commit ([context.md](context.md)) | when present | notes, glossary, decisions, `recall.toml` |

**Where the stack file is found,** first match wins:

1. `stack` in `.recall/recall.toml`, a path under the root;
2. `.doze/recall/stack.json`, where doze writes it, looked for from the
   working directory up to the root, since `doze.hcl` need not sit at the
   git root.

Knowing doze's path is the only thing recall knows about doze. It's a file
location, not a code dependency.

| | Code only | Code plus a stack file |
|---|---|---|
| Apps | found from manifests (§2) | as declared; code still decides which packages each app uses |
| Resources | inferred from names in code (§3), `scoped` at best | declared; `wires` edges `exact` where the file gives a reference |
| Env | `.env` files | the resolved values and where each came from |
| Run facts | from manifests | the producer's run commands, waits and tasks |
| `diagnose` | from the error text the agent passes | also the logs and journal the file points at |

The graph, the six tools, the labels, freshness, budgets and the answer
pipeline are the same code either way. Only the inputs differ.

## 2. The root and the apps

**The root** is picked by the shim when it starts, before MCP's
`initialize`, since a client's roots arrive only after it:

1. the git repository containing the working directory;
2. otherwise the working directory.

When the client later sends roots, recall checks them against its root. If
none is inside it, every answer starts with one line saying which root
recall is serving. With several roots, the one containing the working
directory wins.

**Recall refuses to start** when the root is the home folder or the file
system root, or when no app is found under it. It says why, and how to
point it somewhere else, rather than indexing a whole home folder.

**Apps** are found from manifests under the root, skipping `.gitignore`d
paths, vendored folders and `node_modules`. Discovery reads manifests as
text. It never runs package scripts, build backends, `setup.py` or
`build.rs`:

| Language | An app is | Its entry points |
|---|---|---|
| Go | a `main` package | that package |
| TypeScript, JavaScript | a `package.json` with a `start` or `dev` script, `bin`, or a framework config (`next.config.*`, `vite.config.*`) | the files those name; Next.js routes from the app and pages folders |
| Python | a `pyproject.toml` with `[project.scripts]`, or a folder with `__main__.py`, or an ASGI or WSGI app object the syntax layer finds | those |
| Rust | a `Cargo.toml` with a `[[bin]]` or `src/main.rs` | those |

A library with no entry point is not an app. It is indexed as code the apps
use. When a stack file declares apps, its apps win, and discovery only
supplies entry points the file doesn't name.

**`.env` files** in an app's folder (`.env`, `.env.local`, `.env.example`)
are read even when `.gitignore`d, since they usually are. They are parsed
literally: no `$(…)`, no variable expansion beyond `${OTHER_KEY}` from the
same file. The secret rules ([graph.md](graph.md) §4.1) drop secret values
before anything is stored, and evidence chains show the file and key, never
a value.

**App names** are the folder's name (`orders` for `services/orders`). When
two clash, the path relative to the root with `/` turned into `-`
(`services-orders`, `tools-orders`). App ids are `app.<name>`.

**Optional config,** `.recall/recall.toml` at the root:

```toml
roots   = ["../billing", "../web"]        # other git repos that are part of this system
include = ["docs/**", "adr/**"]           # extra prose
exclude = ["fixtures/**"]                 # .gitignore is always respected
agents  = ["claude", "codex"]             # who may run generation here
stack   = "deploy/stack.json"             # a declared-stack file, if not doze's

[apps.orders]                             # only when discovery gets an app wrong
dir = "services/orders"
```

Nothing else. With no file, recall works on what it finds, and `brief`
says which apps it found and how, so a person sees a wrong guess at once.
This one file replaces the keys the `recall {}` block used to carry.

`recall.toml` is committed, so a repo someone cloned could use it to point
recall at their other files. So `roots` must each be a git repository, and
recall serves one only after the person accepts it once in `doze-recall
review`. Until then, `brief` says it was skipped. `include`, `exclude` and
`stack` only ever apply inside the root and accepted roots.

## 3. Resources without declarations

The join ([graph.md](graph.md) §4.3) runs unchanged up to the point where it
matches a resolved value against declared resources. With no stack file,
nothing is declared, and values that resolve to a name become **inferred
resources**:

```
inferred resource = "inferred:" family ":" kind "." name
                    inferred:aws.sqs:queue.orders
                    inferred:kafka:topic.order.events
                    inferred:postgres:table.orders
```

Declared ids name the module that declares a resource:
`module.cloud:queue.orders` is queue `orders` in the stack file's `cloud`
module. Inferred ids name the family instead. The family comes from the
catalogue row that matched (an SQS client, a Kafka
producer, a SQL statement), since there is no module to bind the client to.

| Value resolves to | Edge |
|---|---|
| a constant name, and another app uses the same family, kind and name | `scoped` to the inferred resource, with "matched by name, no declaration" in its chain |
| a constant name only one app uses | `scoped` to the inferred resource; `impact` says no other app was found to use it |
| an env key whose value comes from a `.env` file | as a constant, with the file in the chain |
| an env key with no value anywhere | no resource; a `doubtful` edge between apps that read the same key name for the same family, and an unresolved-site fact |
| a URL or ARN | its name parsed out (the queue name from an SQS URL), then as a constant |
| `Unknown` | an unresolved-site fact |

**Never `exact`.** A link between two apps through an inferred resource is
`scoped` at best. Two apps naming topic `orders` on two different brokers
would look linked, and only a declaration can say they aren't. Every answer's
coverage line says resources were inferred, and how many sites stayed
unresolved because an env value was missing.

**When a stack file appears,** each inferred resource maps to the declared
one with the same family, kind and name, and each app id maps by folder to
the declared app with that `dir` (its name may differ). Where exactly one
matches, notes and decisions anchored to the old id are re-anchored on the
person's keypress in `review`, never silently. The rest are listed there.

What code alone loses, plainly: names built from env values that live only
in deployment config, resources reached only through a config file recall
doesn't read (docker-compose, Terraform, Helm), and which of two brokers or
databases a client talks to. Stack-file producers for docker-compose and
Terraform are the first thing to add after v1, if E2's arms (§9) show that
declarations matter.

## 4. How it runs

```
claude mcp add doze-recall -- doze-recall serve      # installed on its own
claude mcp add doze-recall -- doze recall serve      # through doze's pinned copy
```

`doze-recall serve` is a thin stdio shim. It finds the root (§2) and
connects to that root's **background process** over a Unix socket in
`~/.doze-recall/run/`, starting it if it isn't running. The background
process holds the graph, the writer queue, the watcher and the language
servers. It exits 5 minutes after its last session closes, so with no agent
connected nothing runs. `doze recall serve` simply runs doze's pinned
`doze-recall serve`.

There is no network listener, no reserved address and no proxy. Stdio is
what every MCP client supports, and it works inside agents' sandboxes, which
loopback addresses and `.doze` names may not.

Why not one process per session: two agent sessions on one repo would each
index it and each write the graph. One background process per root keeps the
one-writer rule ([graph.md](graph.md) §5.1). Details:

- **Starting.** A lock file next to the socket (`flock`) decides which shim
  starts the process when two arrive at once. The lock dies with its holder,
  so after a crash the next shim finds the lock free, removes the stale
  socket and starts a new process.
- **Versions.** The shim sends its version first. When the shim is newer,
  the background process finishes its open requests and exits, and the shim
  starts its own. When the shim is older, it is served as long as the shim
  protocol (small, versioned on its own) matches, and told to upgrade
  otherwise. A person with both their own install and doze's pinned copy
  ends up on the newer one.
- **Overlapping roots.** One repo can be the root of one process and an
  accepted `roots` entry of another. Both may index it; the extraction cache
  is keyed by content hash and written by temp-file-and-rename, so they
  share it safely. Language servers take a machine-wide lock in
  `~/.doze-recall/run/`, so "one server per machine" holds across processes.
- **Access.** `~/.doze-recall/run/` is mode 0700, and the background process
  checks the peer's uid on every connection.

**The local API.** The same socket carries a small versioned JSON API besides
MCP: status, the items waiting for review, acting on one, the topology rows.
`doze-recall review` and doze's dash are both clients of it. `status` also
reads `status.json` from disk without starting the process, so a status
line never wakes recall.

Windows uses a named pipe in place of the socket and polls for changes. It
is tested in doze-recall's own Windows CI from P1 (R93).

## 5. Staying current

Recall runs its own watcher: FSEvents through purego on macOS, inotify on
Linux, git to reconcile, polling elsewhere
([freshness.md](freshness.md)). It saves its event ids under
`~/.doze-recall/projects/<root hash>/`. Starting the background process is
"waking": catch-up, read-your-own-writes and partial answers work as in
[freshness.md](freshness.md).

doze's own `watch` keeps its poller. The tree is watched twice when both
run, which costs little; sharing a watcher would have made recall depend on
doze.

**A stale stack file.** The file lists the source files it was made from
with their hashes (`doze.hcl` and the env files it read). When one of them
changes after the file was written, declared facts are labelled `stale`
until a new file arrives, and answers show the producer's refresh hint
(`doze recall stack`). Recall never runs the hint.

## 6. Language servers

Recall runs its language servers ([graph.md](graph.md) §4.7) as its own
child processes (C2, revised). It downloads each from its compiled-in
`{url, sha256}` table, honours `DOZE_RECALL_MIRROR` (a mirror URL for
offline machines; doze's `DOZE_MIRROR` works too), unpacks gzipped single
binaries, and keeps them in `~/.doze-recall/servers/`. One server at a time
per machine, a 2 GB ceiling, the readiness rule and canary, stop 2 minutes
after the last doubtful site. Recall logs them itself and lists them in
`status.json`.

One thing does run the repo's toolchain: Go export data comes from
`go list -export`, which compiles packages, and for cgo packages that runs
the C compiler. It compiles but never runs the code, and Go limits the
flags cgo directives may pass.

## 7. Review, files and commands

The rule from [context.md](context.md) §1 holds: recall writes a committed
file only when a person presses a key, in `doze-recall review` or in doze's
dash.

| Command | Does | Keys |
|---|---|---|
| `doze-recall review` | everything waiting for a person, in one list: proposals, drift on decisions, decisions code now proves, ids to re-anchor, `roots` to accept, stale and orphaned notes, short names to expand, note-check drafts | `a` approve · `r` reject · `e` edit · `n` note · `s` skip; on notes `c` checked · `t` re-anchor · `y` accept draft · `x` discard |
| `doze-recall generate` | runs generation's jobs with the person's agent CLI ([generation.md](generation.md)); results go to `review`. `--agent claude` picks the agent, remembered in `~/.doze-recall/config.toml` | none |
| `doze-recall status` | root, apps found and how, the stack file and its freshness, index state, servers, items to review | none |
| `doze-recall connect` | the line to paste for Claude Code, Codex, Cursor and others | none |
| `doze-recall check` | the CI check ([context.md](context.md) §8), exit 0 or 1 | none |

`review` is a plain list and prompt: one item at a time, its evidence, the
keys it takes. No map, no query box, no panes. doze's dash shows the same
items in its tabs, through the local API.

**Files**:

| What | Where | Committed |
|---|---|---|
| `recall.toml`; notes about the repo's code | `.recall/` at each root | yes |
| Notes across apps, `glossary.md`, `topology.yaml` | `.recall/stack/` at the root | yes |
| `graph.db`, `proposals.json`, `status.json`, the query log, event ids | `~/.doze-recall/projects/<root hash>/` | no |
| Extraction caches, language servers | `~/.doze-recall/cache/`, `~/.doze-recall/servers/` | no |
| The declared-stack file from doze | `.doze/recall/stack.json`, in doze's project folder, which ignores itself | no |

Stack notes live in `.recall/stack/` so that another system that includes
this repo as a sibling root doesn't load them as notes about the repo's
code.

## 8. Install and release

- **Repository:** `doze-dev/doze-recall`, public, its own Go module, with no
  dependency on doze or doze-sdk.
- **Install:** `install.sh` from the repo, the same shape as doze-kafka's:
  the prebuilt binary for the OS and architecture, its SHA-256 checked,
  installed to `~/.doze-recall/bin`, and the line to add it to `PATH`
  printed. macOS and Linux, amd64 and arm64.
- **Releases:** GitHub Releases through goreleaser, with a changelog.
- **Contracts it publishes,** each with its own version: the declared-stack
  file, the shim protocol and the local API. `doze-recall` accepts each
  contract's current version and the one before.
- **doze's copy:** doze pins an exact version and SHA-256 and downloads it
  the first time a `doze.hcl` has `recall {}` ([fit.md](fit.md)).

## 9. In the eval

The pilot (E1) measures recall **on code alone**, since that is what most
people will run: a plain repo with no `doze.hcl`, a `.env` file in each app
generated from the resolved config (what a team without doze would have),
and recall started by the agent ([eval.md](eval.md) §2 defines it). Those
`.env` files hold every value, which is inference's best case; E2 adds a
keys-only variant to show how much that matters. Its primary measure is A2 − A0 in that
setting ([eval.md](eval.md) §4).

E2 adds the doze setting: `doze.hcl` kept, doze's stack file written.

- **A2 − A0**, plain repo: what recall is worth on code alone.
- **A5 − A2:** what a declared stack adds to recall.
- **A0d − A0:** what `doze.hcl` alone gives an agent with no recall, so A5's
  gain isn't credited to recall when the agent simply read `doze.hcl`.

A plain repo is the harder case: links are inferred, so the effect may be
smaller and harder to see in 80 runs. The pilot reports what it can detect
at its size, as before.

## 10. Code

The repository's layout ([graph.md](graph.md) §11) includes:

```
internal/
  inputs/     code discovery (root, apps from manifests, names, .env files,
              recall.toml) and the declared-stack file reader
  feed/       recall's own watcher, event ids, wake catch-up
  join/       also: inferred resources and their ids
  bgproc/     the background process: socket, lock, idle exit, the local API
  shim/       `serve` over stdio, connecting to or starting the background process
  cli/        review, generate, status, connect; its list-and-prompt renderer
install.sh
```

## Open

- Whether `roots` should also accept a list of git URLs to clone, for
  systems spread over many repositories. Leaning no: the person checks them
  out.
- Whether the declared-stack file should be published as a spec page with a
  JSON Schema from v1, or once a second producer exists. Leaning: the JSON
  Schema from v1, since doze-recall tests against it anyway.
