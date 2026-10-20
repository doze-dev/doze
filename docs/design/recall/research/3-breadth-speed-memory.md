# Recall research 3: breadth, speed and memory

Question: how does recall get broad language coverage, fast incremental indexing
and bounded memory, in pure Go, with nothing to install and no noticeable drag on
the machine?

Date: 2026-10-20. Everything marked **measured** was run on this machine for this
report. Everything else is cited from the source named, and I say where a claim is
the project's own.

## Short answer

1. **Go precision needs no new machinery, but which way you load matters by 10x.**
   Loading the workspace with `go/packages` in "full" mode (deps type-checked from
   source) took **2.2 GB RSS** on doze-aws with tests. Type-checking one workspace
   package at a time against compiler export data took **86–114 MB RSS** and
   **1.0–1.6 s**, and produced the same 577k reference edges. This is the gopls
   v0.12 design, and recall should copy it.
2. **gotreesitter is the only credible pure-Go syntax layer, and it is slower and
   heavier than C.** It ships 206 grammars and is very active (v0.56.0, 2026-10-13).
   Measured against C tree-sitter on the same files, single-threaded, it is **2.1–2.2x
   slower** on TS/TSX, **4.1–4.5x** on JS and doze-aws Go, and **~9x** on
   Kubernetes Go. It needs **~100–150 MB of transient heap per parse worker**, and
   it is a memory disaster if trees are kept: 8 MB of source held **8.5 GB**. It
   works if recall parses, extracts and releases one file at a time, uses 2–4
   workers and caps file size. Go source should never go through it; `go/types`
   covers Go.
3. **Practical invalidation is per file for syntax facts and per package (or
   module) for resolved facts, with an early cutoff.** Re-extract the changed file.
   Re-resolve only if its exported surface hash changed, and then only its direct
   importers, continuing outward only while the surface keeps changing. gopls,
   rust-analyzer/salsa (durability plus early cutoff), Glean (per-file units),
   stack-graphs (per-file partial graphs) and CodeGraph (extract, then resolve) all
   converge on this shape.
4. **Change detection: FSEvents works without cgo, and it replays history.** A
   60-line purego prototype built with `CGO_ENABLED=0` delivered live file-level
   events in about 50 ms. It also **replayed every change made while it was not
   running, from a saved event id, in 13 ms**. That covers "edits while recall
   slept" without polling or git. git with the untracked cache stays a cheap
   fallback (40 ms for `git status` on Kubernetes' 31k files), and so does polling
   (43 ms to lstat 31k files).

---

## Machine and corpora

- Apple M5 Max, 18 cores, 36 GiB RAM, macOS (Darwin 27.0), Go 1.27.2,
  git 2.54.0 (Apple Git-157), APFS.
- Tools lived in the scratchpad (`scratchpad/bench/*`), and outputs went to the
  scratchpad. The repos were not modified: `git status` was unchanged
  before and after. doze was at `142af83` and doze-aws at `383d06a`, the local
  checkouts. I did not `git fetch`, so as not to touch the repos.
- Corpora:

| Corpus | Files | LOC | Bytes |
|---|---:|---:|---:|
| doze (Go, workspace packages) | 226 .go | 45.6k | |
| doze-aws tracked .go | 1,051 | 221.7k | 8.0 MB |
| TS (ai-interview + flint, `*.ts`) | 112 | 31.9k | 1.0 MB |
| TSX (same repos, `*.tsx`) | 246 | 52.5k | 1.9 MB |
| JS (doze-aws/e2e/node_modules, <2 MB each) | 2,322 | 222.8k | 12.3 MB |
| Kubernetes `ae4a6e5e` (shallow clone in scratchpad), non-vendor .go | 13,596 | 3.99M | 141.9 MB |
| Kubernetes main module only (no vendor, no staging) | 5,695 (incl. tests) | 2.02M (0.96M non-test) | |

Note that doze-aws has 12,274 .go files under `.claude/` (agent worktrees). Any
recall walker must skip `.claude/` and other worktree copies, or a single repo
counts as a dozen.

---

## 1. Syntax layer in pure Go

### gotreesitter (github.com/odvcencio/gotreesitter)

Facts from the repo, fetched 2026-10-20:

- Pure-Go reimplementation of the tree-sitter runtime, MIT licensed. Created
  2026-02-20, latest v0.56.0 (2026-10-13), pushed today. 576 stars, 45 open
  issues.
- **206 grammars** in the registry. 119 have hand-written Go external scanners.
  The parse tables are extracted from upstream `parser.c` by `ts2go` and embedded
  as compressed blobs, decoded on first use. Build tags `grammar_subset` and
  `grammar_subset_<lang>` cut the set. Queries are supported (full S-expression
  language plus predicates), as are incremental reparse and typed query codegen.
- The project's own speed claims, in BENCH.md:
  - Sealed "v9" epoch: on four real Go files, public `Parser.Parse` is
    **4.815x slower than C** (geomean), and the "compact" route 3.986x.
  - Fleet scoreboard of 204 languages, largest file per language: **median
    ~3x C**. 10 languages are at or under 1x, 64 are 1–2x, 29 are 2–3x, 101 are
    over 3x, and the worst is up to ~650x on a small DSL.
  - The project withdrew an earlier 1.895x headline after its own audit. The
    honesty is good; the number is not.
- The project's own memory claims: the node header is 104 bytes, and a 3.4 MB JS
  file (poppler.js) parses at 1.7 GB max RSS and retains 410 MB of heap after
  arena compaction.
- Correctness approach: deep tree parity against a pinned C oracle (tree-sitter
  v0.25.1), golden S-expressions, top-50 smoke tests and an editor-latency CI
  gate. Their admission notes say some fixtures (Go, C#, PowerShell) first
  failed fresh parity with C.
- Launch-era HN criticism: AI-written code, "not truly a complete port", and
  doubts about long-term maintenance. Eight months on, the project is still
  shipping weekly.

**Measured**: parse speed against C tree-sitter on the same corpus. Both runs
count every node and check `HasError`.

Binaries:
- `bench/gts`: gotreesitter v0.56.0, pure Go.
- `bench/cts`: `github.com/tree-sitter/go-tree-sitter v0.25.0` (cgo), with
  tree-sitter-go v0.25.0, -javascript v0.25.0 and -typescript v0.23.2.

Single-thread totals, summed per file. The command was
`gts -per -lang L -list corpus/L.txt`, and the same for `cts`.

| Corpus | gotreesitter | C | Ratio (sum) | Per-file ratio p50 / p90 / max |
|---|---:|---:|---:|---|
| doze-aws Go, 8.0 MB | 2,945 ms (2.7 MB/s) | 658 ms (12.2 MB/s) | **4.48x** | 3.43 / 3.88 / 35.5 |
| JS, 12.3 MB | 4,382 ms (2.8 MB/s) | 1,064 ms (11.6 MB/s) | **4.12x** | 3.15 / 8.80 / 48 |
| TSX, 1.9 MB | 226 ms (8.6 MB/s) | 109 ms (17.8 MB/s) | **2.08x** | 2.07 / 2.25 / 5.6 |
| TS, 1.0 MB | 105 ms (9.7 MB/s) | 47 ms (21.6 MB/s) | **2.23x** | 2.25 / 2.53 / 8.0 |
| Kubernetes Go, 135 MB, 13,593 files under 500 KB | **76.2 s** (1.77 MB/s) | 8.55 s (15.8 MB/s) | **8.9x** | 10.3 / 41.5 / 776 (p99 83) |

Per LOC, per core: gotreesitter takes about **1.3 s per 100k LOC** on doze-aws
Go and about **1.9 s per 100k LOC** on Kubernetes Go. On TS/TSX it takes about
**0.35–0.45 s per 100k LOC**. C takes 0.2–0.3 s per 100k LOC.

Parallel runs, whole process, with `/usr/bin/time -l` max RSS:

| Run | Wall | Max RSS |
|---|---:|---:|
| gts Go doze-aws, j=1 | 2.83 s | 136 MB |
| gts Go doze-aws, j=4 | 0.79 s | 322 MB |
| gts Go doze-aws, j=8 | 0.46 s | 571 MB |
| gts Go doze-aws, j=4, `GOMEMLIMIT=200MiB` | 1.16 s | 259 MB |
| gts Go doze-aws, j=2, `GOMEMLIMIT=200MiB` | 1.48 s | 202 MB |
| C Go doze-aws, j=1 / j=8 | 1.03 s / 0.18 s | 28 MB / 34 MB |
| gts JS (all, incl. 1.3 MB `axe.js`), j=8 | 2.58 s | **2.06 GB** |
| gts JS, files under 200 KB (2,309 files, 6.4 MB), j=1 / j=4 / j=8 | 1.23 / 0.43 / 0.39 s | 155 / 276 / 423 MB |
| gts JS under 200 KB, j=4, `GOMEMLIMIT=150MiB` | 0.62 s | 184 MB |
| C JS under 200 KB, j=1 / j=4 / j=8 | 0.76 / 0.20 / 0.13 s | 33 / 40 / 45 MB |
| gts Kubernetes Go, serial | 76 s | **1.38 GB** |
| gts Kubernetes Go, j=8 | 37.9 s | **6.55 GB** |
| gts Kubernetes Go, j=8, `GOMEMLIMIT=300MiB` | 93–100 s | 2.9–3.1 GB, with the GC thrashing |
| C Kubernetes Go, j=8 | 2.67 s | 366 MB |

Other measurements:

- **Retained trees**: holding all 1,051 doze-aws Go trees used **8.5 GB of heap
  for 8 MB of source**, about 1,100 bytes of heap per source byte (`gts/keep`).
  Recall must never keep trees. Parse, extract the facts, `Release()`.
- **Grammar load**: 3–12 ms per grammar on first use. Heap is about 2.4 MB for
  Go, and 9.3 MB for Go, TS, TSX, JS and Python together.
- **Binary size**: 34.9 MB with all 206 grammars. 15.2 MB with
  `-tags 'grammar_subset grammar_subset_go grammar_subset_typescript grammar_subset_tsx grammar_subset_javascript'`.
  The C build for the same 4 languages was 6.8 MB.
- **Parity with C**, judged by node count and error flag per file. Grammar
  commits differ between the two builds, so this is agreement, not ground truth:
  - doze-aws Go: 11 of 1,051 files differ, by 1–30 nodes. 1 file got a false
    error from gotreesitter.
  - TS: 2 of 112 differ. TSX: 4 of 246 differ; C reported an error on those 4
    and gotreesitter did not, which looks like the older C typescript grammar.
  - JS: 1 of 2,322 differs. On `axe.min.js` (580 KB minified), gotreesitter
    gave up: 61k nodes and an error, against 334k nodes and no error from C,
    and 317 ms against 78 ms.
  - Kubernetes Go: 856 of 13,593 files (6.3%) differ in node count. 28 files
    have errors only in gotreesitter, 156 only in C (the C grammar is older), and
    3 in both.
- **Slow outliers**: the slowest Kubernetes files under gotreesitter take 2–4 s
  each (`default_preemption_test.go`, 215 KB: 4.2 s). Many of them are files
  where gotreesitter reports an error, so error recovery is the slow path. 27
  files over 1 s account for 47 s of the 76 s.
- **GOMEMLIMIT**: a tight limit is not a free memory cap. Per-parse garbage is
  large, so a limit near the working set makes the GC thrash: 2.5–3.3x slower on
  JS and Kubernetes. Bounding concurrency (2–4 workers) bounds memory better than
  GOMEMLIMIT. Use the limit only as a backstop well above the expected peak.

### Alternatives

| Option | Pure Go | Languages | Speed | Memory | Notes |
|---|---|---|---|---|---|
| gotreesitter | yes | 206 | 2–9x slower than C (measured) | ~100–150 MB transient per worker; trees huge | active, single maintainer, AI-assisted; pin a version and gate it with our own parity corpus |
| `tree-sitter/go-tree-sitter` (official, cgo) | no | any upstream grammar, one Go module each | baseline | ~5 MB per worker (measured) | needs cgo and a C toolchain to build doze; breaks `CGO_ENABLED=0` cross-builds |
| `smacker/go-tree-sitter` (cgo) | no | ~30 bundled | ~ C | ~ C | older community binding, grammar versions lag; same cgo cost; the official binding supersedes it |
| tree-sitter compiled to WASM, run with wazero | yes | any grammar built with wasi-sdk or emscripten | not measured; wazero AOT is typically ~1.5–3x native for this kind of code, plus a Go↔WASM call per node access, unless extraction runs inside WASM | WASM linear memory per instance (tens of MB) | only a spike exists (`vista-cloud-dev/m-parse`: a 415 KB embedded module, nodes boxed behind i32 handles, a mutex per parser, no benchmarks). Recall would need its own build pipeline per grammar, and node walking across the boundary is the cost. The upside: upstream grammars, unmodified. |
| ctags-style regex | yes | many | fast | tiny | no tree, poor precision; fine only as a last-resort "symbols only" tier |

**Verdict.** Use gotreesitter as the syntax layer for non-Go languages, but:

- Build only the grammars recall uses, behind the `grammar_subset` tags. Start
  with TS, TSX, JS and Python, then add more by demand. Each grammar costs binary
  size and a parity test.
- Never keep trees. Extract to compact facts in the same goroutine and release.
- Cap file size (skip over 256 KB, or minified files with mean line length over
  500), skip generated files and `node_modules`, and give each parse a time
  budget, recording "partial" for files that hit it.
- Run 2 parse workers by default (4 while the person waits on a first index).
  That is about 200–300 MB peak, short-lived.
- Keep a parity corpus of our own (100 files per language, tree digests against
  C, run in CI with cgo on the CI machine only). If gotreesitter regresses or is
  abandoned, the WASM route is the fallback, because it keeps the same grammars.

Go never goes through tree-sitter: `go/parser` plus `go/types` are in the
standard library, exact, and faster.

---

## 2. Go precision: go/packages and go/types (measured)

Program: `scratchpad/bench/pkgload/main.go`, using x/tools v0.52.0. The modes:

- **full**: `NeedName|NeedFiles|NeedImports|NeedDeps|NeedSyntax|NeedTypes|NeedTypesInfo|NeedModule`.
  Every package, the standard library included, is parsed and type-checked from
  source.
- **export**: the same without `NeedDeps`. Workspace packages come from source,
  dependencies from compiler export data (`go list -export`). This is the gopls
  approach.
- **trim**: full, plus a `ParseFile` hook that drops function bodies outside the
  workspace.
- **stream** (`scratchpad/bench/stream`): `go list -deps -export` metadata only,
  then a hand-rolled loop. Each workspace package is parsed, type-checked with
  `go/types` against `gcexportdata`, its edges are extracted, and its syntax and
  `types.Info` are dropped before the next package. `-share=false` also drops the
  imported-types cache per package.
- `-extract` walks every `FuncDecl` and records an edge (from func, to used
  object) for each `Uses` entry. Then the packages are dropped and the heap is
  measured.

Each mode was run twice and the warm (second) run is reported. "Heap" is
`HeapAlloc` after GC with everything loaded. "RSS" is the process max RSS from
`/usr/bin/time -l`. The `go list` child processes are separate.

| Repo | Mode | Tests | Wall | Heap loaded | Max RSS | Packages (roots / all) | Root files | Edges |
|---|---|---|---:|---:|---:|---|---:|---:|
| doze | names only | no | 0.10 s | 1 MB | 43 MB | 33 / 467 | | |
| doze | full | no | 0.58 s | 573 MB | 790 MB | 33 / 467 | 142 | 54.5k |
| doze | full | yes | 0.62 s | 612 MB | 875 MB | 81 / 526 | 349 | 125k |
| doze | export | no | 0.22 s | 42 MB | 78 MB | 33 / 467 | 142 | 54.5k |
| doze | export | yes | 0.37 s | 70 MB | 130 MB | 81 / 526 | 349 | 125k |
| doze | trim | yes | 0.37 s | 307 MB | 462 MB | | | 125k |
| doze | stream, shared imports | yes | 0.22 s list + 0.23 s check | 23 MB max between packages | **59 MB** | 81 | 349 | 125k |
| doze-aws | full | no | 0.91 s | 459 MB | 683 MB | 66 / 316 | 523 | 208k |
| doze-aws | full | yes | 1.49 s | 1,451 MB | **2,214 MB** | 220 / 705 | 1,612 | 577k |
| doze-aws | export | no | 0.61 s | 127 MB | 213 MB | 66 / 316 | 523 | 208k |
| doze-aws | export | yes | 0.94 s | 324 MB | 521 MB | 220 / 705 | 1,612 | 577k |
| doze-aws | trim | yes | 1.03 s | 696 MB | 1,220 MB | | | 576k |
| doze-aws | stream, shared imports | yes | 0.49 s list + 1.04 s check | 74 MB | **114 MB** | 220 | 1,612 | 577k |
| doze-aws | stream, unshared | yes | 0.63 s list + 1.57 s check | 35 MB | **86 MB** | 220 | 1,612 | 577k |
| Kubernetes main module | export, all at once | no | 2.6 s | 1,057 MB | 1,493 MB | 1,463 / 3,578 | 3,414 | 1.29M |
| Kubernetes | stream, shared | no | 32.7 s list (**cold build cache**) + 10.6 s check | 1,444 MB (the imported cache grows to 2,398 packages) | 1,577 MB | 1,463 | | 1.29M |
| Kubernetes | stream, unshared | no | 2.0 s list (warm) + 22.8 s check | **278 MB** | **430 MB** | 1,463 | | 1.29M |

**Cold build cache.** Export data comes from `go list -export`, which compiles
anything not already in `GOCACHE`. Measured with an empty `GOCACHE`:

| Repo | export mode, cold | full mode, cold |
|---|---|---|
| doze (tests) | 4.6 s wall, 35 s CPU, 295 MB RSS | 0.81 s |
| doze-aws (tests) | 10.6 s wall, 111 s CPU, 659 MB RSS | 1.6 s, 2.3 GB |
| Kubernetes | 32.7 s for `go list` | |

Developers who build their code have a warm cache. A fresh clone, or a
dependency bump, pays this once, mostly in parallel compiler processes.

**Re-checking one package** after a save, in the stream loop (parse plus
type-check against export data, imports cached):

- doze: median under 1 ms. The largest, `internal/dash` (42 files with tests),
  takes 16 ms.
- doze-aws: median 1 ms. The largest are `internal/console` (104 files, 36 ms)
  and the root test package (46 files, 42 ms).
- Kubernetes, unshared, so the cost includes reading the imports' export data:
  median 4 ms, worst 126 ms (`kube-controller-manager/app`).
- Fan-in decides what a signature change costs. doze-aws workspace packages have
  a median of 1 direct importer, p90 12, max 31. The worst case is about 31
  re-checks at a few ms each, roughly 100 ms serial.
- `gcexportdata.Write` can produce export data for a re-checked package
  in-process, so dependents can be re-checked without running the `go` command
  again.

**Extracted graph size**: 577k edges and 132k symbols for doze-aws come to
**17.4 MB** of heap after dropping the packages, with names as Go strings. 1.29M
edges for Kubernetes come to 37.7 MB.

### How gopls keeps memory down (go.dev/blog/gopls-scalability, v0.12, 2023)

- **Separate compilation.** One package at a time. Each package's results go to
  a file cache shared across processes, holding export data, a cross-reference
  index, method sets and analysis facts. Dependencies are consumed as the
  compiler's compact export summaries, never as typed syntax.
- **Memory is proportional to the open packages and their direct imports**, not
  to the whole program. The blog reports about 75% less memory and startup time
  across 28 popular repos, with savings growing as repos get larger. Typed syntax
  trees are "typically 30x larger than the source text".
- **Invalidation.** An edit re-checks its own package. If that package's export
  summary is unchanged, nothing else is recomputed. If it changed, direct
  importers are re-checked, continuing outward only while summaries keep
  changing. A syntactic reference graph prunes further.
- **References** are answered by loading per-package xref indexes, not a global
  in-memory graph.

**What this means for recall.** Recall's Go indexer is the gopls loop minus the
editor:

1. `go list -deps -export -json` per module, cached by `go.sum` hash.
2. Stream the workspace packages in dependency order through a bounded worker
   pool. Parse, type-check, extract, drop.
3. Keep an LRU of imported `*types.Package` (bounded by count or bytes), not the
   unbounded shared map. The measured spread is 86 MB to 1.6 GB depending on this
   one choice.
4. Persist per-package facts and an export-data hash, so a wake or a save
   re-checks only the changed package and, when its surface hash moved, its
   importers.

Never load all packages with `NeedDeps|NeedSyntax|NeedTypesInfo`: that was
2.2 GB for doze-aws alone.

---

## 3. Incremental indexing designs

| System | Unit of invalidation | How dependents are handled | Lesson for recall |
|---|---|---|---|
| **gopls v0.12+** | package (files trigger it) | re-check importers only if the export summary hash changed; syntactic reachability pruning | early cutoff on a surface hash is the key trick |
| **rust-analyzer / salsa** | per query, inputs per file | lazy red-green validation. Each query records "last verified" and "last changed", and an unchanged result stops propagation (early cutoff). **Durability**: library and std inputs are "durable", so a keystroke in user code skips validating them, which saved ~300 ms per edit. Their item tree, which ignores function bodies, isolates body edits from signatures (from their architecture docs, not this post). | split facts into a signature layer and a body layer; dependency facts are durable (keyed by `module@version`) and never revalidated on a save |
| **Glean (Meta)** | unit, a string owning facts; per file or per module is recommended | stacked DBs: the incremental DB hides the excluded units' facts in the base through ownership sets (OR/AND); derived facts get the AND of their sources; ownership propagation is O(facts) | per-file fact ownership makes "delete this file's facts, insert new" clean. Derived facts (e.g. a resolved call edge) must record both owners so either side's change invalidates them. Their open gap: derived facts on stacked DBs. |
| **stack-graphs (GitHub)** | file | each file becomes an isolated partial graph built from tree-sitter rules; imports stay unresolved; **resolution happens at query time** by path-finding over the merged per-file graphs. Rationale: avoid re-analysis quadratic in the number of changed files. The github/stack-graphs repo is archived (GitHub API: `archived: true`, last push 2025-09-09). | per-file extraction is cheap and exactly incremental. Pushing all resolution to query time costs latency; recall should resolve at index time but keep the per-file isolation of the extraction step. |
| **SCIP (Sourcegraph)** | whole index per upload; documents inside are per file | the indexer reruns over the project (in CI); no in-process incrementality. **Measured** scip-go: doze 0.43 s / 166 MB; doze-aws 1.2 s / 663 MB / 37 MB index; Kubernetes main module 33 s wall (401 s CPU) / **4.4 GB RSS** / 324 MB index | fine as an exchange format, wrong as a live engine: batch, heavy, whole-project |
| **Kythe** | compilation unit, captured from the build | re-index changed units in a batch pipeline; serving tables are rebuilt offline | build-integrated, needs a build extractor per language. Not suitable for "no installs". |
| **CodeGraph (colbymchenry)** | file | phases are extraction (tree-sitter per file), then storage (SQLite + FTS5), then **resolution** (calls to definitions, imports to files, inheritance, framework patterns). The watcher debounces (2 s window; 300 ms after a lone save) and re-syncs only what changed. Self-reported: ~0.3 s per save on a 4,400-file project, ~0.4 s on the 27k-file Swift repo; a one-file edit re-syncs in ~4 s on the Swift repo in another passage. | the two-phase extract/resolve split is the practical structure. Resolution of a changed file's references, plus the references *into* its changed symbols, is the incremental unit. |
| **codebase-memory-mcp** | repo (git-based change detection by a background poll thread) | re-index on change; RAM-first full index | fast full index (pure C), but re-index cost grows with the repo; CodeGraph claims 2–7x faster per change against "the fastest competing indexer" |

**Practical granularity for a file save:**

1. **File: syntax facts** (definitions, references by name, imports, routes,
   queue and topic literals). Keyed by content hash. Re-extracted in 1–40 ms
   (gotreesitter: one 41 KB Go file in 46 ms worst case; TS files are about
   1 ms).
2. **Surface hash per file** (the exported definitions and their signatures). If
   it is unchanged, stop. Only this file's outgoing references need
   re-resolving, which is cheap.
3. **Surface changed:** re-resolve the incoming references to the changed or
   removed symbols, found through the reverse index of name to referencing
   files. For Go, re-check direct importer packages, continuing while their
   export hash changes. Measured: a median of 1 importer, p90 12, max 31 in
   doze-aws; tens to low hundreds of ms in the worst case.
4. **Stack-level joins** (code to `doze.hcl` resources: queue names, routes,
   topics) are derived facts owned by AND(code file, hcl block). Re-derive when
   either side changes. This is Glean's model, and cheap at stack scale.

Per-symbol or per-function units are not worth it. Glean warns that finer units
blow up ownership sets, and a save rarely touches fewer bytes than a file costs
to re-parse.

---

## 4. Typical indexing speeds and graph memory

| Tool | Work | Reported or measured | Per 100k LOC | Memory | Source |
|---|---|---|---|---|---|
| codebase-memory-mcp (pure C, tree-sitter, 162 languages) | Linux kernel 28M LOC, 75k files, giving 4.81M nodes and 7.72M edges | 3 min full ("fast" mode 1 m 12 s), M3 Pro | ~0.64 s | "RAM-first"; no number published | README, self-reported |
| codebase-memory-mcp | Django | ~6 s, 49k nodes, 196k edges | | | README |
| CodeGraph (Rust kernel plus a portable engine) | Swift compiler repo, 27k files | ~100 s fresh; one-file edit ~4 s | ~3.7 ms per file | Linux kernel (70k files, 2M symbols, 6.4M edges) "under 12 min" on a 2-core / 6 GB VPS | README, self-reported |
| Zoekt (Go, trigram text index) | Kubernetes, 277 MB of tracked content | **4.3 s wall, 951 MB RSS, 660 MB index** (measured) | ~0.08 s (text only, no structure) | index ~3x the corpus, RAM ~1.2x the corpus (design doc) | measured plus doc/design.md |
| scip-go | doze-aws 222k LOC incl. tests / Kubernetes main module 2.0M LOC | 1.2 s / 33 s wall (measured) | 0.5–1.6 s wall; ~20 s CPU per 100k on Kubernetes | 663 MB / **4.4 GB** RSS | measured |
| Recall Go path (stream, unshared) | doze-aws / Kubernetes | 2.2 s / 24.8 s, serial check (measured) | ~1 s / ~2.4 s serial; ÷ workers | 86 MB / 430 MB RSS | measured |
| gotreesitter, extraction excluded | doze-aws Go / Kubernetes Go / TS | 2.9 s / 76 s / 0.1 s serial (measured) | 1.3 / 1.9 / 0.35 s per core | ~100–150 MB per worker; 1.4 GB serial on Kubernetes | measured |
| Serena | LSP-backed (40+ languages through language servers) | no indexing numbers published; symbol cost is the language server's own (tsserver, rust-analyzer: GBs on big repos) | n/a | per server | README |

**Graph memory per million edges** (measured, `bench/graphmem`):

- Flat CSR layout: a sorted `[]Edge{from, to, file, line uint32; kind uint8}`
  (20 B) plus a reverse permutation (4 B) plus an offset table. With 200k
  symbol names of ~60 bytes: **38.9 MB per 1M edges**, about 41 B per edge with
  names.
- Naive `map[uint32][]Edge` plus a reverse map: 50 MB per 1M edges, 53 B per
  edge, excluding names.
- Real extraction: doze-aws gave 577k edges and 132k symbols in 17.4 MB.
- So a stack of a few services (1–3M edges) fits in **40–120 MB in memory**, or
  can be served from an on-disk store with a small page cache. The Linux kernel
  at 7.7M edges would be ~300 MB in flat form, which recall should not hold
  resident.

---

## 5. Change detection at scale

### git (measured)

| Repo, tracked files | `git status --porcelain=v2` | with `core.untrackedCache=true` | `-uno` | with fsmonitor plus untracked cache | `git diff --name-only HEAD` |
|---|---:|---:|---:|---:|---:|
| doze-aws (1,814 tracked; 136k files on disk incl. `.claude` and node_modules) | 10–20 ms | 10 ms | 10 ms | | |
| Kubernetes shallow clone (31,443 tracked) | **140 ms** | **40 ms** | 30 ms | **20 ms** | 30 ms |

- These are clean trees, warm OS cache, M5 Max. The `-c core.untrackedCache=true`
  and fsmonitor runs were made only on the scratch Kubernetes clone, because they
  write index extensions. The builtin `git fsmonitor--daemon` was started for
  that test and then stopped.
- The GitHub blog's figures (4-core Windows laptop): Chromium (393k files)
  17.6 s, falling to 0.83 s with FSMonitor plus untracked cache; 1M files 41 s to
  0.59 s; 2M files 85 s to 0.75 s. The untracked cache alone is about 2x on the
  untracked scan; with FSMonitor it is about 10x.
- Caveats from the same post: the first status after the daemon starts is slow;
  dropped events force a full rescan; build artifacts next to sources change
  directory mtimes and defeat the untracked cache.
- Recall should **not** turn on `core.untrackedCache` or fsmonitor in people's
  repos. That writes their config and index. Use what is already configured,
  and run `git status` at most on wake and after a debounce.

### Polling (measured, `bench/statw`, Kubernetes, 31,443 files)

- `lstat` of every tracked file: **43 ms serial (1.4 µs per file)**, 78 ms the
  first time, 20 ms with 8 goroutines.
- `filepath.WalkDir` without stat over 37.5k entries: 104–127 ms.
- At doze's `internal/watch` default of 1 s, polling a Kubernetes-sized tree
  costs about 4% of a core continuously, plus directory walks to find new files.
  It is acceptable as a fallback for small app folders. As the main feed for a
  stack it is wasteful, and it misses nothing only at 1 s granularity.

### FSEvents without cgo (measured prototype, `bench/fsev/main.go`)

- Built with `CGO_ENABLED=0` (2.8 MB binary) using `github.com/ebitengine/purego`
  v0.11.2.
- It `Dlopen`s CoreServices, CoreFoundation and libSystem and calls
  `FSEventStreamCreate` with a Go callback from `purego.NewCallback`. Flags are
  `kFSEventStreamCreateFlagFileEvents|NoDefer` with 50 ms latency, and the
  stream runs on a `dispatch_queue_create` queue. Then
  `FSEventStreamSetDispatchQueue` and `FSEventStreamStart`.
- **Live**: one stream on the 31k-file Kubernetes root. Setup took 0.3–1.1 ms.
  Write, touch and rm events arrived as file-level paths with flags about 50 ms
  after the writes. The whole process was **13.9 MB RSS**, nearly all Go runtime.
  There is one stream per root and no descriptor per file, unlike kqueue.
- **Replay while not running**: I recorded `FSEventsGetCurrentEventId`, stopped
  the watcher, created, touched, mkdir'd and removed files, then started a stream
  with `sinceWhen = <saved id>`. All 4 changes were replayed with exact
  file-level paths, then `HistoryDone`, **13 ms after start**. So recall (or the
  daemon) can persist one event id per root and, on wake, ask the OS what changed
  while it slept: no re-stat, no git, and it works for folders outside git.
- **What remains to handle**:
  - `MustScanSubDirs`, `UserDropped`, `KernelDropped`, `EventIdsWrapped` and
    `RootChanged` flags mean "rescan this subtree" (fall back to git or a stat
    walk).
  - Event ids are per volume. Store `FSEventsCopyUUIDForDevice`, and when the
    volume UUID changes the history is gone, so do a full catch-up.
  - Network volumes don't deliver events.
  - The callback runs on a dispatch thread, so keep it to a non-blocking
    hand-off.
  - purego's callback count is limited, which is fine for one callback.
- No published pure-Go FSEvents library was found. `fsnotify/fsevents` and
  `rjeczalik/notify` both use cgo, and `fsnotify` on macOS uses kqueue. Doze
  would own about 150 lines of purego bindings.
- **Linux**: inotify through `golang.org/x/sys/unix` is pure Go (fsnotify already
  does this). It needs one watch per directory, has no history, and on overflow
  (`IN_Q_OVERFLOW`) needs a rescan. So on wake, use git diff plus a stat walk of
  dirty candidates. On Windows, `ReadDirectoryChangesW` is pure Go through
  x/sys/windows.

### What other tools do

- **watchman**: FSEvents on macOS, inotify on Linux. Its clock ids (`c:123:234`)
  give race-free "since" queries, and named cursors exist. It must be running to
  see changes; a restart reports a "fresh instance" and the client rescans.
- **git fsmonitor--daemon** (2.37+): builtin on macOS and Windows, with an
  in-memory time-sorted queue and IPC. It resyncs (a full scan) on dropped
  events.
- **Bazel `--watchfs`**: "tries to use the operating system's file watch service
  for local changes instead of scanning every file" on Linux and macOS. It is off
  by default, undefined on network file systems, and a no-op on Windows without
  an experimental flag.
- **Turborepo**: its docs now say "the daemon is no longer used for
  `turbo run`", and the flags are deprecated. Only `turbo watch` and its LSP
  still use it. A watcher daemon was not worth its complexity for one-shot runs;
  hashing on demand was enough.
- **CodeGraph**: native OS file events, a 2 s debounce, source files only.
  **codebase-memory-mcp**: a git-based background poll thread, git projects only
  by default, and watching follows open MCP sessions.

---

## Implications for recall

### Budgets (proposed, to be checked against `test/bench/budget.json`)

| State | Budget | Basis |
|---|---|---|
| Asleep | 0 MB (no process); the daemon's feed holds one FSEvents stream per watched root at about 0 extra MB, plus a persisted event id | measured 13.9 MB for a whole standalone process; inside the daemon the runtime is already paid |
| Awake, serving, stack ≤ 1M edges | **≤ 80 MB RSS** | flat graph ~40 MB per 1M edges (measured) plus the Go runtime and caches |
| Awake, serving, stack ≤ 3M edges | ≤ 150 MB, or serve from disk with a bounded page cache | |
| Indexing peak, Go | **≤ 300 MB RSS** at 4 workers with a bounded import LRU | stream/unshared: 86 MB doze-aws, 430 MB Kubernetes serial; the LRU sits between those and shared (1.6 GB) |
| Indexing peak, tree-sitter languages | **≤ 300 MB** at 2 workers, 256 KB file cap | measured 155–276 MB at j=1–4 on JS under 200 KB |
| Save to answer-ready (one file) | **≤ 100 ms p95** for syntax languages, ≤ 200 ms p95 for Go including importer re-checks | measured 1–46 ms per file to parse; Go package re-check median 1–4 ms, worst 126 ms; fan-in p90 12 |
| Wake catch-up, nothing changed | ≤ 50 ms | FSEvents replay 13 ms; `git status` 10–40 ms |
| First full index, ~250k LOC stack, warm Go build cache | **≤ 5 s wall, ≤ 4 cores** | Go stream ~2.2 s serial for doze-aws; TS at ~0.4 s per 100k LOC per core |
| First full index, 1M LOC stack | ≤ 30 s wall at 4 workers, with progress shown and answers served as each app completes | |
| Cold Go build cache | expect +5–30 s once (the `go list -export` compile), at low priority (`nice`, `GOMAXPROCS` capped) | measured 4.6 s doze, 10.6 s doze-aws, 32.7 s Kubernetes |

### Design choices

1. **Go: the gopls loop, not go/packages-all-at-once.**
   - `go list -deps -export` metadata, then per-package `go/types` over a worker
     pool, extraction, and dropping syntax and `types.Info` immediately.
   - A bounded LRU of imported packages.
   - Persist per-package facts with an export-data hash, and write export data
     for re-checked packages with `gcexportdata.Write` so dependents don't need
     the `go` command.
   - Early cutoff when the hash is unchanged.
   - Never set `NeedDeps` together with `NeedSyntax`/`NeedTypesInfo`.
   - This alone keeps the indexing peak in the low hundreds of MB, where the
     naive approach measured at 2.2 GB, and makes a save cost milliseconds.
2. **Other languages: gotreesitter behind a narrow interface, with the grammar
   subset pinned.**
   - Start with TS, TSX, JS and Python (one build tag per language), and add a
     language when a real stack and the eval ask for it, each with a parity
     corpus.
   - Parse, extract and release in the same goroutine; trees must never escape.
   - 2 workers, raised to 4 for a first index the person is waiting on.
   - Hard limits: file size cap, minified and generated detection, a per-file
     time budget with "partial" labelling, and skipping `node_modules`,
     `vendor`, `.claude`, worktrees and `.gitignore`d paths.
   - GOMEMLIMIT only as a backstop above the budget. A tight limit made the GC
     thrash 2.5–3x in the measurements.
   - Keep the WASM-tree-sitter route as the documented fallback if gotreesitter
     stalls.
   - Expect 2–9x C speed. Budgets above already assume it.
3. **Two-phase index: extract per file, resolve per surface change.**
   - Facts are owned by a file (content hash). Resolved edges are derived facts
     owned by both ends (Glean's AND ownership), so a change on either side
     invalidates them.
   - Each file's exported-surface hash gives the early cutoff.
   - Dependency facts are "durable" (salsa), keyed `module@version`
     machine-wide, and never revalidated on a save.
4. **Change feed: FSEvents (purego) on macOS, inotify on Linux, git as the
   reconciler, polling only for tiny non-git folders.**
   - Persist the FSEvents event id and volume UUID per root, and on wake replay
     since that id. Fall back to `git status` plus `git diff <indexed commit>`
     when history is unavailable or a must-rescan flag arrives.
   - This resolves the freshness.md concern that macOS needs cgo for FSEvents.
     It doesn't; it needs purego (one dependency, `CGO_ENABLED=0` holds).
   - The daemon owns the stream so `watch` can share it, as freshness.md already
     plans. Move `internal/watch` from 1 s polling to this feed: Kubernetes-sized
     polling is about 4% of a core continuously.
5. **Memory shape while serving.**
   - A flat CSR edge store with interned symbol ids is ~40 B per edge. Use it for
     stacks up to a few million edges.
   - Above that, serve from `graph.db` with a page cache and keep only the
     symbol-name index resident.
   - Drop the in-memory graph after a quiet period (fit.md's open question):
     reloading 1M edges from a flat file is tens of ms.
6. **Add to the CI bench**: an awake-serving RSS and an indexing peak, each on a
   fixed corpus (doze-aws plus a TS app is a good pair), and a save-to-fresh
   latency. Fail on regression, as for the daemon.

---

## Commands (reproduce)

All tools are under `scratchpad/bench/`.

```sh
# Go loading modes
cd bench/pkgload && go build -o pkgload .
/usr/bin/time -l ./pkgload -mode {names|full|export|trim} [-tests] -dir <repo> -extract
GOCACHE=$(mktemp -d) /usr/bin/time -l ./pkgload -mode export -tests -dir <repo>   # cold cache

# Streaming per-package check
cd bench/stream && go build -o stream . && /usr/bin/time -l ./stream [-tests] [-share=false] -dir <repo>

# Tree-sitter: pure Go against C
cd bench/gts && go build -o gts .          # gotreesitter v0.56.0
cd bench/cts && go build -o cts .          # go-tree-sitter v0.25.0 (cgo)
gts -lang {go|ts|tsx|js} -list corpus/<l>.txt -j N
gts -per -lang ... 2> per/gts-<l>.tsv      # the same for cts; diff per file
gts/keepbin corpus/go.txt                  # retained-tree heap

# Change detection
git -C <repo> status --porcelain=v2 ; git -c core.untrackedCache=true ... ; git -c core.fsmonitor=true ...
bench/statw/statw <root> <file-list>
CGO_ENABLED=0 go build -o fsev . ; ./fsev <root> now 3 ; ./fsev <root> <savedEventId> 2

# Baselines
bin/zoekt-index -index zidx ext/k8s ; bin/scip-go index -q -o scipout/x.scip   # run inside each repo
```

## Sources

- gotreesitter repo and README: https://github.com/odvcencio/gotreesitter
- gotreesitter BENCH.md (v9 sealed epoch, fleet scoreboard, memory receipts):
  https://raw.githubusercontent.com/odvcencio/gotreesitter/main/BENCH.md
- gotreesitter benchmark notes and limitations:
  https://raw.githubusercontent.com/odvcencio/gotreesitter/main/docs/benchmark-notes.md
- gotreesitter testing guide:
  https://raw.githubusercontent.com/odvcencio/gotreesitter/main/docs/testing-guide.md
- gotreesitter module versions: https://proxy.golang.org/github.com/odvcencio/gotreesitter/@latest
- Show HN discussion (criticism, maintenance, the WASM suggestion): https://hn.nuxt.dev/item/47155597
- Official cgo binding: https://github.com/tree-sitter/go-tree-sitter
- smacker binding: https://github.com/smacker/go-tree-sitter
- wazero plus tree-sitter spike: https://pkg.go.dev/github.com/vista-cloud-dev/m-parse
- gopls scalability (v0.12 design): https://go.dev/blog/gopls-scalability
- x/tools go/packages and gcexportdata: https://pkg.go.dev/golang.org/x/tools/go/packages,
  https://pkg.go.dev/golang.org/x/tools/go/gcexportdata
- rust-analyzer, durable incrementality: https://rust-analyzer.github.io/blog/2023/07/24/durable-incrementality.html
- Glean incrementality: https://glean.software/docs/implementation/incrementality/
- Introducing stack graphs: https://github.blog/open-source/introducing-stack-graphs/
- scip-go (moved to github.com/scip-code/scip-go): https://github.com/scip-code/scip-go
- Sourcegraph, indexing a Go repository: https://sourcegraph.com/docs/code-navigation/how-to/index-a-go-repository
- Kythe: https://kythe.io/docs/ (background, not re-fetched)
- CodeGraph README: https://github.com/colbymchenry/codegraph
- codebase-memory-mcp README and paper: https://github.com/DeusData/codebase-memory-mcp,
  https://arxiv.org/pdf/2603.27277
- Serena: https://github.com/oraios/serena
- Zoekt design: https://github.com/sourcegraph/zoekt/blob/main/doc/design.md
- Git FSMonitor and untracked cache numbers:
  https://github.blog/engineering/infrastructure/improve-git-monorepo-performance-with-a-file-system-monitor/
- Watchman clockspec: https://facebook.github.io/watchman/docs/clockspec
- Bazel `--watchfs`: https://bazel.build/reference/command-line-reference
- Turborepo run reference (daemon deprecated for run): https://turborepo.dev/docs/reference/run
- purego: https://github.com/ebitengine/purego
- fsnotify/fsevents (cgo): https://pkg.go.dev/github.com/fsnotify/fsevents
- Apple FSEvents API: `FSEventStreamCreate`, `FSEventsGetCurrentEventId`, the
  `sinceWhen` history replay
