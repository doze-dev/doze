# 2. Where precision comes from beyond Go

Research for `doze recall`, 2026-10-20. Question: for languages other than Go, where should precise
symbol resolution come from (definitions, references, call edges, receiver types), given the owner's
constraints? The experience must not be tedious, must not fail often, must not ask people to install
much, and must not hurt performance badly. The options compared are language servers run on demand,
SCIP indexers, stack-graphs, and syntax-only resolution with doze's own resolvers.

Evidence standard: release and repo state comes from the GitHub API, read on 2026-10-20. Performance
numbers marked **measured** are from my own runs on this machine (Apple Silicon, macOS), using
the pinned binaries in the scratchpad and a bun LSP client (`research/lspbench.ts`). Everything else
is cited. Nothing was installed outside the scratchpad.

---

## TL;DR

1. **The landscape moved in 2026, and it favours language servers shipped as one static binary.**
   - **TypeScript:** TypeScript 7 (native Go, 7.0.2) ships per-platform `tsc` binaries with a built-in LSP (`tsc --lsp --stdio`). It needs no Node.
   - **Python:** both ty (Rust, 0.0.89) and pyrefly (Rust, 1.4.0 stable) are single ~12–14 MB downloads.
   - **Rust and PHP:** rust-analyzer and PHPantom are the same shape.
   - **Download sizes:** all of these are 9–15 MB compressed. That is the same order as the database binaries doze already fetches.
2. **SCIP indexers are the worst fit for doze.**
   - They need the build to succeed, and most need a runtime: Node for scip-typescript and scip-python, a JVM and the build for scip-java, a compile DB for scip-clang.
   - Several have stalled. scip-python's last real commit was 2025-09. scip-typescript is pinned to TS 6 because TS 7 has no compiler API yet. scip-kotlin was folded into scip-java.
   - SCIP governance moved to scip-code.org in March 2026. The format is healthy, but the indexers are mostly unmaintained.
   - The only free SCIP producer is `rust-analyzer scip`.
3. **stack-graphs is archived.** GitHub stopped supporting it (archived 2025-09). Do not build on it.
4. **Bulk extraction through LSP is feasible on small and medium repos and fast with the new servers (measured).**
   - Asking for references on every declared symbol takes:
     - tsgo: 9.5k symbols on hono in 20 s.
     - ty: 17k symbols on pydantic in 9.5 s.
     - rust-analyzer: 3.5k symbols on ripgrep in 1.9 s.
   - The cost is peak memory of 0.1–1.4 GB while the server is up, and a set of silent failure modes, listed below.
5. **The failure modes are silent, so doze must detect them.** Each was reproduced here:
   - rust-analyzer with no `cargo` returns document symbols and **zero references**, with no error.
   - Querying rust-analyzer before it is ready loses **78 % of references**: 2,925 instead of 13,177.
   - pyrefly rooted one directory too deep found **4.3× fewer references**: 6,000 instead of ~25,700.
   - tsgo, ty, pyrefly and clangd (without a compile DB) emit **no `$/progress`**, so a "wait for indexing to finish" heuristic hangs forever.
6. **Syntax alone is not enough for references.** Measured: **52–79 % of reference occurrences point at a name declared more than once in the repo.**
   - The rates were 62 % on hono (TypeScript), 79 % on pydantic (Python) and 52 % on ripgrep (Rust).
   - Import-aware scope resolution can settle many of these, but not method calls on receivers.
   - Precise backends are worth having for TypeScript, Python and Rust, where they are now cheap.
7. **Recommendation.** Syntax plus doze's own resolvers covers every language. On top of that:
   - Add a **pinned single-binary language server**, downloaded lazily, for TypeScript/JavaScript, Python and Rust (tier 1).
   - Add it for PHP as well (tier 2, preview).
   - Use the user's existing toolchain only where it is already present: C#, Swift, C/C++ with a compile DB, and opt-in Java.
   - No SCIP in v1, except ingesting an `index.scip` a repo already produces.
   - Details are in the last section.

---

## 1. Per-language landscape (state on 2026-10-20)

Sizes are the macOS arm64 download unless noted. "No runtime" means a native executable with nothing
else to install.

### Language servers

| Language | Best server(s) today | Latest / date | Standalone binary? Size | Runtime / toolchain needed | Works without deps installed / without a build? | Notes |
|---|---|---|---|---|---|---|
| TypeScript / JavaScript | **TypeScript 7 native `tsc --lsp --stdio`** (the typescript-go port) | 7.0.2, 2026-08-20 in `microsoft/TypeScript` (first released 2026-07-08); 7.1 stable planned 2026-11-24 | **Yes**: `typescript-<os>-<arch>.tgz` 9.3 MB, 26 MB unpacked (23.6 MB Go binary + `lib.*.d.ts`). Binary built with go1.26.4 | None. It calls `npm` only for automatic type acquisition | Yes. It runs on bare source, and unresolved packages become `any`. Measured on hono with no `node_modules` | Ships call hierarchy, references, implementation and typeDefinition (strings checked in the binary). No compiler API in 7.0; 7.1 adds a stable API over `tsc --api` (msgpack over stdio/pipe). Vue/Svelte/Astro templates are not supported. The old path (typescript-language-server 6.0.2 + tsserver, or vtsls) needs Node |
| Python | **ty** (Astral); **pyrefly** (Meta) | ty 0.0.89 (2026-10-16, still beta, weekly releases); pyrefly 1.4.0 (2026-10-15, stable since 1.0.0 on 2026-05-12) | **Yes, both**: ty 12 MB (26 MB binary); pyrefly 13 MB (30 MB binary) | None | Yes. Unresolved third-party imports become `Unknown`/`Any`. Both find a venv if one exists. Measured on pydantic with no venv | Both support definition, references, workspace symbol, call hierarchy, type hierarchy and implementation (their docs). pyright 1.1.414 and basedpyright 1.40.2 need Node (basedpyright also ships as a wheel that bundles Node) |
| Rust | **rust-analyzer** | 2026-10-19 (weekly) | **Yes**: 14 MB `.gz`, 38 MB binary | `cargo` plus the rust toolchain for normal project loading (`cargo metadata`, sysroot). **Not needed with a `rust-project.json`** | With no cargo it **silently returns no references**. With a generated `rust-project.json` it works on bare source (measured), minus std and third-party crates | Has a built-in `rust-analyzer scip` subcommand. Cargo mode runs build scripts and proc-macros by default, so it executes project code |
| C / C++ | **clangd** | 23.1.0, 2026-09-02 (snapshots weekly) | **Yes**: 95 MB zip on mac, 112 MB on Linux (bundles clang resource headers) | A `compile_commands.json` for correct results. Without one it guesses flags | Partly. Without a compile DB it parses each file with fallback flags and only indexes files that are open (measured: works on jq/src when all 44 files are open) | The background index needs a compile DB. Apple's clangd also ships in Xcode and swiftly toolchains |
| Java | **Eclipse JDT LS (jdtls)** | 1.64.0 snapshot, 2026-10-19 | No. 51 MB tarball of OSGi jars | **Java 21+ runtime** (a Temurin JRE is a 48 MB download); Python 3.9 for the convenience wrapper (optional) | Needs Maven or Gradle import for dependencies. Import **runs Gradle** (code execution and network). Plain folders work as an "invisible project" with source only | IDE-grade, but it starts slowly and uses a lot of memory (GB range on large projects, reported widely; not measured) |
| Kotlin | **JetBrains kotlin-lsp** | v263.7084.0, 2026-10-17 | Standalone archive is **360 MB** (aarch64 `.sit`) or 368 MB (Linux) | Bundled JVM / IntelliJ platform | Needs a Gradle or Maven JVM project import. KMP is not supported yet | README badge: **Alpha**, "partially closed-source". Call hierarchy is listed. fwcd/kotlin-language-server is the older community server |
| C# | **Roslyn LSP** (`roslyn-language-server`, the server behind C# Dev Kit); csharp-ls 0.28.0 | roslyn-language-server 5.13.0-1.26514.10, 2026-10-15 (prerelease) | No. 60 MB nupkg; not self-contained | **.NET 10 runtime**, and the SDK/MSBuild to load projects | Needs `.sln`/`.csproj` and restore (`--autoLoadProjects`) | csharp-ls also needs the .NET 10 SDK. Any C# developer already has `dotnet` |
| Ruby | **ruby-lsp** (Shopify); **Sorbet** (`srb tc --lsp`) | ruby-lsp 0.27.0.beta5 (2026-08-21); Sorbet daily builds (2026-10-20) | ruby-lsp: no, it is a gem. **Sorbet: yes, 12 MB static binary** | ruby-lsp: Ruby + Bundler (it builds a composed bundle) | ruby-lsp works on bare source, but **find-references is constants only**; methods use "guessed types" from variable names | Sorbet is precise only on `# typed: true` code. Shopify's **rubydex** (Rust "static analysis toolkit", v0.4.1, 2026-09) is the new indexer behind ruby-lsp |
| PHP | **Intelephense** (Node; references free, call/type hierarchy paid); **Phpactor** (PHP 8.1+, 4 MB phar); **PHPantom** (Rust, new) | Intelephense 1.18.5 (2026-06); Phpactor 2026.10.07; PHPantom 0.11.1 (2026-10-07) | PHPantom: **yes, 14 MB**. Others no | PHPantom: none. Intelephense: Node. Phpactor: PHP | Mostly works on source; uses `vendor/` when present | PHPantom's own table: 2 s to ready, 578 MB vs Intelephense 11 s / 766 MB and Phpactor 94 s / 467 MB. Call and type hierarchy are free. "In active development" |
| Swift | **sourcekit-lsp** | Swift 6.4.0, 2026-09-15 | Ships with the Swift toolchain or Xcode | The Swift toolchain | Background indexing (on by default since Swift 6.1) **builds modules itself**; SwiftPM only; initial indexing is "2–3× a regular build" | Use the toolchain that is already installed; never download one |
| Lua (aside) | lua-language-server | 3.19.2 | Yes, 3 MB | None | Yes | Cheap tier-2 candidate if stacks include Lua |

### SCIP indexers

| Indexer | State | Needs | Verdict for doze |
|---|---|---|---|
| scip-typescript | v0.4.0 (2025-10). Still committed to in 2026-10, but **built on the TS 6 compiler API** (`typescript ^6.0.3`). The "Update to TS v7" PR has been open since 2026-07-08 and is blocked because 7.0 has no API | Node + `npm install`, or it degrades | No |
| scip-python | npm 0.6.6 (2025-09-05); **last commit 2025-09-05** | Node + Python env + `pip show` | No (stale) |
| scip-java (now `scip-code/scip-java`, also covers Kotlin and Scala) | v0.13.1 (2026-07); 82 MB launcher | JVM + **a successful build** (compiler plugin) | No |
| scip-clang | v0.4.0 (2026-02); 67 MB mac / 142 MB Linux | `compile_commands.json` and a working build | Only if a CDB exists; clangd covers the same case |
| scip-dotnet | v0.2.14 (2026-05) | .NET SDK + restore | No (Roslyn LSP is better) |
| scip-ruby | v0.5.0 (2026-09); 18 MB static binary (Sorbet-based) | None to run; precise only on Sorbet-typed code, `typed: false` is "best effort" | Possible for Sorbet shops only |
| scip-php (davidrjenni) | 2026-07, small | PHP + composer | No |
| rust-analyzer `scip` | Built into rust-analyzer | Same as rust-analyzer | The one free SCIP producer |
| SCIP format | `scip-code/scip` v0.10.0 (2026-09-03). Governance moved from Sourcegraph to a steering committee of Meta, Uber and Sourcegraph (announced 2026-03-25) | Protobuf | Healthy format; useful as an **import** format |

### Other structural approaches

| Approach | State |
|---|---|
| **stack-graphs** (`github/stack-graphs`) | **Archived** (last push 2025-09-09). README: "no longer supported or updated by GitHub… fork it". The last language crates (TypeScript, Python, Java) were released 2024-12 |
| tree-sitter-graph | Last push 2024-12; effectively dormant |
| github/semantic | Archived (2025-04) |
| tree-sitter (core) | Active; v0.27.1 (2026-10-08) |
| Pure-Go tree-sitter | `odvcencio/gotreesitter`: pure-Go runtime, 206 grammars, v0.9.x (2026-03). The other no-cgo route is wazero with grammar WASM. Relevant because doze prefers no cgo |

---

## 2. Measurements (this machine, 2026-10-20)

**Method.**
- `lspbench.ts` (bun) starts the server and runs `initialize` / `initialized`.
- It `didOpen`s every source file, then calls `documentSymbol` on each.
- For every class, method, function, interface, enum, constructor, struct or property it gets back, it calls `textDocument/references`.
- It also measures 100 `definition` calls and up to 50 `prepareCallHierarchy` + `outgoingCalls` calls.
- Requests are sequential (one in flight). The peak RSS of the server process tree is sampled every 200 ms.
- Repos are shallow clones with **no dependencies installed** (no `node_modules`, no venv, no cargo registry). This tests the "nothing installed" case doze cares about.

| Server | Repo (size) | Ready | documentSymbol pass | refs calls / locations | refs total time | ref p50 / p95 | def p50 / p95 | peak RSS |
|---|---|---|---|---|---|---|---|---|
| tsc 7.0.2 `--lsp` | hono (429 .ts/.tsx, ~66k lines in src) | initialize 6–31 ms; blocks until the project loads | 1.25 s | 9,523 / 14,804 | **20.0 s** | 0 / 16 ms | 0 / 0 ms | **724 MB** |
| tsc 7.0.2 `--lsp` | hono/src only, first 400 symbols | – | 0.57 s | 400 / 419 | 0.36 s | 0 / 2 ms | 0 / 0 | 267 MB |
| ty 0.0.89 `server` | pydantic repo root (579 .py) | first ref 25 ms | 3.0 s | 17,043 / 41,489 | **9.5 s** | 0 / 1 ms | 0 / 0 | **593 MB** |
| pyrefly 1.4.0 `lsp` | pydantic repo root (579 .py) | first ref 2 ms | 1.5 s | 17,043 / 37,852 | **80.0 s** | 4 / 9 ms | 1 / 1 ms | **1,381 MB** |
| ty | pydantic/pydantic subdir (105 .py, 46.7k lines) | init 1.2 s | 0.68 s | 2,324 / 25,724 | 2.1 s | 1 / 1 | 0 / 0 | 408 MB |
| pyrefly | same subdir | init 0.5 s | 0.21 s | 2,324 / **6,000** | 4.4 s | 2 / 2 | 0 / 0 | 347 MB |
| rust-analyzer 2026-10-19, **no cargo** | ripgrep (56k lines) | progress ended, 3 s | 0.07 s | 3,472 / **0** | 0.2 s | – | – | 57 MB |
| rust-analyzer + **doze-generated `rust-project.json`** (11 crates, no std, no crates.io deps) | ripgrep | progress ended, ≤3 s | 0.08 s | 3,472 / **13,177** | **1.9 s** | 0 / 2 ms | 0 / 0 | **125 MB** |
| same, **without waiting for ready** | ripgrep | – | 0.03 s | 1,320 / **2,925** | 1.1 s | 1 / 2 | – | 99 MB |
| clangd 21 (Apple), no `compile_commands.json`, all files open | jq/src (44 files, 27k lines) | **no `$/progress` ever** | 0.72 s | 1,415 / 12,068 | 1.0 s | 0 / 1 ms | 0 / 0 | 373 MB |

**Call hierarchy.**
- ty, pyrefly, rust-analyzer and clangd answered `prepareCallHierarchy` + `outgoingCalls` for 39–50 of 50 named functions, all with p50 at or under 4 ms.
- tsgo's low count in my run came from my targets, not from the server. The first 50 "functions" in hono's sorted file list were anonymous test callbacks (`describe(...)`).

**Ambiguity of names (how much syntax alone cannot settle).**
- From the same runs, I computed the share of reference occurrences whose target name is declared more than once in the repo:
  - hono: 62.4 % (5,154 distinct names, 4,221 unique).
  - pydantic: 79.1 %.
  - ripgrep: 52.5 %.
- This is an upper bound on what a resolver that only matches names gets wrong. Imports and lexical scope cut it down a lot. What is left is mostly method or field access on a receiver whose type must be inferred (`c.req.param()`, `self.model_fields`, `matcher.find_at()`). That remainder is exactly what the language servers answer cheaply.

**Readiness behaviour seen.**

| Server | Signals readiness via `$/progress`? | Behaviour before ready |
|---|---|---|
| tsgo | No | Requests block until the project loads (correct) |
| ty | No | Blocks (first ref 25–55 ms) |
| pyrefly | No | Blocks |
| rust-analyzer | Yes (9 progress tokens) | **Answers early with partial results** |
| clangd without CDB | No | Indexes open files only |

A generic "wait for progress to end" rule is wrong for 4 of the 5 servers. Not waiting is wrong for rust-analyzer.

**Disagreement between servers.**
- At the pydantic repo root, ty and pyrefly were close: 41.5k vs 37.9k reference locations.
- Rooted at the package directory instead of the repo root, pyrefly found 6.0k and ty 25.7k.
- So the workspace root (where `pyproject.toml` lives) matters, and doze must choose it, not the user.

---

## 3. LSP for bulk graph extraction: feasibility and prior art

**Feasible?** Yes, up to medium repos, with the 2026 native servers.
- The cost model is (number of declarations × reference latency), or (number of identifier sites × definition latency).
- The new servers answer a reference in about 0.5–5 ms and a definition in under 1 ms once loaded.
- A 50–100k-line app costs 2–80 s of wall time and 0.1–1.4 GB of transient memory.
- Ways to cut it:
  - Run requests concurrently. tsgo is explicitly multi-threaded for "simultaneous requests" (TS 7 announcement).
  - Run definition-per-site instead of references-per-declaration. This gives edges in the graph's own direction (site → definition) and lets doze skip sites it already resolved.
- What does not scale:
  - Large monorepos: rust-analyzer reports reach ~13 GB on a workspace with 830 dependencies.
  - Opening every file.
  - Holding the server resident.
- So the server must be a **transient worker**: started for an indexing pass, kept for a short idle window to answer doubtful queries, then stopped. That fits doze's sleeping-service model and keeps idle memory at zero.

**`workspace/symbol`.** Every server here advertises it. It is a fuzzy name search with capped result counts, not an enumeration API. `documentSymbol` per file is the reliable way to enumerate, and doze's syntax layer already does that.

**Prior art.**

- **Sourcegraph, 2016–2021.** Sourcegraph ran language servers at scale (lsp-proxy, lsp-adapter), then dropped them.
  - Its 2019 retrospective names "incompatible compiler and package manager versions, not installing dependencies, slow initialization, and poor quality in general", and a proxy that "no single Sourcegrapher fully understood".
  - It deprecated LSP support entirely in 2021 in favour of LSIF indexes.
  - Lesson for doze: the pain was *environment reproduction on a server farm*. doze runs on the developer's machine, where the environment already exists. Single-binary servers remove most of the version matrix.
- **LSIF → SCIP (2022).** LSIF's graph encoding was hard to type, needed large in-memory structures, and blocked incremental indexing. SCIP indexes are 4–5× smaller, and scip-typescript gave 10× faster CI than lsif-node. Meta's Glean found SCIP "8x smaller, and can be processed 3x faster".
  - Neither format solves the build/runtime prerequisite, which is doze's actual problem.
- **Serena (oraios/serena, 30k stars, v2.1.0 2026-10-19) and its MIT core SolidLSP, forked from Microsoft multilspy (v0.1.1 2026-10-13).** This is the closest prior art: LSP behind MCP for coding agents.
  - Its language table shows the tedium doze must avoid. Many servers say "needs Node/npm", "needs .NET 10", "needs uv/uvx", or "Manual".
  - Its Python servers (pyright, ty, pyrefly) are installed through `uvx`. It added `typescript_tsgo` on 2026-10-19.
  - **Security fix in v2.1.0:** typescript-language-server loaded the repo's own `node_modules/typescript/lib/tsserver.js`, so "merely opening an untrusted repository executed JavaScript supplied by that repository". Serena now pins its own tsserver unless the project is trusted.
  - Lesson: doze should run **pinned binaries it downloaded**, never project-local servers, and should turn off build-script and proc-macro execution in rust-analyzer.
- **Aider.** Uses a tree-sitter `tags.scm` repo map with name matching and PageRank, with no resolution. This is the baseline any agent already has.
- **OpenHands.** No LSP-based graph; it relies on search, plus integrations (Serena-style) as plugins.
- **"lsp-to-scip" / LSIF-from-LSP.** rust-analyzer's own `lsif` and `scip` subcommands generate an index from its IDE database. They are the canonical example that "LSP data → index" is sound when the server exposes it internally.

**Failure modes to design for** (seen or documented):
1. **Silent empty results when the project model fails.** rust-analyzer without cargo (measured), clangd without a CDB (cross-file only for open files), jdtls before import, Roslyn before a project loads.
2. **Partial answers before ready.** rust-analyzer (measured: 78 % of references missing).
3. **No standard readiness signal.** tsgo, ty, pyrefly and clangd emit no `$/progress` here; rust-analyzer does. Some servers also send `experimental/serverStatus` (rust-analyzer) or custom notifications.
4. **Workspace-root sensitivity.** pyrefly 4.3× fewer references from the wrong root (measured).
5. **Server-to-client requests.** `workspace/configuration`, `client/registerCapability` and `window/workDoneProgress/create` must be answered or some servers stall. The bench answers all of them generically.
6. **Executing project code.** Project-local tsserver (Serena advisory), rust-analyzer build scripts and proc-macros, Gradle import in jdtls and kotlin-lsp, MSBuild evaluation in Roslyn.
7. **Memory blow-ups on big repos** (multi-GB for rust-analyzer, jdtls, kotlin-lsp).
8. **Crashes and version drift.** Mitigated by pinning versions and by doze-binaries smoke tests.

---

## 4. Targeted use: settle only what the syntax layer marks doubtful

- **Per-request cost once warm (measured):**
  - `definition` p50 0 ms and p95 ≤1 ms on tsgo, ty, rust-analyzer and clangd; pyrefly 1 ms.
  - `references` p50 0–4 ms, p95 1–16 ms.
- **Fixed cost is the server start plus project load.**
  - Roughly 0.5–3 s on these repos.
  - Memory 0.1–0.7 GB, or 1.4 GB for pyrefly.
  - On large repos the load dominates (tsgo builds the vscode repo in 10.6 s versus 125.7 s for TS 6; that is a full check, and editor first-load is lower).
- **The right granularity is batches, during the indexing pass:**
  1. The syntax layer resolves imports and lexical scope.
  2. Sites it cannot settle, mainly member accesses on inferred receivers, go to the server as `definition` requests, pipelined and in parallel.
  3. On an incremental reindex, only changed files' doubtful sites are re-asked.
- **An MCP query** that lands on a doubtful edge with no cached answer wakes the server, or reuses it if it is still warm. That costs one load (seconds) and then milliseconds per question. After an idle timeout the server is stopped.
- **Receiver types.** `hover` at the receiver gives a type string. For TypeScript, 7.1's API (`tsc --api`: checker-level `getTypeAtLocation`-style calls, stable 7.1 due 2026-11-24) will be cleaner than parsing hover text. ty and pyrefly hovers give the inferred type directly.

---

## 5. How good does syntax-only resolution get?

- **Name matching alone** (Aider-style tags) is wrong or ambiguous for a majority of reference occurrences: 52–79 % of occurrences hit a non-unique name in the three repos measured above.
- **Scope- and import-aware resolution without types** is the stack-graphs and PyCG class.
  - PyCG reports **~99.2 % precision and ~69.9 % recall** for Python call graphs on its own benchmark (ICSE 2021). pyan has low recall, down to 7.6 % on one package.
  - JavaScript approximate call graphs (Feldthaus et al., ICSE 2013) are reported at ≥66 % precision and ≥85 % recall against dynamic call graphs (secondary source; whole-edge precision is lower in replication).
  - The typical shape: precision is high if you only emit what you can prove, and recall caps around 70 % because receivers are untyped.
- **Languages where own resolvers get far.** Declarations carry types and imports are explicit: Java, C#, Kotlin (less so, because of inference), and Go (already done with go/types).
  - For Java, an own resolver can read imports plus declared field, parameter and local types (`var` needs initializer inference).
  - This is weeks per language, not months, for a "precision where provable, mark the rest doubtful" resolver. Kotlin and Scala are the hard ones.
- **Languages where own resolvers plateau early.** These have dynamic receivers: Python, Ruby, JavaScript, PHP.
  - Ruby is the extreme. Even ruby-lsp resolves methods by "guessed types" from variable names, and its find-references only covers constants.
  - For Ruby, syntax plus own resolver plus honest "doubtful" marks is the realistic ceiling unless the repo uses Sorbet.

---

## 6. Comparison of the four sources against the owner's constraints

| | Language server on demand (pinned single binary) | SCIP indexers | stack-graphs | Syntax + own resolvers |
|---|---|---|---|---|
| Installs asked of the user | None for TS, Python, Rust, PHP (doze downloads 9–15 MB). JVM/.NET languages need a runtime | Node/JVM/.NET + a working build | None (would be compiled in) | None |
| Works on broken or partial code, no deps | Yes for tsgo, ty, pyrefly, PHPantom; rust-analyzer with a doze-generated project file | No; most need the build | Yes | Yes |
| Failure rate | Medium, but detectable and recoverable (sections 3 and 7) | High (build failures, stale indexers) | n/a (archived) | Low (parsers rarely fail) |
| Precision on receivers | High | High when they run | Medium (no types) | Low to medium |
| Perf impact | Transient 0.1–1.4 GB and seconds per pass; zero idle | Minutes and GBs per index | Low | Low |
| Maintenance for doze | One LSP client + per-server quirks table + pins | One ingester + many unmaintained indexers | Fork an archived Rust project (cgo/FFI) | Per-language resolver code |
| Status in late 2026 | Strongest it has ever been (TS7, ty, pyrefly 1.x, PHPantom) | Format healthy, indexers mostly stalled | Dead | Always available |

---

## 7. Recommendation for recall

**Shape.**
- **Breadth for every language** comes from syntax plus doze's own resolvers: imports, lexical scope, and declared types where the language has them. Each edge carries a provenance label: `exact` (go/types or a server), `scoped` (own resolver proved it), or `doubtful`.
- **Precision for receivers and ambiguous names** comes from a pinned language server. doze downloads it as a single binary and runs it as a **transient sleeping service** (the same lifecycle as the databases):
  1. Woken for the indexing pass and for MCP queries that hit uncached doubtful edges.
  2. Stopped after an idle window (suggest 5 min).
  3. Never resident at idle, so the idle-heap CI budget is untouched.
- **No SCIP indexers in v1.** Add a cheap **SCIP ingester** (protobuf, pure Go) so that a repo which already emits `index.scip` in CI gets exact edges for free. Treat `rust-analyzer scip` as an optional bulk mode for Rust later.
- **Never run project-local servers or project code by default.**
  - Use the pinned binary, not `node_modules/.bin`.
  - Turn rust-analyzer build scripts and proc-macros off (`cargo.buildScripts.enable=false`, `procMacro.enable=false`) unless the user opts in.
  - Run no Gradle or MSBuild import unless the user opted that language in.

### Per-language plan

| Tier | Language | Precision source | What doze downloads, and when | Fallback when it fails |
|---|---|---|---|---|
| 1 | **TypeScript / JavaScript** | `tsc --lsp --stdio` from TypeScript 7.x, pinned. Move doubtful-edge queries to `tsc --api` once 7.1 is stable (planned 2026-11-24) | `typescript-<os>-<arch>.tgz` (~9–10 MB) from the `microsoft/TypeScript` GitHub release. Lazily, the first time recall indexes a stack app containing a `tsconfig.json`/`jsconfig.json` or `.ts`/`.js` sources. Mirror into doze-binaries with sha256 | Syntax + own resolver (ES imports are explicit). Vue/Svelte/Astro files: syntax only (TS 7 has no template support) |
| 1 | **Python** | **ty**, pinned. Measured ~8× faster references and ~2.3× less memory than pyrefly on pydantic. Keep **pyrefly** (stable 1.x) as the configured alternate if a ty release regresses. doze sets the workspace root to the directory holding `pyproject.toml`/`setup.cfg` and passes the venv if one exists | `ty-<target>.tar.gz` (~12 MB) lazily on the first Python app. pyrefly (~13 MB) only if selected | Own resolver with import graph + `self.`/class scope; mark receivers doubtful |
| 1 | **Rust** | **rust-analyzer**, pinned. If `cargo` is on PATH: cargo mode with build scripts and proc-macros off. If not, or for untrusted repos: doze **generates `rust-project.json`** from the `Cargo.toml` path deps (prototype `genrpj.ts`, ~60 lines). Measured: 13k references in 1.9 s at 125 MB on ripgrep with nothing installed | `rust-analyzer-<target>.gz` (~14–15 MB) lazily on the first Cargo app. Use the user's rustup sysroot if present, for std | Syntax + `use`-path resolver. Wait on rust-analyzer's progress (or `experimental/serverStatus` quiescent) before querying |
| 2 | **PHP** | **PHPantom** (Rust, 14 MB, no PHP runtime needed), behind a "preview" flag until it reaches 1.0 | Lazily on the first app with `composer.json`. Re-evaluate the pin quarterly | Own resolver (namespaces + `use` are explicit; `$this->` scoped) |
| 2 | **C / C++** | **clangd**, *only when* `compile_commands.json` exists (or the user's build emits one). Prefer a clangd already on PATH or in Xcode/swiftly, otherwise download | clangd zip (~95–112 MB, the largest download here). Only on explicit opt-in or when a CDB is found | Syntax + include graph; mark member calls doubtful |
| 3 (existing toolchain only) | **C#** | Roslyn `roslyn-language-server`, only if `dotnet` (10+) is on PATH. It is for anyone who builds C# | 60 MB nupkg on first C# app, only when `dotnet` is present | Own resolver (explicit `using` + declared types go far) |
| 3 | **Swift** | `sourcekit-lsp` from the *installed* toolchain (Xcode or swiftly). Never download a toolchain. Background indexing builds modules, so run it at low priority | Nothing | Syntax + own resolver |
| 3 | **Java** | Own resolver by default: imports + declared types + `var` initializer inference. **jdtls opt-in** only, when a JDK 21+ exists; it needs a Gradle/Maven import, which executes the build | Nothing by default. On opt-in, jdtls (51 MB) + JRE (48 MB) if no JDK | — |
| 3 | **Kotlin** | Own resolver. kotlin-lsp is **alpha, 360 MB, partly closed-source**; revisit when it leaves alpha | Nothing | — |
| 3 | **Ruby** | Own resolver (Prism-like parse via tree-sitter; constants and `require` graph). **Sorbet** (12 MB static) only when `sorbet/config` exists. Watch Shopify's rubydex (Rust crate) as a future static backend | Sorbet only for Sorbet repos | Mark all method calls on untyped receivers doubtful |

### Guards that keep failures rare and visible

1. **Per-server readiness rules**, kept in a quirks table:
   - tsgo, ty, pyrefly: no progress, requests block until loaded.
   - rust-analyzer: progress, then quiescent.
   - clangd: per-file.
   - Never a single generic rule.
2. **Canary check after load.** Pick 3 symbols the syntax layer saw referenced across files. If the server returns zero references for all of them, mark it `degraded` for this app and fall back to syntax. This would have caught both rust-analyzer-without-cargo and pyrefly-at-the-wrong-root.
3. **Root and config selection by doze**, not the user: nearest `tsconfig`/`pyproject`/`Cargo.toml`/`composer.json` per stack app.
4. **Budgets.**
   - Wall time per indexing pass (for example 120 s per app). Past the budget, switch from "all doubtful sites" to "only sites in changed files", or to on-demand only.
   - A memory ceiling. If server RSS exceeds about 2 GB, stop it and go targeted-only.
   - Concurrency 4–8 in-flight requests.
5. **Crash policy.** Restart once with backoff. On a second crash in a pass, mark the app `degraded`, and surface it in `doze recall status` and in the MCP answer's provenance. Never block an answer on a server.
6. **Pins and verification.** Every server version is pinned, sha256-checked, and smoke-tested in doze-binaries CI on macOS, Linux and Windows. All tier-1/2 binaries publish Windows builds. Upgrades are deliberate.
7. **Answer the server's own requests** (`workspace/configuration` → nulls or defaults, `registerCapability`, `workDoneProgress/create`). Otherwise some servers stall.

**What the user experiences:**
- The first time a stack with a TS/Python/Rust app is indexed, doze downloads 9–15 MB per language in the background, with a one-line progress note, the same way it fetches databases.
- Indexing a medium app adds seconds to tens of seconds once.
- After that, nothing stays resident.
- No language runtime is ever installed for them.
- Where precision is not available, answers say `scoped` or `doubtful` instead of failing.

---

## Artifacts

- `research/lspbench.ts`: the bun LSP bulk-extraction bench (generic over servers).
- `research/genrpj.ts`: a crude `rust-project.json` generator from Cargo.toml path dependencies.
- Pinned binaries used: `scratchpad/dl/{ts7,ty,pyrefly,ra}`. Repos: `scratchpad/repos/{hono,pydantic,ripgrep,jq}`.

## Sources

Opened or queried (GitHub API reads were made 2026-10-20):

- TypeScript 7 announcement: https://devblogs.microsoft.com/typescript/announcing-typescript-7-0/
- TypeScript releases (v7.0.2 assets): https://github.com/microsoft/TypeScript/releases/tag/v7.0.2
- typescript-go (archived staging repo; `cmd/tsgo/{main,lsp,api}.go`): https://github.com/microsoft/typescript-go
- TypeScript 7.1 iteration plan: https://github.com/microsoft/TypeScript/issues/63703
- ty releases: https://github.com/astral-sh/ty/releases; LSP features: https://docs.astral.sh/ty/features/language-server/
- ty beta coverage: https://www.infoworld.com/article/4108979/python-type-checker-ty-now-in-beta.html, https://pydevtools.com/blog/ty-beta/
- pyrefly releases and 1.0.0 notes: https://github.com/facebook/pyrefly/releases/tag/1.0.0; IDE features: https://pyrefly.org/en/docs/IDE-features/
- basedpyright: https://github.com/DetachHead/basedpyright; pyright: https://github.com/microsoft/pyright
- rust-analyzer releases: https://github.com/rust-lang/rust-analyzer/releases
- rust-analyzer memory report: https://users.rust-lang.org/t/rust-analyzer-using-13-gib/133914
- clangd releases: https://github.com/clangd/clangd/releases
- jdtls README and snapshots: https://github.com/eclipse-jdtls/eclipse.jdt.ls, https://download.eclipse.org/jdtls/snapshots/
- Adoptium JRE 21 API: https://api.adoptium.net/v3/assets/latest/21/hotspot
- kotlin-lsp README and release v263.7084.0: https://github.com/Kotlin/kotlin-lsp
- Roslyn LSP package: https://www.nuget.org/packages/roslyn-language-server.linux-arm64
- csharp-ls: https://github.com/razzmatazz/csharp-language-server
- ruby-lsp docs: https://shopify.github.io/ruby-lsp/; rubydex: https://github.com/Shopify/rubydex; Sorbet releases: https://github.com/sorbet/sorbet/releases
- PHPantom: https://github.com/PHPantom-dev/phpantom_lsp; phpactor: https://github.com/phpactor/phpactor; intelephense: https://github.com/bmewburn/vscode-intelephense; mago: https://github.com/carthage-software/mago
- sourcekit-lsp background indexing: https://github.com/swiftlang/sourcekit-lsp/blob/main/Documentation/Enable%20Experimental%20Background%20Indexing.md
- SCIP: https://github.com/scip-code/scip, https://scip-code.org/, governance: https://scip-code.org/governance.html
- The future of SCIP (2026-03-25): https://sourcegraph.com/blog/the-future-of-scip
- Announcing SCIP: https://sourcegraph.com/blog/announcing-scip
- scip-typescript: https://github.com/sourcegraph/scip-typescript (package.json, PR "Update dependency typescript to v7")
- scip-python: https://github.com/sourcegraph/scip-python
- scip-java: https://github.com/scip-code/scip-java; scip-kotlin (merged): https://github.com/sourcegraph/scip-kotlin
- scip-clang, scip-dotnet, scip-ruby: https://github.com/sourcegraph/scip-clang, https://github.com/sourcegraph/scip-dotnet, https://github.com/sourcegraph/scip-ruby
- scip-php: https://github.com/davidrjenni/scip-php
- npm registry metadata: https://registry.npmjs.org/@sourcegraph/scip-typescript, …/@sourcegraph/scip-python, …/pyright, …/basedpyright, …/intelephense, …/typescript-language-server, …/@vtsls/language-server, …/@typescript/native-preview
- stack-graphs (archived): https://github.com/github/stack-graphs; tree-sitter-graph: https://github.com/tree-sitter/tree-sitter-graph; semantic: https://github.com/github/semantic
- Sourcegraph LSP history: https://sourcegraph.com/blog/improving-language-support-in-2019, https://about.sourcegraph.com/blog/deprecating-lsp
- Serena changelog v2.0/v2.1 (tsgo support; tsserver code-execution fix): https://github.com/oraios/serena/blob/main/CHANGELOG.md
- Serena language support: https://oraios.github.io/serena/01-about/020_programming-languages.html
- multilspy: https://github.com/microsoft/multilspy
- Aider: https://github.com/Aider-AI/aider; OpenHands: https://github.com/All-Hands-AI/OpenHands
- PyCG: https://arxiv.org/pdf/2103.00587; JARVIS: https://arxiv.org/pdf/2305.05949
- JS approximate call graphs: https://2013.icse-conferences.org/content/efficient-construction-approximate-call-graphs-javascript-ide-services.html, https://www.academia.edu/69427704/Efficient_construction_of_approximate_call_graphs_for_JavaScript_IDE_services
- Pure-Go tree-sitter: https://pkg.go.dev/github.com/odvcencio/gotreesitter
- Benchmark repos: https://github.com/honojs/hono, https://github.com/pydantic/pydantic, https://github.com/BurntSushi/ripgrep, https://github.com/jqlang/jq
