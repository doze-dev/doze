# 1. What context makes coding agents better, and where they fail without it

Research for doze recall, 2026-10-20. Question: from the agent's side, what
must a per-stack graph and context layer contain, and what should it leave out?

Every number below comes from a page I opened (listed under Sources). Where I
only saw an abstract, I say so. Vendor numbers are marked **[vendor]**: the
vendor built both the tool and the eval, and none of them has been replicated
independently.

---

## 0. Bottom line

1. **Finding the right file in one repo is mostly no longer the bottleneck for
   frontier agents.** Failed SWE-bench runs still touch the right files 72–81%
   of the time. In failure taxonomies, localization accounts for roughly
   9–16% of failures. Code-graph localizers add about 2–4 points on top of a
   plain agent. Below about 400K LOC, Sourcegraph's own benchmark shows code
   search tools *lowering* reward (−0.080).
2. **The dominant failure is the incomplete change.** The agent fixes the
   primary site and misses other callers, wrappers, fixtures, generated code
   or downstream consumers, then stops because the visible tests pass. This
   is the largest category in every study that measures it: 53% of failed
   trajectories in one taxonomy, the top root cause in DepBench (where 61.8% of
   failures passed visible tests first), and 2 of 7 files touched in
   Sourcegraph's refactor case.
3. **Cross-repo and large-codebase work is where structured retrieval clearly
   pays.** The multi-repo retrieval F1 gain is +0.209, against +0.085 for a
   single repo. Precision@5 on cross-org tasks rises from 0.007 to 0.471. For
   400K–2M LOC the reward gain is +0.259. **No benchmark tests multi-service
   changes over a declared topology (queue producer to consumer, route to
   client) — a real evidence gap.** Recall would be first here, so its own
   eval has to make the case.
4. **Version-correct library use is a measured, large failure, and context
   fixes much of it.** Version-conditioned tasks pass at 48–51%, and
   documentation RAG adds up to +10 points. A version-matched docs index took
   Vercel's evals from 53% to 100% **[vendor]**. Docs alone are "necessary but
   not sufficient": executable output went from 42.6% to 66.4% with docs.
5. **Wrong or stale context hurts, and models do over-trust it.** Models
   adopt incorrect retrieved content over their own correct knowledge more
   than 60% of the time. Misleading comments cut retained fault-localization
   success to about 25%. LLM-generated AGENTS.md files *lowered* success
   (−0.5 to −2 points) and raised cost by 20–23%.
6. **Agents often don't use what you give them.** A skill was never invoked
   in 56% of Vercel's eval cases. Claude chose an LSP tool over grep in 0–6% of
   localization tasks. Forcing the semantic tool first cut success from 100%
   to 89%. Context the agent has to *decide* to fetch underperforms context
   that is simply present, but present context costs tokens on every turn.
7. **Upfront overviews do not help.** Repository overviews in context files
   were "not effective". File-by-file descriptions are on Claude Code's own
   "exclude" list. Only non-inferable facts earn a place: commands, tooling,
   env quirks and gotchas.

For recall, this says: be the authority on **completeness of impact across
the stack**, on **how to run and verify each app**, on **pinned versions**
and, later, on **runtime state**. Do *not* compete with grep on "where is X"
inside one small repo. Never serve an edge or note you can't vouch for as
current.

---

## 1. Method and caveats

- Sources are peer-reviewed or arXiv papers (2023–2026), vendor engineering
  posts, and two practitioner pilots. Dates and model versions are given
  inline because they matter: results on Claude 3.5 or GPT-4o (2024–25) may
  not carry over to Opus 4.8 or GPT-5.5 (2026).
- SWE-bench Verified is contaminated. Models name the buggy file from the
  issue text alone 76% of the time on Verified, against 53% on repos outside
  SWE-bench (SWE-Bench Illusion, NeurIPS 2025). Any localization gain on
  Verified is therefore suspect, and SWE-bench Pro is the better reference.
- The harness matters as much as the context. DepBench: holding GPT-5.5
  fixed, Codex beat Claude Code by 16.3 points. "Is Grep All You Need" (May
  2026): scores "depend strongly on which harness and tool-calling style is
  used". "The Devil Is in the Interface" (Aug 2026): six architectures
  exposing the same information changed behavior; CodeAct-style tools reached
  similar success with 56% fewer tokens. A context layer must be evaluated
  *inside* Claude Code, Codex and Cursor, not in a custom loop.
- Most code-graph and MCP evidence is vendor-run, on one codebase or one
  model, often with no quality metric at all.

---

## 2. Where agents fail, by evidence

### 2.1 Localization (wrong file): a smaller problem than it looks

| Evidence | Finding |
|---|---|
| Success/failure trajectories, OpenHands, SWE-agent and Prometheus on SWE-bench (arXiv 2511.00197, Oct 2025) | 72–81% of *failed* runs still identified the right files. "Localization is not the main differentiator" |
| "Near-Misses, Not Wrong Turns" taxonomy, 250 failed GPT-4o/OpenHands trajectories (repo v3.1, Oct 2026) | NARROW-INCOMPLETE 53.2%, NARROW-REGRESSIVE 12.4%, LOC-REPAIR 10.8%, LOC-EXPLORE 5.6%. Localization totals about 16% |
| SWE-Next (search snippet only, not opened) | Search/localization 8.7% of failures; patch quality 75.7% |
| RepoGraph (ICLR 2025), SWE-bench Lite, GPT-4/4o | Resolve rate +2.0 to +2.7 points (Agentless 27.33→29.67; SWE-agent 18.33→20.33). File accuracy +5–7 points, line accuracy only +2–3. A 1-hop ego graph (about 2.3K tokens) beat 2-hop flattened (26.00) |
| LocAgent (ACL 2025), Loc-Bench, Claude 3.5 | File Acc@5 83.4 against a plain OpenHands agent's 79.8, a gain of only 3.6 points. The biggest ablation drop came from removing *SearchEntity* (keyword entity lookup), not graph traversal. Downstream Pass@10 33.6→37.6 when replacing Agentless's weaker localizer |
| CoSIL (2025), Qwen2.5-Coder-32B | Top-1 43–45%; +2.98 to +30.5% resolution with Agentless. The gains are on a weaker open model |
| OrcaLoca (ICML 2025) | Function match 65.3% on Lite; +6.33 points resolved for an open framework |
| SWE-bench Illusion | 76% of file paths guessable from the issue alone on Verified, 53% elsewhere |

**Reading:** graph-based localization helps most for weak models and weak
baselines, and adds a few points for strong agents. Its value is real but
small, and partly inflated by contamination. Keyword lookup of named entities
carries much of it.

### 2.2 Incomplete propagation (missed callers, consumers and sites): the main failure

| Evidence | Finding |
|---|---|
| DepBench, "Update from Hell" (arXiv 2608.30300, Aug 2026), 203 dependency-upgrade tasks in 5 ecosystems | Best: Codex + GPT-5.5 51.2%; Claude Code + Opus 4.8 about 83/203. **"Incomplete migration"** (right API surface found, change not carried through wrappers, generated state, fixtures, call sites) is the largest category in every configuration. In **61.8%** of non-pass trials the visible tests or build *passed* before hidden tests failed |
| Failure taxonomy above | NARROW-INCOMPLETE 53.2% is the single largest label |
| Sourcegraph, "Why coding agents fail in large codebases" (May 2026) **[vendor]** | Strata refactor: baseline changed 2 of 7 affected files (score 0.32); with code search it found all 7 (0.80). "A partial refactoring is often worse than no refactoring" |
| SWE-bench Pro (Scale AI, Sept 2025) | Reference patches average 107 lines over 4.1 files. Top public-set Pass@1 43.6% (Sonnet 4.5) against >70% on Verified; commercial set 17.8% (Opus 4.1). Failure modes: wrong solution 35.9% (Opus 4.1); context overflow 35.6% and endless file reading 17.0% (Sonnet 4) |
| BeyondSWE (arXiv 2603.03194, Mar 2026) | DepMigrate (repo-wide edits after upstream breaking changes): best 48.6% |

**Reading:** the strongest, most replicated signal in this research. The
missing context is "here is *everything* that depends on what you changed",
with a way to check the list is complete. Agents stop when tests pass. Tests
rarely cover cross-boundary consumers.

### 2.3 Scale and cross-repository work

- **CodeScaleBench** (Sourcegraph, Mar–May 2026, 370 tasks, 40+ repos, 9
  languages, 1,281 runs; Claude Code + Haiku 4.5 in the March post)
  **[vendor]**:
  - Reward delta from code search tools: **−0.080 below 400K LOC** ("tools
    add overhead"); **+0.259 at 400K–2M LOC**.
  - Retrieval F1@5 0.099→0.262. Multi-repo F1 delta **+0.209 vs +0.085**
    single repo. Org (cross-repo) P@5 **0.007→0.471**, R@5 0.002→0.149.
  - Task reward gains are concentrated: Understand +11.5%, Refactor +10.3%,
    Fix +9.9%, Incident debugging +11.2%, Security +10.6%. Other suites were
    flat within error bars. "Better retrieval doesn't always translate into a
    huge reward jump."
  - Cost $0.73→$0.51 per task (about −30%). One refactor took 96 tool calls
    over 84 minutes with 6 reversals (0.32), against 5 calls in 4.4 minutes
    (0.68).
  - An earlier summary of the same benchmark (288 tasks) cited file F1
    0.091→0.240 with aggregate reward "effectively unchanged". I saw this
    only in a search summary.
- **BeyondSWE CrossRepo** (200 issues needing external repos): best 55.2%
  (Codex GPT-5.4 xhigh with a search prompt), 46.2% default, OpenHands
  44.0%. Search helped on 20 of 32 paired comparisons and *regressed* about
  31%. More searching did not reliably help.
- **Microservice-specific agent benchmarks**: none tests editing existing
  multi-service systems. RepoGenesis (greenfield microservice generation)
  reaches 23.7% Pass@1, with failures attributed to cross-file consistency,
  architectural coherence and dependency management. Codebase-Memory builds
  HTTP_CALLS/ASYNC_CALLS edges but *does not evaluate them*.

**Reading:** this is recall's home ground, the part a code-only tool can't
reach. But the supporting evidence is vendor-run and indirect. Typical doze
stacks may well sit below 400K LOC, where code search *hurt*. The case for
recall must come from the *stack join*, not from code search.

### 2.4 Library API correctness at pinned versions

| Evidence | Finding |
|---|---|
| GitChameleon 2.0 (NeurIPS 2025), 328 version-pinned Python tasks | Enterprise models pass 48–51%. Documentation RAG gives up to +10 points (GPT-4.1 48.5→58.5). Self-debugging gives +10–20. Agents with a sandbox gain about +25 (GPT-4o 25.4→50.0). Coding assistants given the problem statement gain +12–35 (Goose GPT-4.1 19.2→55.5). Zero-shot CoT often *hurt* (o1 51.2→41.2) |
| Evolving APIs (arXiv 2604.09515, Apr 2026), 270 post-cutoff updates; 11 *small or old* models | With an update description only: 74.6% adoption, 42.6% executable. With full docs: 92.9% adoption, 66.4% executable. Failures include old-API reversion (16.4%) and mixed old/new (12.3%); 63% of failures on API additions are hallucinated APIs. "Documentation is necessary but not sufficient" |
| Vercel, AGENTS.md vs skills (Jan 2026), Next.js 16 APIs absent from training **[vendor]** | Baseline 53%, skill (default) 53%, skill with explicit instruction 79%, **8KB pointer index in AGENTS.md 100%**. The index lists *paths* to version-matched docs, not their content; compressing 40KB→8KB lost nothing |
| BeyondSWE failure analysis | "Version grounding": agents used patterns from a newer library version incompatible with the repo's pinned environment |
| Package hallucination (USENIX Security 2025) | Hallucinated packages at ≥5.2% (commercial) and 21.7% (open models). A 2026 replication found 4.6–6.1% on newer models (search summary only) |

**Reading:** strong evidence that version-matched references fix a large
share of these errors, and that execution feedback fixes much of the rest.
What the agent most needs is the *exact pinned version* plus a pointer to the
matching docs or source. Doze already knows module versions (postgres,
kafka, the AWS emulator surface) and can read go.mod and lockfiles.

### 2.5 How to run, build and test

- **AGENTS.md study** (Gloaguen et al., ETH/ICLR 2026; Claude Code + Sonnet
  4.5, Codex + GPT-5.2/5.1 mini, Qwen Code): instructions *are* followed when
  they name tooling. uv was used 1.6× per task when mentioned, against
  <0.01 when not; repo-specific tools 2.5× against <0.05. Context files
  increased test runs. The authors recommend human-written files with
  "minimal requirements, such as specific tooling".
- **Claude Code best practices** (current docs): "Give Claude a way to verify
  its work… It's the difference between a session you watch and one you walk
  away from." The include list starts with "Bash commands Claude can't guess",
  "Testing instructions and preferred test runners" and "Developer environment
  quirks (required env vars)".
- **EnvBench** (ICLR 2025, GPT-4o): the best agent configured only 6.7% of
  Python and 29.5% of JVM repos. Environment setup is hard for agents left on
  their own.
- **"To Run or Not to Run"** (ISSTA 2026): agents average 8.8 test runs per
  task. On commercial agents with SOTA models, *forbidding* execution changed
  resolve rate by only 1.25 points (not significant), and the benefit was
  concentrated in some instances. Counterweight: on SWE-bench, where the
  harness supplies the environment, execution matters less than assumed.
  DepBench says test running was the main discovery method.
- **AGENTS.md efficiency** (Lulla et al., Jan 2026, 10 repos, 124 PRs):
  AGENTS.md present → median runtime −28.6%, output tokens −16.6%,
  completion comparable.

**Reading:** a medium-to-strong case. Exact, working commands (start this
app, run its tests, which env vars and ports, which dependencies must be up)
are followed and save time. This is exactly what doze knows and agents can't
guess. In a multi-service stack, "how do I run the thing that exercises
api → queue → worker" is out of reach without the stack declaration.

### 2.6 Runtime state (logs, errors, traces)

- **OpenRCA** (ICLR 2025): root cause from 68 GB of telemetry, best about
  11%. **OpenRCA 2.0** (Jun 2026, 500 instances, 11 frontier LLMs): exact
  root-cause set 20.7%; a correct service named 76.0%, but only 61.5% of
  those tied to a verified propagation path ("ungrounded diagnosis").
- CodeScaleBench incident-debugging suite: +11.2% reward with code search
  **[vendor]**.
- No study measured "agent with recent app logs or errors" against "without"
  on code tasks.

**Reading:** weak but suggestive. Agents can name the right service from
telemetry and fail to connect it to the cause. A graph that links a running
service's errors to code and to upstream/downstream edges is the missing
piece, but nothing has measured it. Fine as a later layer, if the eval shows
it moving the numbers.

### 2.7 Conventions

- Logging conventions (arXiv 2604.09409, Apr 2026): agents broadly matched
  project logging norms, but **ignored explicit logging requests about 67% of
  the time**. Instructions made no measurable difference (14.8% vs 20.8%). The
  authors recommend deterministic guardrails (linters, CI).
- Cursor adoption (difference-in-differences, Nov 2025): a persistent rise in
  static-analysis warnings and code complexity. Velocity gains were
  transient.
- Augment scores "code reuse" and "best practices" in its PR eval, but
  publishes no per-dimension numbers **[vendor]**.
- Claude Code docs: include "code style rules that differ from defaults",
  exclude "standard language conventions Claude already knows".

**Reading:** weak. Agents mostly copy local style from code they read
anyway. Prose conventions are often ignored; checks are what work.

---

## 3. Vendor findings: what each measured

| Vendor | Claim | What was measured | Weight |
|---|---|---|---|
| Anthropic / Claude Code (Cherny, 2025) | Early Claude Code used RAG with a local vector DB; agentic search "outperformed everything else by a lot" | Internal "vibes" plus internal benchmarks; reasons include *staleness* ("the code drifts out of sync"), security, liability | Anecdotal; the trade is latency and tokens |
| Anthropic, "Effective context engineering" (Sep 2025) | Just-in-time retrieval via lightweight identifiers; CLAUDE.md up front; "hybrid strategy" likely best; context rot; "bloated tool sets" a common failure | Guidance, not numbers | Design principles |
| Anthropic, "Writing tools for agents" (Sep 2025) | Consolidate tools; return names, not ids; `response_format` concise/detailed; 25K-token default cap on tool output in Claude Code; actionable errors | Internal tool evals; resolving UUIDs to names cut hallucinations | Directly applicable |
| Anthropic, "Code execution with MCP" (Nov 2025) | Loading tool definitions on demand: 150K→2K tokens | One example workflow | Tool-schema cost is real |
| Cursor, semantic search (Nov 2025) | +12.5% answer accuracy (6.5–23.5% by model) with grep+semantic vs grep | Offline Cursor Context Bench (size and scoring not disclosed); online A/B: code retention +0.3% overall, **+2.6% on 1,000+ file repos**; dissatisfied follow-ups +2.2% without | **[vendor]** Small online effect; largest on large repos; best result is grep *plus* semantic |
| GitHub Copilot embeddings (Sep 2025) | Retrieval quality +37.6% (0.362→0.498); acceptance +110% for C#, +113% for Java | Retrieval benchmark; per-language acceptance | **[vendor]** Retrieval metric, not task success |
| Cognition / Windsurf SWE-grep (Oct 2025) | >60% of the first turn spent retrieving context; retrieval scored with F0.5 (**precision weighted over recall** because "polluting the main agent's context hurts more"); 4 turns × 8 parallel calls | Internal CodeSearch Eval | **[vendor]** The precision-first rationale is worth copying |
| Sourcegraph CodeScaleBench (2026) | See §2.3: precision up, cost −30%, gains above 400K LOC and cross-repo; *negative* below 400K; "MCP death spiral" when repo mirrors broke (0.0 against baseline 1.0); more search tools sometimes diluted context | 370 tasks, Haiku 4.5 | **[vendor]**, but publishes negative results; benchmark is public |
| Augment Context Engine MCP (Feb 2026) | "70%+" improvement: Claude Code + Opus 4.5 +80%, Cursor + Opus 4.5 +71%, Cursor + Composer-1 +30% | 300 Elasticsearch PRs × 3 prompts, 5 dimensions; grader and scale undisclosed; one codebase | **[vendor]** Weakest methodology here |
| Aider repo map (2023) | Tree-sitter definitions and references, graph-ranked, ~1K-token default budget | No quantitative evaluation | Design reference only |
| Vercel (Jan 2026) | 8KB always-present pointer index: 100%, against 53–79% for on-demand skills | Next.js 16 API evals | **[vendor]** Strong signal on *passive vs on-demand* |

---

## 4. Do code-graph MCP servers help? What the evidence says

**Efficiency gains are well supported. Quality gains are not shown, and
sometimes quality falls.**

- **Codebase-Memory** (arXiv 2603.27277, Mar 2026; Opus 4.6; 31 languages,
  one repo each; author-graded): quality **0.83 against 0.92** for a file-
  exploring agent. About 10× fewer tokens (~1K vs ~10K per question) and 2.1×
  fewer tool calls. The graph won on graph-native questions (hubs, caller
  ranking) in 19/31 languages. The explorer won on questions needing
  *source* (16/31) and on exhaustive call-site grep (10/31), "since the graph
  stores relationships but not source lines". The authors recommend a hybrid.
- **CodeGraph** (colbymchenry, 2026; Claude Code headless, Opus 4.8; 7 repos,
  one architecture question each, 4 runs): 88% fewer tool calls, 62% fewer
  tokens, 44% cheaper **[vendor]**, and **no answer-quality metric**. Its
  candid caveats:
  - about 80% *more* retrieval context left in the window at session end;
  - "only helps when the main agent queries it directly", so it is overhead
    if subagents read files instead;
  - earlier figures were contaminated (the baseline reached CodeGraph through
    Bash in 26/28 runs);
  - cross-file coverage tops out at 74% on Django because of dynamic
    dispatch.

  It also tells agents to "trust the results rather than re-verify with
  grep", which is dangerous whenever the index is stale. It writes a block
  into CLAUDE.md/AGENTS.md because subagents never see MCP instructions.
- **Serena**: no published task-success benchmark. Its token claims measure
  schema size only.
- **AgentConnect pilot** (Aug 2026; Opus 4.8, Sonnet 4.6, Haiku 4.5; small
  sample): with grep and LSP both available, models chose LSP **0–6%** on
  localization and 45–57% on reference-completeness tasks. Forcing LSP first
  on localization cut success **100%→89%**. On references, LSP raised
  precision (1.00 vs 0.76) but recall stayed about 0.66 in both arms. LSP
  helped on a noisy repo, not a clean one. **Returning 2 lines of source with
  each location** raised rename pass@1 from 0.67 to 0.83 and cut follow-up
  reads from 15.2 to 3.2.
- **Tool non-use**: in Vercel's evals the skill was never invoked in 56% of
  cases. Wording mattered: "MUST invoke first" made the agent anchor on docs
  and miss project context (it wrote the right `page.tsx` but missed
  `next.config.ts`). "Explore project first, then invoke" worked better.
- **Tool bloat**: Anthropic names bloated tool sets as a top failure.
  RAG-MCP: tool-selection accuracy 13.6% with all tools loaded against 43.1%
  with retrieval. Sourcegraph: extra search tools sometimes lowered results.

**Reading:** a graph server tends to (a) be ignored for questions grep
answers well, (b) cost tokens for its schema, (c) lose on questions that need
source text, and (d) win on structural, aggregate and cross-boundary
questions grep can't answer cheaply. That is the slice recall should own.

---

## 5. Wrong or stale context: do agents over-trust it?

Yes, measurably, in both directions: they adopt wrong context, *and* they
sometimes ignore right context in favor of training memory.

| Evidence | Finding |
|---|---|
| ClashEval (2024), 1,200 questions, 6 LLMs incl. GPT-4o | Models adopt incorrect retrieved content, "overriding their own correct prior knowledge over 60% of the time". Less-confident models adopt more readily; blatant errors are adopted less than subtle ones |
| "How accurately do LLMs understand code" (arXiv 2504.04372), 8 models incl. GPT-4o and Claude 3.7 | After semantics-preserving misleading comments, only **24.55%** of previously correct fault localizations survived (misleading names 28.7%, dead code 18.5%). Overall, 81% failed to re-localize the same fault under such mutations |
| "When Comments Mislead" (ACM, 2025–26; abstract via search only) | Outdated comments have "a systematic misleading effect": code that looks plausible but fails where the implementation changed |
| AGENTS.md study (ICLR 2026) | LLM-generated context files: −0.5 points (SWE-bench Lite), −2 points (AGENTbench), +20–23% cost, +2.5–3.9 steps. Developer-written: +4 points on average, **but not for Claude Code**. Context files did *not* shorten the steps before first touching a patched file. With repo docs removed, LLM files *helped* (+2.7%), so the harm was redundancy |
| "What to retrieve" (ICSE 2026) | Retrieved *similar code* degraded generation by up to 15%; in-file context and API info helped |
| Evolving APIs | Even with correct docs, models reverted to old APIs (16.4% of non-adoption failures) or mixed old and new (12.3%) |
| STALE (arXiv 2605.06527) | Best model 55.2% at recognizing invalidated memories; models "accept outdated assumptions embedded in a user's query" |
| Context rot (Chroma, Jul 2025; 18 models) | Accuracy falls with input length; one distractor hurts, four hurt more; focused prompts (~300 tokens) far beat full ones (~113K) |
| Lost in the Middle (TACL 2024); GSM-IC (ICML 2023) | Position effects; irrelevant context "dramatically" lowers accuracy |
| Claude Code docs | "Bloated CLAUDE.md files cause Claude to ignore your actual instructions" |

**Reading:** a wrong edge or stale note does not simply fail to help. It
steers the agent wrong, and agents rarely re-check. Recall's "no edge rather
than a wrong edge" and "nothing silently stale" principles are backed by
direct evidence. A visible staleness label is better than nothing, but there
is no evidence that agents *act* on such labels (STALE suggests they often
won't). The safer default is to omit, or to demote to "unverified, check
with grep".

---

## 6. Question types, ranked by evidence

| Rank | Question type | Evidence that agents fail without it | Evidence that context fixes it | Overall |
|---|---|---|---|---|
| 1 | **Impact / blast radius, with completeness** ("everything that must change if I change X": callers, consumers, wrappers, fixtures, generated code) | Strong: DepBench, failure taxonomy, Sourcegraph Strata, SWE-bench Pro | Medium: Sourcegraph +10.3% refactor reward **[vendor]**; LSP improved *precision*, not recall (pilot) | **Strong need, medium proof of fix** |
| 2 | **Library/API correctness at pinned versions** | Strong: GitChameleon 48–51%, evolving APIs, BeyondSWE version grounding, package hallucination | Strong: docs RAG +10, Vercel 53→100 **[vendor]**, evolving-API docs 42.6→66.4 executable | **Strong** |
| 3 | **How to run, test and verify** (commands, env, dependencies up) | Medium: EnvBench 6.7–29.5%; agents stop when "looks done" | Medium-strong: tooling instructions followed (uv 1.6× vs ~0); runtime −28.6%; Claude Code docs | **Medium-strong** |
| 4 | **Flows across services / repos** (who produces and who consumes this queue; which client calls this route) | Medium: CrossRepo 44–55%; Org P@5 0.007 baseline **[vendor]** | Medium: multi-repo F1 +0.209 **[vendor]**; no direct microservice benchmark | **Medium, with an evidence gap recall must fill with its own eval** |
| 5 | **Runtime state** (recent errors, logs, which service is failing) | Medium: OpenRCA 11–21% exact; ungrounded diagnosis | Weak: incident-debugging +11.2% **[vendor]**; nothing measured on code tasks | **Weak-medium** |
| 6 | **"Where is X handled"** (single-repo localization) | Weak for frontier agents: 72–81% right files even in failures; contamination | Weak: +2–4 points; negative below 400K LOC; agents ignore semantic tools | **Low priority for recall** |
| 7 | **Conventions** | Weak-medium: logging requests ignored 67% | Weak: prose instructions show no measurable effect; checks work | **Low; prefer checks** |
| — | **Architecture overviews / file-by-file descriptions** | — | Negative: "not effective" (ETH); on Claude Code's exclude list | **Avoid** |

---

## 7. Implications for recall

Each requirement is tied to evidence and graded **strong / medium / weak** by
how directly the evidence supports it.

### Must contain

**R1. Complete, closed-world impact answers across the stack. — strong**
Given a symbol, route, queue, topic, table, env var or module, return *every*
dependent site across all apps. Order the list by hop, give `file:line` and
2–3 source lines for each, and say explicitly whether the answer is
**complete** or **partial, and why** (dynamic dispatch, non-Go code,
unresolved reference). Include non-code dependents the agent won't think to
grep: fixtures, generated code, seeds and migrations, `doze.hcl` wiring,
other apps' env.
*Evidence:* incomplete propagation is the top failure (DepBench, 53.2%
taxonomy, 2/7 files); 61.8% of failures passed visible tests; LSP left recall
at about 0.66; the Codebase-Memory explorer won on exhaustive call-site
questions, so a graph only beats grep here if it *is* exhaustive.

**R2. Stack-join edges no code tool has. — medium**
Edges from producer to queue to consumer, publisher to topic to subscriber,
app to table, HTTP client call to route, and env var to consuming code, each
proven from `doze.hcl` plus code. This is the "flows across services" answer.
*Evidence:* cross-repo retrieval gains are about 2.5× single-repo (+0.209 vs
+0.085) and Org P@5 goes from 0.007 to 0.471 **[vendor]**. No benchmark covers
this, so recall's eval (eval.md) must include **cross-app change tasks with
hidden tests in the consuming app**. That is the gap where recall's value
would show and where no published result yet exists.

**R3. Per-app run and verify facts, derived from the stack. — medium-strong**
For each app: how to start it and what it depends on (which modules must be
awake), the exact test command, the ports and env vars it reads, and how to
exercise a cross-app path end to end (send to this queue, watch that worker).
Short, exact, copy-pasteable.
*Evidence:* tooling instructions are followed (uv 1.6× vs <0.01); AGENTS.md
cut runtime 28.6%; agents stop at "looks done" without a check; EnvBench
shows agents can't reliably reconstruct environments on their own. This is
cheap for doze, because it already knows all of it.

**R4. Pinned versions, with pointers to version-matched references. — strong**
Per app: language toolchain and direct dependency versions (go.mod,
lockfiles). Per module: engine and emulator versions and the supported API
surface (for example which AWS operations doze-aws implements). Point to
docs or vendored source for that exact version (a path in the module cache,
not pasted content). Pair this with R3's verify step, because docs alone
leave about a third of outputs failing.
*Evidence:* GitChameleon 48–51% and +10 with docs; Vercel pointer index
53→100% **[vendor]**; evolving APIs 42.6→66.4% executable; BeyondSWE version
grounding failure. An 8KB *pointer* index lost nothing against 40KB of
content.

**R5. Source lines with every location. — medium**
Answers carry the snippet, not bare ids or bare `file:line`.
*Evidence:* the pilot's 2 lines of context took rename pass@1 from 0.67 to
0.83 and follow-up reads from 15.2 to 3.2; Codebase-Memory lost 16/31
languages for lacking source; Anthropic: names beat ids.

**R6. Human notes limited to what can't be inferred. — medium**
Gotchas, decisions, "never call X directly", required env quirks. Each note
anchored to a graph node so it surfaces only with that node, and flagged
when its anchor changes.
*Evidence:* developer-written files +4 points (but not for Claude Code);
Claude Code docs include/exclude table; bloated files get ignored.

### Must avoid

**A1. No overviews, per-file or per-symbol summaries, no restating docs. — strong**
*Evidence:* overviews "not effective"; LLM-generated files −0.5 to −2 points
and +20% cost; harm came from redundancy; Claude Code excludes file-by-file
descriptions. This backs the cut of per-symbol descriptions (freshness.md).
Generated `flow` and `contract` kinds must justify themselves in the eval,
because the only measurement of generated repo context was negative.

**A2. Never serve stale or doubtful content as fact; default to omission. — strong**
Every item carries freshness. Items stale beyond a threshold, or edges below
the proof bar, are dropped or shown only as "unverified: check with grep"
leads, never mixed in with facts. Do *not* follow CodeGraph in telling
agents to "trust results rather than re-verify" unless the answer is proven
current.
*Evidence:* ClashEval >60% adoption of wrong context; misleading comments
leave about 25% of localizations intact; STALE 55.2%; Cherny on index
staleness.

**A3. Precision over recall in *ranked* answers; small default budget. — medium-strong**
Default responses around 1–3K tokens, with `concise`/`detailed` and
pagination, far under Claude Code's 25K cap. One hop by default. Never pad
with "similar code".
*Evidence:* Cognition's F0.5 rationale; RepoGraph 1-hop (2.3K tokens) beat
2-hop; similar code −15%; context rot and distractors; CodeGraph left 80%
more context behind. **Tension with R1:** impact lists must be *complete*.
Resolve it by making completeness a property of the list (a count, plus a
grouped summary when long, plus a cursor) rather than dumping all snippets.

**A4. Don't compete with grep on single-repo "where is X"; never force recall first. — medium-strong**
Position recall as the answer to structural and cross-app questions. Tool
descriptions should say *when* to use it ("before changing a queue, route,
table, env var or exported symbol used by another app") instead of
"always use first".
*Evidence:* Sourcegraph −0.080 below 400K LOC **[vendor]**; forced LSP
100→89%; LocAgent +3.6 over plain OpenHands; Cherny; Vercel's "MUST invoke"
wording backfired.

**A5. Few, distinct, task-shaped tools. — medium**
A handful of tools (impact, stack/flow, how-to-run, versions, note), each
with a clear trigger. Keep schemas small.
*Evidence:* Anthropic on bloated tool sets; RAG-MCP 13.6% tool-selection
accuracy with everything loaded; Sourcegraph saw extra tools dilute context;
150K→2K schema tokens.

**A6. Fail loud and cheap. — medium**
If recall is asleep, half-built or broken, say so in one short line and let
the agent fall back to its own tools. Never return empty answers that look
authoritative.
*Evidence:* Sourcegraph's "MCP death spiral" (0.0 against baseline 1.0 when
mirrors broke).

### Design tensions to settle in answers.md and eval.md

**T1. Passive vs on-demand. — medium**
Present context gets used (Vercel 100% vs skills 53–79%; skill never
invoked 56%; LSP chosen 0–6%). On-demand MCP tools get ignored. But recall
"never edits agent files". Options:
- MCP `initialize` instructions;
- a tiny always-present resource or prompt with the stack's pointer index
  (apps, modules, run commands, versions; Vercel-sized, ≤8KB);
- a one-line snippet the person pastes into CLAUDE.md themselves.

Measure tool-adoption rate in the eval. Subagents don't see MCP instructions
(CodeGraph), so the pasted line may matter more than expected.

**T2. Runtime state. — weak**
Plausibly valuable (ungrounded RCA, incident-debugging +11.2%
**[vendor]**), but unmeasured on code tasks and cut from v1. Revisit only if
the eval shows a failure class it would fix. Doze already holds app logs.

**T3. Conventions. — weak**
Prose conventions are often ignored (67% non-compliance on logging). Prefer
surfacing the project's *checks* (lint and test commands, which hooks run)
through R3. Keep `conventions` generation only if the eval rewards it.

### Eval requirements (for eval.md) — strong

- With and without recall, inside real harnesses (Claude Code, Codex,
  Cursor). The harness changes results by up to 16 points.
- Include **small stacks** (<400K LOC) as a control, where tools can hurt.
- Primary tasks: **cross-app changes scored by hidden tests in the
  dependent app** (DepBench design: visible tests pass, hidden ones catch
  incomplete propagation); pinned-version API tasks; run/verify tasks.
- Measure success, *completeness of touched sites* against ground truth,
  tokens and cost, turns, **tool adoption rate**, and a **stale-context
  injection** arm (deliberately stale edges or notes) to check A2.
- Report negative and flat results. Every vendor number above lacks
  independent replication, and the only independent study of generated
  context found harm.

---

## Sources (opened)

- https://arxiv.org/html/2602.11988v1 — Gloaguen et al., Evaluating AGENTS.md (ICLR 2026)
- https://arxiv.org/abs/2601.20404 — Lulla et al., AGENTS.md efficiency
- https://cursor.com/blog/semsearch — Cursor, Improving agent with semantic search (Nov 2025)
- https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents — Anthropic (Sep 2025)
- https://www.anthropic.com/engineering/writing-tools-for-agents — Anthropic (Sep 2025)
- https://www.anthropic.com/engineering/code-execution-with-mcp — Anthropic (Nov 2025)
- https://code.claude.com/docs/en/best-practices — Claude Code best practices
- https://officechai.com/ai/claude-researcher-explains-how-agentic-search-performed-better-than-rag-for-code-generation/ — Cherny on agentic search vs RAG (May 2025)
- https://arxiv.org/abs/2503.09089 and https://arxiv.org/html/2503.09089v2 — LocAgent
- https://arxiv.org/abs/2410.14684 and https://arxiv.org/html/2410.14684v2 — RepoGraph
- https://arxiv.org/abs/2502.00350 — OrcaLoca
- https://arxiv.org/abs/2503.22424 — CoSIL
- https://arxiv.org/abs/2407.01489 — Agentless
- https://arxiv.org/abs/2509.16941 and https://arxiv.org/html/2509.16941 — SWE-bench Pro
- https://arxiv.org/abs/2506.12286 — The SWE-Bench Illusion
- https://arxiv.org/abs/2511.00197 — Success and failure trajectories
- https://github.com/San-Lins/swe-failure-taxonomy — Near-Misses failure taxonomy
- https://arxiv.org/html/2603.03194 — BeyondSWE
- https://arxiv.org/html/2608.30300v1 — DepBench, "Update from Hell"
- https://codex.danielvaughan.com/2026/09/01/depbench-dependency-upgrade-hidden-breakage-coding-agents-codex-cli/ — DepBench summary
- https://arxiv.org/html/2603.27277v1 — Codebase-Memory
- https://github.com/colbymchenry/codegraph — CodeGraph
- https://www.agentconnect.md/blog/grep-beat-lsp-harness/ — grep vs LSP pilot (Aug 2026)
- https://arxiv.org/abs/2605.15184 — Is Grep All You Need?
- https://arxiv.org/abs/2608.11386 — The Devil Is in the Interface
- https://sourcegraph.com/blog/codescalebench-testing-coding-agents-on-large-codebases-and-multi-repo-software-engineering-tasks — CodeScaleBench (Mar 2026)
- https://sourcegraph.com/blog/why-coding-agents-fail-large-codebases — Sourcegraph (May 2026)
- https://codex.danielvaughan.com/2026/06/14/400k-loc-threshold-codescalebench-agent-failure-patterns-codex-cli-large-codebase-context-infrastructure/ — CodeScaleBench summary
- https://www.augmentcode.com/blog/context-engine-mcp-now-live — Augment Context Engine MCP (Feb 2026)
- https://cognition.com/blog/swe-grep — Cognition SWE-grep (Oct 2025)
- https://aider.chat/2023/10/22/repomap.html — Aider repo map
- https://vercel.com/blog/agents-md-outperforms-skills-in-our-agent-evals — Vercel (Jan 2026)
- https://arxiv.org/abs/2507.12367 and https://arxiv.org/html/2507.12367v2 — GitChameleon 2.0
- https://arxiv.org/html/2604.09515v1 — Knowledge conflicts from evolving APIs
- https://arxiv.org/abs/2503.20589 — What to retrieve for RACG (ICSE 2026)
- https://arxiv.org/abs/2404.10198 — ClashEval
- https://arxiv.org/html/2504.04372v1 — How accurately do LLMs understand code (misleading comments)
- https://arxiv.org/abs/2605.06527 — STALE
- https://www.trychroma.com/research/context-rot — Chroma, Context Rot (Jul 2025)
- https://arxiv.org/abs/2307.03172 — Lost in the Middle
- https://arxiv.org/abs/2302.00093 — LLMs easily distracted by irrelevant context
- https://arxiv.org/abs/2505.03275 — RAG-MCP
- https://arxiv.org/abs/2606.26978 — To Run or Not to Run (ISSTA 2026)
- https://arxiv.org/abs/2606.27154 — OpenRCA 2.0
- https://arxiv.org/abs/2601.13943 — RepoGenesis
- https://arxiv.org/abs/2409.20550 — Hallucinations in practical code generation (abstract only, not used for numbers)
- https://arxiv.org/html/2604.09409v1 — Do AI coding agents log like humans
- https://arxiv.org/abs/2511.04427 — Cursor adoption difference-in-differences
- https://arxiv.org/abs/2504.02605 — Multi-SWE-bench (abstract only)

Known only from search summaries (not opened; numbers cited in the text are
labeled): the "We Have a Package for You" figures (5.2% / 21.7%) and their
2026 replication; SWE-Next's 8.7%; the OpenRCA v1 11%; EnvBench's 6.69% /
29.47%; GitHub Copilot's embedding numbers (the github.blog post was not
opened); "When Comments Mislead" (the ACM page returned 403).
