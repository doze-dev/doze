# Recall without doze

Status: design, 2026-10-21. Nothing here is implemented. Decided in R96–R102
([decisions.md](decisions.md)).

Recall is its own product, `doze-recall`, in its own repository, released on
its own like doze-aws and doze-kafka. It runs in two ways:

- **Standalone.** A person who doesn't use doze installs one binary, points
  their agent at it, and gets the code graph and cross-app context for their
  repo. No `doze.hcl`, no daemon.
- **Hosted by doze.** doze runs the same binary as its built-in recall
  instance ([fit.md](fit.md)) and hands it the declared stack. Answers get
  declared resources, env provenance, run facts and the dash.

Most people who could use recall don't use doze. Standalone lets them have
it, and the difference between the two modes is the plainest argument for
doze there is: add a `doze.hcl` and the links your agent sees go from
inferred to declared.

## 1. What each host gives

| | Standalone | Hosted by doze |
|---|---|---|
| The stack | apps found from manifests under one root (§2) | `doze.hcl`, through `stack.json` ([graph.md](graph.md) §4.1) |
| Resources (queues, topics, buckets, tables) | inferred from the names code uses (§3) | declared by modules, through `Declared` |
| Env values | `.env` files in each app folder | the resolved config, with each key's `ref` |
| Run facts | run and test commands from manifests | doze's run commands, waits and tasks |
| Started by | the agent: `doze-recall serve` over stdio (§4) | doze's proxy, on a connection to the recall address |
| Change feed | recall's own watcher (§5) | the daemon's feed, shared with `watch` |
| Language servers | recall's own child processes (§6) | the same |
| Review, notes, generation | `doze-recall` commands in the terminal (§7) | the dash's Notes, Review and Topology tabs |
| CI check | `doze-recall check` | `doze recall check`, which runs `doze-recall check` |
| Files people commit | `.recall/` at the root | `recall/` beside `doze.hcl`, and `.recall/` in app repos |

The graph, the six tools, the labels, freshness rules, budgets and the
answer pipeline are the same code in both. Only what feeds the graph
differs.

## 2. Finding the stack without `doze.hcl`

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
use, the same as under doze.

**`.env` files** in an app's folder (`.env`, `.env.local`, `.env.example`)
are read even when `.gitignore`d, since they usually are. They are parsed
literally: no `$(…)`, no variable expansion beyond `${OTHER_KEY}` from the
same file. The secret rules ([graph.md](graph.md) §4.1) drop secret values
before anything is stored, and evidence chains show the file and key, never
a value.

**App names** are the folder's name (`orders` for `services/orders`). When
two clash, the path relative to the root with `/` turned into `-`
(`services-orders`, `tools-orders`). App ids are `app.<name>` as under doze.

**Optional config,** `.recall/recall.toml` at the root, for what discovery
can't know:

```toml
roots   = ["../billing", "../web"]        # other git repos that are part of this system
include = ["docs/**", "adr/**"]           # extra prose
exclude = ["fixtures/**"]                 # .gitignore is always respected
agents  = ["claude", "codex"]             # who may run generation here

[apps.orders]                             # only when discovery gets an app wrong
dir = "services/orders"
```

The same three keys as `recall {}`, plus `roots` and `apps`. Nothing else.

`recall.toml` is committed, so a repo someone cloned could use it to point
recall at their other files. So `roots` must each be a git repository, and
recall serves one only after the person accepts it once in `doze-recall
review`. Until then, `brief` says it was skipped. `include` and `exclude`
only ever apply inside the root and accepted roots.
With no file, recall works on what it finds, and `brief` says which apps it
found and how, so a person sees a wrong guess at once.

## 3. The join without declarations

The join ([graph.md](graph.md) §4.3) runs unchanged up to the point where it
matches a resolved value against declared resources. Standalone, there is
nothing declared. Values that resolve to a name become **inferred
resources**:

```
inferred resource = "inferred:" family ":" kind "." name
                    inferred:aws.sqs:queue.orders
                    inferred:kafka:topic.order.events
                    inferred:postgres:table.orders
```

The family comes from the catalogue row that matched (an SQS client, a Kafka
producer, a SQL statement), since there is no module to bind the client to.

| Value resolves to | Standalone edge |
|---|---|
| a constant name, and another app uses the same family, kind and name | `scoped` to the inferred resource, with "matched by name, no declaration" in its chain |
| a constant name only one app uses | `scoped` to the inferred resource; `impact` says no other app was found to use it |
| an env key whose value comes from a `.env` file | as a constant, with the file in the chain |
| an env key with no value anywhere | no resource; a `doubtful` edge between apps that read the same key name for the same family, and an unresolved-site fact |
| a URL or ARN | its name parsed out (the queue name from an SQS URL), then as a constant |
| `Unknown` | an unresolved-site fact, as under doze |

**Never `exact`.** A link between two apps through an inferred resource is
`scoped` at best. Two apps naming topic `orders` on two different brokers
would look linked, and only a declaration can say they aren't. Every
standalone answer's coverage line says resources were inferred, and how many
sites stayed unresolved because an env value was missing.

**Moving to doze.** When the root gains a `doze.hcl`, each inferred resource
maps to the declared one with the same family, kind and name, and each app
id maps by folder to the `doze.hcl` app with that `dir` (its name may
differ). Where exactly one matches, notes and decisions anchored to the inferred id are re-anchored on
the person's keypress in Review, never silently. The rest are listed there.

What standalone loses, plainly: names built from env values that live only
in deployment config, resources reached only through a config file recall
doesn't read (docker-compose, Terraform, Helm), and which of two brokers or
databases a client talks to. Reading docker-compose and Terraform is the
first thing to add after v1 if the eval's standalone arm (§9) shows it
matters.

## 4. How it runs

```
claude mcp add doze-recall -- doze-recall serve
```

`doze-recall serve` is a thin stdio shim. It finds the root (§2) and
connects to that root's **background process** over a Unix socket in
`~/.doze-recall/run/`, starting it if it isn't running. When the root
holds a `doze.hcl` with `recall {}`, the shim forwards to doze's recall
instance instead, so one registration works for both modes (a candidate
for P0.1's address question). The background
process holds the graph, the writer queue, the watcher and the language
servers. It exits 5 minutes after its last session closes, so with no agent
connected nothing runs, the same rule as under doze.

Why not one process per session: two agent sessions on one repo would each
index it and each write the graph. One background process per root keeps the
one-writer rule ([graph.md](graph.md) §5.1). Details:

- **Starting.** A lock file next to the socket (`flock`) decides which shim
  starts the process when two arrive at once. The lock dies with its holder,
  so after a crash the next shim finds the lock free, removes the stale
  socket and starts a new process.
- **Versions.** The shim sends its version first. A background process from
  an older release finishes its open requests and exits, and the shim starts
  the new one.
- **Overlapping roots.** One repo can be the root of one process and an
  accepted `roots` entry of another. Both may index it; the extraction cache
  is keyed by content hash and written by temp-file-and-rename, so they
  share it safely. Language servers take a machine-wide lock in
  `~/.doze-recall/run/`, which doze-hosted recall takes too, so "one server
  per machine" holds across processes.
- **Access.** `~/.doze-recall/run/` is mode 0700, and the background process
  checks the peer's uid on every connection.

Under doze, the background process is the built-in recall instance and the
proxy plays the shim's part. The shim isn't used.

Windows uses a named pipe in place of the socket and polls for changes. It
is unverified until doze has Windows CI (R93).

## 5. Staying current

The watcher (FSEvents through purego on macOS, inotify on Linux, git to
reconcile, polling elsewhere; [freshness.md](freshness.md)) lives in
doze-sdk as a package both use. Under doze, the daemon runs it and recall
subscribes. Standalone, the background process runs it and saves its event
ids under `~/.doze-recall/projects/<root hash>/`. Starting the background
process is "waking": catch-up, read-your-own-writes and partial answers work
as in [freshness.md](freshness.md).

## 6. Language servers

Recall runs its language servers ([graph.md](graph.md) §4.7) as its own
child processes in both modes (C2, revised). It downloads each from its
compiled-in `{url, sha256}` table, honours `DOZE_MIRROR`, unpacks gzipped
single binaries, and keeps them in `~/.doze-recall/servers/` (or doze's
cache when hosted). The rules are unchanged: one server at a time per machine, a
2 GB ceiling, the readiness rule and canary, stop 2 minutes after the last
doubtful site.

This replaces the earlier plan of hidden doze services behind
`doze __lspbridge`. Standalone needs the supervision anyway, and one code
path for both modes is simpler than two. What's given up is doze's journal
and log store for the servers; recall logs them itself and lists them in
`status.json`.

One thing does run the repo's toolchain: Go export data comes from
`go list -export`, which compiles packages, and for cgo packages that runs
the C compiler. It compiles but never runs the code, and Go limits the
flags cgo directives may pass. This is the same under doze.

## 7. Review, notes and generation in the terminal

The rule from [context.md](context.md) §1 holds: recall writes a committed
file only when a person presses a key. Standalone, the keys live in a few
commands, with the same letters as the dash's tabs:

| Command | Does | Keys |
|---|---|---|
| `doze-recall review` | everything waiting for a person, in one list: proposals, drift on decisions, decisions code now proves, inferred ids to re-anchor, `roots` to accept, stale and orphaned notes, short names to expand, note-check drafts | `a` approve · `r` reject · `e` edit · `n` note · `s` skip; on notes `c` checked · `t` re-anchor · `y` accept draft · `x` discard |
| `doze-recall generate` | runs generation's jobs with the person's agent CLI ([generation.md](generation.md)); results go to `review`. `--agent claude` picks the agent, remembered in `~/.doze-recall/config.toml` | none |
| `doze-recall status` | root, apps found and how, index state, servers, items to review | none |
| `doze-recall connect` | the line to paste for Claude Code, Codex, Cursor and others | none |
| `doze-recall check` | the CI check ([context.md](context.md) §8), exit 0 or 1 | none |

`review` is a plain list and prompt: one item at a time, its evidence, the
keys it takes. It has its own small renderer in doze-recall, since the
dash's lives in doze. No map, no query box, no panes.

**Files**:

| What | Standalone | Committed |
|---|---|---|
| `recall.toml`; notes about the repo's code | `.recall/` at the root | yes |
| Notes across apps, `glossary.md`, `topology.yaml` | `.recall/stack/` at the root | yes |
| Notes about a sibling root's code | that root's `.recall/` | yes |
| `graph.db`, `proposals.json`, `status.json`, the query log, event ids | `~/.doze-recall/projects/<root hash>/` | no |
| Extraction caches, language servers | `~/.doze-recall/cache/`, `~/.doze-recall/servers/` | no |

Stack files live in `.recall/stack/` so that a doze stack containing this
repo doesn't load them as notes about the repo's code. A doze stack whose
`doze.hcl` sits at this root reads `.recall/stack/` as it reads `recall/`,
so nothing has to move when a person adds doze.

## 8. Install and release

- **Repository:** `doze-dev/doze-recall`, public, its own Go module.
- **Install:** `install.sh` from the repo, the same shape as doze-kafka's:
  the prebuilt binary for the OS and architecture, its SHA-256 checked,
  installed to `~/.doze-recall/bin`, and the line to add it to `PATH`
  printed, the same as doze-kafka's. macOS and Linux, amd64 and arm64.
- **Releases:** GitHub Releases through goreleaser, with a changelog.
- **Under doze:** doze pins an exact `doze-recall` version and SHA-256 in its
  build, downloads it on the first `recall {}`, and runs that one. A person
  never manages two versions. `stack.json` carries a contract version;
  `doze-recall` accepts its own contract and the one before, so a doze
  release can lag a recall release by one.

## 9. In the eval

The pilot (E1) runs hosted by doze: it is recall's best case, and the thing
to prove first. E2 adds **A5, standalone**: A2's setup with `doze.hcl` removed, recall
started by the agent, and a `.env` file in each app generated from the
resolved config, as a team without doze would have ([eval.md](eval.md) §4).
No backend runs in any arm, so the daemon adds nothing else to A2, and the
difference is the declarations.

- **A5 − A0** is what standalone recall is worth.
- **A2 − A5** is what `doze.hcl` adds on top.

If A5 barely beats A0, that says recall's value lies in the declarations,
and standalone is pitched as the way into doze rather than as a product on
its own.

## 10. Code

The repository's layout ([graph.md](graph.md) §11) gains:

```
internal/
  host/       the two hosts behind one interface: doze (stack.json, daemon feed)
              and standalone (discovery, .env, recall's own feed)
  discover/   also: app discovery from manifests, app names, recall.toml
  join/       also: inferred resources and their ids
  bgproc/     the background process: socket, lock, idle exit
  shim/       `serve` over stdio, connecting to or starting the background process
  cli/        review, generate, status, connect; its own list-and-prompt renderer
install.sh
```

## Open

- Whether `roots` should also accept a list of git URLs to clone, for
  systems spread over many repositories. Leaning no: the person checks them
  out.
