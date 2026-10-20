# 6. Answer assembly and MCP tool design for doze recall

Research for `docs/design/recall/answers.md`. Written 2026-10-20. Question: how should
recall turn an agent's question into an answer agents actually use and do better with
(anchoring, expansion, ranking, packing to a budget), and how should the MCP tools look?
Constraints: no LLM at serve time, no embeddings; graph + SQLite FTS5 + Zoekt only.

Every number below was read from the source page on 2026-10-20 unless marked
"secondhand" or "unverified". Dates are the source's publication date.

---

## 1. Summary of the evidence

1. **Less, sharper context beats more context, and the curve turns down early.**
   SWE-bench's BM25 baseline (Claude 2) resolved 1.96% at 13k tokens, 1.87% at 27k,
   1.22% at 50k, while recall of the gold files *rose* from 29.6% to 51.1%
   (Jimenez et al., 2023). RepoGraph's 1-hop flattened subgraph (~2.3k tokens) kept
   the full gain; 2-hop flattened (~10.5k tokens) fell *below the no-graph baseline*
   (26.0 vs 27.3) (Ouyang et al., ICLR 2025). Chroma's context-rot study (July 2025,
   18 models) found every model did significantly better on ~300-token focused
   prompts than on ~113k-token full ones, and that a single distractor measurably
   hurts, four compound it.
2. **Graph + lexical search can match or beat embeddings when an agent drives it.**
   LocAgent (Mar 2025): plain BM25 gets 61.7% file Acc@5 on SWE-bench Lite, the
   CodeRankEmbed dense retriever 84.7%, but LocAgent (BM25 over entity IDs + a typed
   graph + three tools) reaches 92.7% (fine-tuned 32B) / 94.2% (Claude 3.5). Removing
   its keyword search tool costs ~19 points; removing the BM25 index costs ~13;
   removing graph traversal costs ~5 at function level.
3. **Raw BM25 for natural-language-to-code retrieval is weak, and that is the risk
   recall carries by refusing embeddings.** CoIR (2024): BM25 averages 29.8 nDCG@10
   vs 56.3 for Voyage-Code-002. Cursor (Nov 2025) reports +12.5% average answer
   accuracy from its semantic search. Identifier-aware tokenization closes much of
   the lexical gap (a May 2026 preprint reports BM25 on CodeSearchNet-Go going from
   0.258 to 0.487 nDCG@10 with a query-side fix, and says identifier-aware
   tokenization "largely removes" the remaining gain). The no-embedding bet is viable
   only if anchoring leans on exact names, the graph, and human/generated glossary
   text, and if the agent can iterate.
4. **Agents do not use MCP tools unprompted reliably.** The codebase-memory-mcp
   issue #69 (Mar 2026): "Claude never use codebase-memory-mcp except if I edit the
   CLAUDE.MD file". The maintainer's fix was keyword-rich tool descriptions, a
   PreToolUse hook and an installer. With Claude Code's tool search on by default,
   only tool *names and server instructions* load at session start; server
   instructions are cut at 4,096 chars. Codex says to keep the first 512 chars of
   instructions self-contained. Recall cannot edit agent files (decided), so the
   server `instructions`, tool names/descriptions, and the optional AGENTS.md
   snippet carry all of adoption, and adoption must be measured.
5. **Tool design rules with numbers.** Anthropic (Sep 2025): consolidate into
   task-shaped tools, a `response_format` of concise (72 tokens) vs detailed (206),
   Claude Code caps tool output at 25,000 tokens (warns at 10,000), errors must teach.
   SWE-agent (2024): capped, summarized search (max 50 hits, else "be more specific")
   beat iterative search by 6 points; a 100-line window beat 30 lines and the full
   file. LocAgent: tree-rendered subgraphs beat JSON and rows (79.4 vs 76.5 vs 70.6
   file Acc@5).
6. **"Already sent" is dangerous.** Claude Code micro-compaction and the API's
   tool-result clearing (keep last 3 tool results by default after 100k tokens)
   silently remove earlier tool output; the server is not told. Server-side
   "skip what this session already received" will hand an agent a pointer to content
   it no longer has.
7. **Labels are weak protection.** ClashEval (NeurIPS 2024): models adopt wrong
   retrieved content over their own correct prior >60% of the time. Prompt-only
   credibility labels helped gpt-3.5 by 3 to 18 points on noisy sets but the
   authors conclude models are "not inherently sensitive to credibility" in the
   prompt. So: separate unconfirmed items structurally, do not just tag them.

---

## 2. Ranking and packing: what the prior art does

### 2.1 Aider repo map (the closest working analogue)

Read from `aider/repomap.py` (main branch, fetched 2026-10-20) and the Oct 2023 post.

- Tags: tree-sitter `tags.scm` queries yield `def` and `ref` tags per file; cached
  per file by mtime in a SQLite-backed diskcache.
- Graph: nodes are **files**; for every identifier both defined and referenced, an
  edge from each referencing file to each defining file. Weight =
  `mul * sqrt(num_refs)`, with
  - identifier mentioned in the chat: `x10`
  - "meaningful" identifier (snake/kebab/camel and length >= 8): `x10`
  - starts with `_` (private): `x0.1`
  - defined in more than 5 files (ambiguous, e.g. `get`, `run`): `x0.1`
  - referencer is a file already in the chat: `x50`
  - definitions with no references get a self-loop of 0.1.
- Personalization vector: `100/len(files)` added for chat files, files whose path
  matches a mentioned filename, and files whose path component or stem matches a
  mentioned identifier. `nx.pagerank(G, weight, personalization, dangling=personalization)`
  with default damping (0.85).
- Rank is pushed from files onto `(file, identifier)` definitions in proportion to
  edge weight, then sorted.
- Packing: binary search over "how many top tags to render" to fit `max_map_tokens`;
  accepts a result within 15% of the budget (it can exceed the budget by up to 15%).
  Starts at `budget/25` tags (assumes ~25 tokens per tag line).
- Budget: default 1,024 tokens; multiplied by 8 (`map_mul_no_files`) when no files
  are in the chat yet, capped at context window minus 4,096. So: ~1k when the task
  is focused, ~8k when cold.
- Rendering: file headers + the definition lines (signatures), with `⋮...` elisions.
- Anchoring without embeddings: the "mentioned identifiers" are just the words in
  the user's messages that match identifiers. That is the whole trick, and it works.

Independent check: Agent Retrieval Bench (Jul 2026, 427 samples, 25 repos) found
"RepoMap gives the best budgeted context yield at 8K tokens", ahead of Qwen3
embeddings, which won on MRR and Recall@20. Logged agent trajectories missed every
gold file on 27 to 35% of samples, so a good seed helps: retrieval-chosen initial
context gave higher file F1 and less post-seed exploration than random context.

What recall should copy: personalized PageRank seeded by anchors; down-weight
private and multiply-defined names; up-weight long, specific identifiers; signature
lines as the default rendering; a token-measured fit. What to change: recall's graph
has typed nodes (symbols, routes, queues, topics, tables, env vars, apps) and typed
edges, so PageRank should run over symbols and stack nodes, not files, with per-edge-
type weights; and the budget must be a hard ceiling, not +15%.

### 2.2 Localization pipelines

- **Agentless** (Jul 2024): hierarchical narrowing: repo tree -> files -> skeletons
  (class/function headers) -> edit locations. Skeletons beat full files for choosing
  elements (58.3% at 698 LoC vs 53.7% at 778 LoC, and 7x cheaper). Combining LLM
  file ranking with embeddings gave the best file recall (81.7% vs 78.7% / 70.3%),
  meaning lexical-ish and semantic signals find different files. End-to-end: 32.0%
  SWE-bench Lite at $0.70.
  Lesson: show skeletons (signatures), not bodies, until the agent narrows.
- **LocAgent** (Mar 2025, ACL 2025): graph of directory/file/class/function with
  contain/import/invoke/inherit edges; a sparse hierarchical index:
  (1) fully qualified entity IDs (`src/utils.py:MathUtils.calculate_sum`),
  (2) short-name -> nodes dictionary, (3) BM25 over entity IDs for fuzzy matching,
  (4) BM25 over code chunks mapped back to entities. Tools: `SearchEntity` (full code
  when <= 3 matches, skeleton for large files, IDs only when > 3 matches),
  `TraverseGraph` (typed BFS, direction, hops, entity and relation filters, tree
  output), `RetrieveEntity` (full attributes). Numbers in section 1.
  Lesson: recall's anchoring index is exactly this, plus stack nodes; the
  adaptive-detail rule (full when few, IDs when many) is a packing rule worth copying.
- **RepoGraph** (Oct 2024, ICLR 2025): line-level def/ref graph, k-hop ego graphs
  around a search term. +2.0 to +2.7 points resolve rate across four frameworks
  (32.8% average relative). k=1 flattened: 2,311 tokens, full gain. k=2 flattened:
  10,505 tokens, *worse than baseline*. Authors stopped at k=2 because of "the
  potential introduction of noise".
  Lesson: default to 1 hop inside code; reach further only along typed, high-value
  edges (cross-service joins), and summarize rather than flatten wide neighbourhoods.
- **RepoCoder** (Mar 2023, EMNLP 2023): a Jaccard bag-of-words sparse retriever
  "achieves equivalent performance" to UniXcoder dense embeddings for repo-level
  completion (56.8 vs ~56.3 EM). Iterating retrieval with the draft output helps
  (40.6 in-file -> 55.3 -> 56.8 -> 57.0), flat after 2 to 3 rounds. Retrieved code
  gets half the prompt. Lesson: for code-to-code similarity, lexical is as good as
  dense; let the agent's follow-up call (with real identifiers it just saw) be the
  "second iteration".
- **CodeRAG** (Apr 2025, bigraph): requirement graph linked to a code graph, with
  linked code nodes as anchors for agentic traversal; +40.9 Pass@1 on DevEval for
  GPT-4o vs no retrieval. Lesson: anchors first, then graph walking, then code.
- **"Code Isn't Memory"** (Jun 2026): a structural index (graph + BM25 + vectors)
  exposed as two tools (`codebase_search`, `codebase_graph` returning callers and
  callees grouped by direction with distance) raised resolve from 41.9% to 50.4%
  (p=0.003) and localization acc@5 from 44.3% to 84.5% with Claude Opus 4.7, with
  fewer turns (28.3 vs 36.2) and fewer tokens, and no cost penalty. The paper does
  not report how often the tools were called or how the agent was told about them.
  Includes vectors, so not proof of lexical-only, but the graph tool shape matters.
- **Sourcegraph Cody**: Feb 2024 moved Enterprise from embeddings to its own search
  (reasons: code sent to a third party, vector ops overhead, scale past 100k repos).
  Two stages: recall-oriented retrieval from several sources, then precision-oriented
  ranking to a token budget (RecSys 2024 paper). April 2025: BM25F in Zoekt with
  filename and symbol matches boosted x5 in term frequency, file-level ranking then
  line-level ranking of chunks inside a file; ~20% improvement on internal metrics;
  "ranking is not very sensitive to the exact boost". Zoekt also down-ranks test,
  vendored and generated files and boosts ctags symbol matches.
  Lesson: recall's FTS5 should be BM25F-shaped: `name`, `qualified`, `signature`
  weighted above `doc` and `body`.
- **CodeCompose** (Meta, 2023): 22% acceptance across 9 languages; relevant mostly as
  proof that local context dominates for completion. Not a ranking source.

### 2.3 Lexical retrieval for code: what works without embeddings

- Identifier splitting: index both the whole identifier and its sub-tokens
  (`createOrder` -> `createorder`, `create`, `order`). A 2026 preprint (Radha and
  Goktas, arXiv 2605.18561) reports identifier-aware tokenization removes most of the
  gap its query-side fix closes on CoIR Go (BM25 0.258 -> 0.487 nDCG@10 for the fix).
  Single unreplicated preprint; the direction matches the older ICPC 2011 identifier-
  splitting work. Sub-tokens only, without the whole identifier, did worse per a
  secondhand summary. FTS5 needs a custom tokenizer (or pre-split text in a shadow
  column) to do this; codebase-memory-mcp does exactly this with a custom FTS5
  tokenizer.
- Document expansion (doc2query): ~15% over BM25 in the original 2019 work;
  query2doc 3 to 15%. For code, a Jul 2026 study (arXiv 2607.11046) found raw code
  beat every generated representation for BM25 file localization (Hit@5 0.676 on
  SWE vs 0.440 for role summaries, 0.378 technical summaries, 0.094 synthetic bug
  reports), but role summaries helped as an *addition* in Agentless (+2.2 points
  Hit@6). Lesson: index generated glossary/flow/contract text as extra fields
  pointing at nodes, never instead of code; synthetic bug reports are noise.
- NoLiMa (ICML 2025): when the question shares no literal words with the needle,
  GPT-4o drops from 99.3% to 69.7% at 32K; 11 of 13 models fall below half their
  short-context score. Literal overlap is what models (and BM25) find easily. The
  glossary layer exists to bridge user vocabulary ("checkout") to code vocabulary
  (`orders.Create`, queue `order-events`), deterministically.
- Graph proximity as a relevance signal: HippoRAG (NeurIPS 2024) uses personalized
  PageRank seeded on query entities; up to +20% on multi-hop QA and 10 to 30x cheaper
  than iterative retrieval. Recall can do the same without embeddings because its
  "entities" are exact names.

### 2.4 Context size and placement

- Lost in the Middle (2023, TACL 2024): gpt-3.5 with 20 documents: 75.8% when the
  answer is first, 53.8% in the middle, 63.2% last; middle is *below closed-book*
  (56.1%). Beyond 20 retrieved documents, +1.5% at most.
  Placement rule: the most important item first, the next-step guidance last.
- Chroma context rot (Jul 2025): performance drops with length in every experiment;
  lower question-needle similarity degrades faster; distractors hurt and compound;
  coherent haystacks were harder than shuffled ones (so a wall of plausibly related
  code is the worst case).
- Anthropic context engineering (Sep 2025): "smallest set of high-signal tokens";
  just-in-time retrieval with lightweight references (paths, ids) that the agent
  expands; sub-agents return 1,000 to 2,000-token summaries.
- Irrelevant code specifically: MRCoder (Jul 2026) and a 2026 repo-QA benchmark
  report redundant/hard-negative context degrades code generation; RepoExec (NAACL
  2025) is the counterpoint: cutting real dependencies misleads. Net: precision
  inside the answer, completeness of the *dependency chain that matters*.

Observed budget points: Aider 1k (focused) / 8k (cold); Agent Retrieval Bench best
yield at 8k; RepoGraph's good variant 0.7k to 2.3k; SWE-bench BM25 best at the
smallest tested (13k); sub-agent summaries 1 to 2k; Claude Code warns at 10k and
cuts at 25k; Codex lets users set a per-tool `output_token_limit`.

---

## 3. Tool design for agents

### 3.1 What the guidance says

Anthropic, "Writing effective tools for agents" (11 Sep 2025):
- Few, task-shaped tools ("search_logs not read_logs", "get_customer_context" over
  three lookups); overlapping tools confuse.
- Namespacing by service; prefix vs suffix effects vary by model, test it.
- Return high-signal fields, resolve opaque IDs into names.
- `response_format: "concise" | "detailed"`; their example 72 vs 206 tokens; detailed
  includes the IDs later calls need.
- Pagination, filtering, truncation with sensible defaults; when truncating, tell
  the agent how to narrow. Claude Code: 25,000-token tool-output cap.
- Errors: specific, actionable, with a correct example input; no tracebacks.
- Descriptions as for a new hire; unambiguous parameter names; small description
  edits moved Claude 3.5 Sonnet to SOTA on SWE-bench Verified.
- Evaluate with realistic tasks, read transcripts; many redundant calls means
  pagination/limits are wrong; invalid-parameter errors mean descriptions are wrong.

Anthropic advanced tool use (24 Nov 2025): 58 tools across 5 servers cost ~55k
tokens; tool search raised MCP-eval accuracy 49% -> 74% (Opus 4) and 79.5% -> 88.1%
(Opus 4.5); defer when > 10 tools or > 10k tokens of definitions; keep 3 to 5
most-used tools loaded; tool-use examples raised complex-parameter accuracy 72% -> 90%.

RAG-MCP (May 2025): with many MCP schemas in the prompt, tool selection accuracy was
13.6%; retrieving only the relevant schema raised it to 43.1%. Cursor historically
sent only the first 40 MCP tools to the agent (forum reports; current behaviour
unverified). Six tools are well inside every limit.

SWE-agent ACI (May 2024, NeurIPS 2024): simple actions with few options; compact
actions; concise informative feedback; guardrails. Ablations on SWE-bench Lite
(GPT-4 Turbo, 18.0% full): iterative search 12.0%, no search 15.7%; 30-line window
14.3%, full file 12.7%; full history instead of last-5 observations 15.0%.
"Iterative search often led agents to step through every match". A search returning
more than 50 hits returned nothing but a request to be more specific.

"The Devil Is in the Interface" (Aug 2026, 11,700 trajectories): interface shape
alone changed consistency up to 4.7x; natural-language search widened access to
relevant files by >11%; CodeAct-style interfaces used 41.6% fewer steps.

MCP spec 2025-11-25: `outputSchema` + `structuredContent` (and "SHOULD also return
the serialized JSON in a TextContent block" for compatibility), `resource_link`
content, `annotations` (readOnlyHint etc., untrusted unless the server is trusted),
`isError: true` tool execution errors "contain actionable feedback that language
models can use to self-correct", protocol errors for malformed calls, `listChanged`.

### 3.2 Will agents call recall at all?

- Claude Code (docs fetched 2026-10-20): tool search on by default; at session start
  Claude sees tool names and server instructions only. "Server instructions help
  Claude understand when to search for your tools, similar to how skills work."
  Instructions and upfront tool descriptions are cut at 4,096 chars; deferred tool
  descriptions at 16,384; "put the words Claude would search for near the start".
  A tool can ask to load upfront via `_meta: {"anthropic/alwaysLoad": true}`.
  Over 10,000 tokens of output: warning; over 25,000 (or 50,000 chars): saved to a
  file and the model gets a path.
- Codex: uses the `instructions` field "as server-wide guidance alongside the
  server's tools"; "Keep the first 512 characters self-contained". Default tool
  timeout 60 s. `default_tools_approval_mode = "writes"` prompts for tools not marked
  read-only, so `readOnlyHint: true` is what lets recall run without prompts there.
- Field reports: codebase-memory-mcp #69 (Mar 2026) and others: agents ignore code-
  graph MCP tools and grep instead unless CLAUDE.md says otherwise; fixes were
  keyword-rich descriptions and hooks. Claude Desktop reportedly stores but ignores
  `instructions`; some SDK adapters drop it. Serena ships an `initial_instructions`
  tool for clients that do not read instructions.
- Agent documentation behaviour (Aug 2026, 557 sessions): 60.5% of documentation
  interactions are with agent instruction files and working notes; 70.2% of
  consultations self-initiated. Instruction files are what agents read.
- Anthropic's own position: Claude Code's search is "glob and grep" because agentic
  search beat a vector RAG in their testing (Boris Cherny, secondhand, 2025). Recall
  competes with grep, so each answer must save the agent several greps, or it will
  be skipped after the first try.

Implication: adoption is designed, not assumed. Levers recall controls: the
server `instructions` (first 512 chars carry the "when"), tool names that read as
verbs an agent would search for, the first sentence of each description listing the
trigger words (impact, callers, who consumes, which queue, route, env var, breaks,
error), `alwaysLoad` on `brief` only, and the optional AGENTS.md section from the
Connect tab. Plus one lever that depends on quality: the first answer must beat
grep visibly (cross-service facts grep cannot see).

### 3.3 Session dedupe under compaction

- Claude Code micro-compaction replaces old tool results with a placeholder
  (third-party descriptions; thresholds disagree across sources); auto-compaction
  replaces history with a summary. The API's `clear_tool_uses_20250919` defaults to
  trigger at 100,000 input tokens and keep the 3 most recent tool results, replacing
  older ones with a placeholder. Context editing gave +29% on Anthropic's agentic
  search eval and 84% fewer tokens on a 100-turn task (Sep 2025), so clients will keep
  doing this.
- The MCP session survives all of this; the server sees no signal. A server-held
  "already sent" set will, after any compaction, answer "see earlier" to an agent
  that has no earlier.
- A known failure in the other direction: one oversized tool result re-fetched after
  every compaction causes compaction thrash (third-party report, Jul 2026). Small
  answers avoid both.

Design consequence: dedupe must be **client-held** (the agent passes back a token
it still has) or **content-preserving** (repeat items as one-line stubs carrying
id + `file:line` + one-line fact, never as "already sent"). See 5.6.

### 3.4 Evidence formats and labels

- `file:line` (or `file:start-end`) is what agents act on: the next step is Read with
  an offset, or Edit. SWE-agent's 100-line window result suggests snippet windows of
  tens of lines, not files.
- ClashEval: wrong retrieved content overrides correct priors >60% of the time; less
  likely when the error is blatant. Recall's wrong edges will be subtle, so they will
  be believed.
- Credibility-aware generation (EMNLP 2024): labels in the prompt help some (gpt-3.5:
  RGB 0.773 -> 0.807, EvolvingTempQA 0.579 -> 0.673, NewsPollutedQA 0.231 -> 0.408)
  but did not help on every set and fine-tuning helped far more. Modern models are
  likely better, unmeasured.
- So recall should rely on structure: proven facts in the body, doubtful ones in a
  separate trailing section with the check to run ("open api/client.go:88 to
  confirm"), and never a doubtful edge on the main path of `impact` or `trace`
  without a marker *in the line itself*. Matches the README principle "no edge rather
  than a wrong edge".

---

## 4. Anchoring natural-language task text to graph nodes, without embeddings

What works, in order of precision (each grounded above):

1. **Exact mentions** (Aider's mentioned identifiers, LocAgent's ID index): regex the
   input for paths (`orders/create.go`, `orders/create.go:42`), qualified symbols
   (`orders.Create`, `OrderService.Create`), bare identifiers (camel/snake/kebab,
   length >= 4), routes (`POST /orders`, `/orders/{id}`), queue/topic/table/env names
   from the stack (the stack's names are a closed vocabulary: match them as phrases),
   app names, error codes and log message fragments (for `diagnose`, match against
   string literals indexed per symbol). Exact hits get the top seed weight.
2. **Short-name dictionary**: bare `Create` maps to every `*.Create`; weight by
   1/count (Aider's x0.1 when > 5 definitions), break ties by proximity to other
   anchors.
3. **BM25F over identifier-split fields** (Sourcegraph BM25F, identifier-aware
   tokenization): FTS5 columns with `bm25()` weights, for example name 5, qualified
   path 3, signature 2, doc comment 1.5, note/glossary/flow text 1.5, string literals
   1, body tokens 0.5 (body via Zoekt or a reduced token set). Tests, generated and
   vendored code at x0.3. Index whole identifiers and sub-tokens.
4. **Glossary expansion (the doc2query substitute)**: human glossary entries and
   generated glossary/flow/contract text are indexed as fields that point at nodes.
   A query word that hits a glossary term adds that term's linked nodes as seeds at
   reduced weight, labeled with the layer it came from. Deterministic, reviewable,
   and the only bridge across vocabulary gaps recall has.
5. **Graph proximity**: run personalized PageRank from the seeds over the typed graph
   (damping 0.85, a few dozen iterations, restricted to a k-hop neighbourhood for
   speed). Seeds that are near each other reinforce; an isolated weak lexical hit
   decays. This is also how two vague words ("refund" + "email") find the one
   consumer that joins them.
6. **Ambiguity is an answer**: if the top seeds are close in score and in different
   apps, or no seed clears a floor, return the candidates (name, kind, app,
   `file:line`, one-line signature) and ask the agent to pick, instead of guessing.
   LocAgent's "IDs only when > 3 matches" rule.

What does not work, or is not worth it: synthetic bug reports as index text
(0.094 Hit@5); raw BM25 over whole files for NL questions (61.7% vs 84.7% dense);
flattening 2-hop neighbourhoods.

The risk to measure (section 6): anchor precision@1 and recall@5 against gold files
for NL-only task text. If it is far below ~80% file recall@5, the glossary layer and
`find` iteration are what to improve, not more hops.

---

## 5. Recommendation for recall

### 5.1 The answer pipeline (identical for every tool, parameterised per tool)

```
input ─► 1 parse      explicit anchors (paths, file:line, symbols, routes, stack names,
                       error text) + residual words
       ─► 2 seed      exact ids (w=1.0) > short-name dict (w=1/n) > BM25F hits (score-
                       normalised, w<=0.6) > glossary links (w<=0.4, layer-labeled)
       ─► 3 resolve   ambiguity check: candidates answer if top-2 within 15% and in
                       different apps, or best < floor
       ─► 4 freshen   stat files the candidate items cite; re-index changed ones
                       (freshness.md "read your own writes")
       ─► 5 expand    tool-specific typed walk (table below), 1 hop in code by default;
                       cross-service join edges (route<->client, producer->queue->
                       consumer, table, env) do not consume the hop budget
       ─► 6 rank      PPR from seeds over the expanded subgraph with edge-type weights,
                       x layer trust (proven 1.0, told 0.9, proposed 0.6), x penalties
                       (private x0.3, test/generated/vendored x0.3, >5 same-name defs
                       x0.1), stable tie-break on node id
       ─► 7 pack      group into sections; render each item at the richest level the
                       budget allows (id line ≈15 tok < signature ≈30 < skeleton <
                       snippet ≤ 20 lines); greedy by score/token with per-app and
                       per-file caps; hard ceiling, measured with a real tokenizer
                       estimate; overflow becomes a counted "more" line with a cursor
       ─► 8 render    lead line (the answer in one sentence) ▸ proven sections ▸
                       "unconfirmed" section ▸ "next" (2-3 suggested calls or files)
                       ▸ footer (generation, commit, stale count, truncated count)
```

Tool-specific expansion:

| Tool | Seeds | Walk | Sections |
|---|---|---|---|
| `brief` | task text or path/symbol | containment + 1 hop calls both ways; owning app; stack nodes touched; notes and flows anchored to seeds | Where it lives · How it connects (stack edges) · Notes/flows · Next |
| `find` | words or a name | none (search only) | Ranked matches: kind, app, `file:line`, signature; adaptive detail (≤3 hits: signatures + 10-line snippet; >3: one line each; >50: count + refine hint) |
| `impact` | symbol(s), file:line, or a diff | reverse refs/calls 1 hop in code, then every cross-service join transitively to consumers (bounded by 3 joins) | Direct callers · Other apps affected (via route/queue/topic/table/env) · Contracts/tests that cover it · Unconfirmed |
| `trace` | route, queue/topic, symbol, or flow name | forward along calls and joins, ordered, with branch points, depth cap 12 steps | Ordered path with `file:line` per step · Side effects (writes, publishes) · Unconfirmed hops |
| `topology` | app name or none | stack graph only, no code | Apps, what each exposes and consumes, who calls whom; whole stack ≤ 1.5k concise |
| `diagnose` | error text, stack trace, failing route, app name | match frames and log literals to symbols, plus doze runtime state (what runs, health, recent restarts) | Likely origin · Path to it · Runtime state · Next checks |

`review` as planned is `impact` over a diff (same engine, different input): fold it in
as `impact({diff: "git"})` or keep it as a thin alias only if the eval shows agents
call it at PR time. Six tools (`brief`, `find`, `impact`, `trace`, `topology`,
`diagnose`) is the recommended surface. `find` is added because LocAgent's largest
ablation drop (-19 points) was removing keyword entity search; without it, agents
fall back to grep for every name lookup and leave recall.

### 5.2 Budgets

| | concise (default) | detailed | hard cap |
|---|---|---|---|
| `brief` | 1,500 | 4,000 | 6,000 |
| `find` | 600 | 2,000 | 3,000 |
| `impact` | 1,500 | 4,000 | 6,000 |
| `trace` | 1,500 | 4,000 | 6,000 |
| `topology` | 1,000 per app / 1,500 stack | 4,000 | 6,000 |
| `diagnose` | 1,500 | 4,000 | 6,000 |

Rationale: RepoGraph's good variant sat at 0.7 to 2.3k and its 10.5k variant hurt;
Aider defaults to 1k with files in chat; sub-agent hand-backs are 1 to 2k; Agent
Retrieval Bench's best yield at 8k is for a cold start and is split across several
recall calls here; everything stays under Claude Code's 10k warning so no client
saves recall's output to a file. Per-call `max_tokens` override allowed up to the
hard cap. Starting values; the eval sets the final ones (budget is a column in the
eval matrix).

### 5.3 Item shape (rendered text; the same data in `structuredContent`)

```
impact orders.Create  (gen 412 · commit a3f9c1e · 0 stale)
Changing orders.Create affects 3 callers in api and 2 other apps through queue order-events.

Direct callers
  api   handlers/orders.go:57      func (h *Handler) PostOrder(w, r)        proven
  api   handlers/admin.go:112      func (h *Handler) ReplayOrder(...)       proven
  api   orders/create_test.go:20   TestCreate                               proven · test

Other apps affected
  orders.Create ─publishes─► queue order-events  (orders/create.go:88)       proven
    └─consumes─► worker  consumers/order_events.go:31  HandleOrderEvent     proven
    └─consumes─► mailer  jobs/receipt.go:14            SendReceipt          told: note by srini, 2026-09-02

Unconfirmed (check before relying on it)
  web   src/api/orders.ts:40  fetch("/orders") may call POST /orders     proposed · stale since 9b1d2e0
        verify: open src/api/orders.ts:40

More: 4 more callers (tests) · call impact {symbol:"orders.Create", cursor:"c2"}
Next: trace {from:"queue order-events"} · brief {path:"consumers/order_events.go"}
```

Rules: the lead sentence first and "Next" last (lost-in-the-middle); one item per
line with `app  file:line  signature  layer`; trees for paths (LocAgent's best
format); paths relative to the repo root the agent works in; line numbers from the
freshened index; no UUIDs, ids only where a follow-up call needs them; every
non-proven item says so on its own line, and unconfirmed items live in their own
section; counts of what was cut, with the exact call that gets more.

### 5.4 Example tool schemas

Server instructions (first 512 characters self-contained, total well under 4,096):

```
doze recall knows this stack: every app, route, queue, topic, table and env var, and
which code sends to or calls which. Use it BEFORE grep when a task touches more than
one function or app: brief (start of a task), impact (before changing a symbol, route,
queue or schema), trace (how a request or message flows), find (where is X defined),
topology (what apps exist and how they connect), diagnose (an error, failing route or
crashed app). Answers cite file:line; open those lines before editing.
<details below the first 512 chars: layers, staleness, cursors, limits>
```

`impact`:

```json
{
  "name": "impact",
  "title": "What breaks if this changes",
  "description": "Impact of changing a symbol, file:line, route, queue, topic, table, env var or the current diff: direct callers, and every other app affected through routes, queues, topics, tables and env wiring that grep cannot see. Call before editing shared code or contracts. Returns file:line evidence, each item labeled proven, told (human note) or proposed (unconfirmed).",
  "inputSchema": {
    "type": "object",
    "properties": {
      "target": {"type": "string", "description": "What will change. Examples: \"orders.Create\", \"orders/create.go:42\", \"POST /orders\", \"queue order-events\", \"env STRIPE_KEY\"."},
      "diff": {"type": "string", "enum": ["none", "worktree", "staged", "branch"], "default": "none", "description": "Use the uncommitted or branch diff as the target instead of `target`."},
      "response_format": {"type": "string", "enum": ["concise", "detailed"], "default": "concise", "description": "concise: one line per item (about 1.5k tokens). detailed: signatures and short snippets (about 4k)."},
      "max_tokens": {"type": "integer", "minimum": 300, "maximum": 6000},
      "cursor": {"type": "string", "description": "From a previous answer's 'More' line, to get the next page."},
      "since": {"type": "string", "description": "Optional. The generation token from an earlier answer you still have in context; unchanged items come back as one-line stubs. Omit if unsure."}
    },
    "additionalProperties": false
  },
  "annotations": {"readOnlyHint": true, "idempotentHint": true, "openWorldHint": false},
  "outputSchema": {
    "type": "object",
    "required": ["generation", "commit", "lead", "sections"],
    "properties": {
      "generation": {"type": "string"},
      "commit": {"type": "string"},
      "lead": {"type": "string"},
      "sections": {"type": "array", "items": {"type": "object", "properties": {
        "title": {"type": "string"},
        "items": {"type": "array", "items": {"type": "object", "properties": {
          "id": {"type": "string"}, "kind": {"type": "string"}, "app": {"type": "string"},
          "file": {"type": "string"}, "line": {"type": "integer"}, "end_line": {"type": "integer"},
          "text": {"type": "string"},
          "layer": {"type": "string", "enum": ["proven", "told", "proposed"]},
          "fresh": {"type": "string", "description": "current, or stale since <commit>"},
          "via": {"type": "array", "items": {"type": "string"}, "description": "edge path from the target"}
        }}}
      }}},
      "more": {"type": "object", "properties": {"count": {"type": "integer"}, "cursor": {"type": "string"}}},
      "next": {"type": "array", "items": {"type": "string"}}
    }
  }
}
```

`brief` (the one tool to mark `_meta: {"anthropic/alwaysLoad": true}`):

```json
{
  "name": "brief",
  "description": "Start-of-task briefing: given the task in plain words, or a path or symbol, returns where the relevant code lives, which apps, routes, queues, tables and env vars it touches, the team's notes and flows about it, and what to read first, all with file:line. Use instead of exploring with grep at the start of a task.",
  "inputSchema": {
    "type": "object",
    "properties": {
      "task": {"type": "string", "description": "The task or question in plain words, e.g. \"refund emails are sent twice\". Include any names you know."},
      "focus": {"type": "string", "description": "Optional path, file:line or symbol to center on."},
      "response_format": {"type": "string", "enum": ["concise", "detailed"], "default": "concise"},
      "max_tokens": {"type": "integer", "minimum": 300, "maximum": 6000}
    },
    "required": ["task"],
    "additionalProperties": false
  },
  "annotations": {"readOnlyHint": true, "idempotentHint": true, "openWorldHint": false},
  "_meta": {"anthropic/alwaysLoad": true}
}
```

`find`, `trace`, `topology`, `diagnose` follow the same shape: one required natural
input (`query`, `from`, `app?`, `error`), plus `response_format`, `max_tokens`,
`cursor`. `trace` adds `to` (optional stop node) and `direction` (`forward` default,
`backward`). `diagnose` accepts `error` (text, stack trace or log lines) and optional
`app`, `route`.

Errors that teach (`isError: true`, plain text):

```
No symbol, route or stack name matches "OrderCreate".
Closest: orders.Create (api, orders/create.go:42), orders.CreateDraft (api, orders/draft.go:10),
route POST /orders (api).
Try: impact {"target": "orders.Create"}
```

Ambiguity is *not* an error: it returns `isError: false` with a "Which one?" section
of candidates and the exact follow-up call for each. Index-not-ready is not an error
either: answer from the last index with the staleness label (freshness.md).

### 5.5 Determinism

Same input, same index generation: byte-identical output. Stable sort keys
(score rounded to 1e-6, then node id). The token estimate is a fixed function (for
example bytes/3.5 calibrated once per client family), not a model call. This makes
answers diffable in the eval and cacheable per `(tool, args, generation)`.

### 5.6 Session dedupe

- Default: no server-side omission. Every answer is self-sufficient.
- Agent-held `since` token: if the agent passes the generation token of an earlier
  answer it still holds, unchanged items render as stubs
  (`api handlers/orders.go:57 PostOrder · unchanged since g412`), changed items in
  full with "changed since g412". A compacted agent that lost the earlier answer
  also lost the token, and simply gets the full answer.
- Server-side, only one thing: when the same `(tool, args)` repeats within a session
  at the same generation, answer in full but prefix "same as your previous call at
  g412; nothing changed", which tells the agent a re-call is useless without hiding
  content (and is the redundant-call signal for the eval).
- Revise freshness.md's "uses the generation to skip what a session already
  received" to this.

### 5.7 Adoption plan (no agent files edited by doze)

1. Server `instructions` as in 5.4, with the trigger words in the first 512 chars.
2. Tool names are plain verbs (`brief`, `impact`, `trace`, `find`, `topology`,
   `diagnose`); clients namespace them (`mcp__doze-recall__impact`). Test against
   `recall_impact` style in the eval, per Anthropic's "prefix vs suffix varies".
3. Descriptions start with the triggers, mention "grep cannot see" for cross-app
   facts, include one example input each (tool-use examples: 72% -> 90%).
4. `alwaysLoad` on `brief` only; the rest deferred and found through instructions.
5. The Connect tab's optional AGENTS.md / CLAUDE.md snippet (already decided), five
   lines: when to use which tool, and "fall back to grep for literal strings".
6. Serena-style fallback for clients that drop `instructions`: an MCP prompt
   (`/doze-recall:brief`) so a person can start a session with it.

### 5.8 What to measure

Retrieval (offline, cheap, run on every change; gold = files/symbols touched by the
task's reference patch):
- Anchor precision@1 and file/symbol recall@5 from task text alone; split by "task
  names an identifier" vs "plain words only".
- Context yield at budget: share of answer tokens that point at gold items (Agent
  Retrieval Bench's measure); share of answer items that are distractors.
- Ambiguity rate and correctness of the candidate list.
- Answer tokens p50/p95 per tool; truncation rate; stale-item share; unconfirmed-item
  share and its precision (how many unconfirmed edges a person later approved).
- Determinism: identical bytes on rerun.

Agent behaviour (with-and-without eval, transcripts read):
- Adoption: share of sessions that call recall unprompted, per client (Claude Code,
  Codex, Cursor), with and without the AGENTS.md snippet, with tool search on/off.
- First tool call: recall vs grep; grep-after-recall on the same name (signals the
  answer was not trusted or not enough).
- Calls per task, repeated identical calls, invalid-argument errors, cursor use.
- Whether agents open the cited `file:line` before editing, and whether they act on
  unconfirmed items without checking (the label test).
- Outcomes: task success, localization acc@5, turns, total tokens, cost, wall clock;
  report against the no-recall arm with seeds and paired tests ("Code Isn't Memory"
  style: +8.5 pp resolve, 28 vs 36 turns is the size of effect worth having).
- Budget sweep: concise vs detailed vs 2x, to set the defaults in 5.2.
- After compaction: success on tasks long enough to compact, with `since` vs none.

---

## Sources

Ranking, retrieval, localization
- Aider, "Building a better repository map with tree sitter" (2023-10-22): https://aider.chat/2023/10/22/repomap.html
- Aider `repomap.py` (main, fetched 2026-10-20): https://github.com/Aider-AI/aider/blob/main/aider/repomap.py
- Zhang et al., RepoCoder (2023-03, EMNLP 2023): https://arxiv.org/abs/2303.12570
- Xia et al., Agentless (2024-07): https://arxiv.org/abs/2407.01489
- Chen et al., LocAgent (2025-03, ACL 2025): https://arxiv.org/abs/2503.09089
- Ouyang et al., RepoGraph (2024-10, ICLR 2025): https://arxiv.org/abs/2410.14684
- Li et al., CodeRAG: Supportive Code Retrieval on Bigraph (2025-04): https://arxiv.org/abs/2504.10046
- Jimenez et al., SWE-bench (2023-10, ICLR 2024): https://arxiv.org/abs/2310.06770
- Li et al., CoIR (2024-07): https://arxiv.org/abs/2407.02883
- Wang et al., CodeRAG-Bench (2024-06): https://arxiv.org/abs/2406.14497
- "Code Isn't Memory: A Structural Codebase Index Inside a Coding Agent" (2026-06): https://arxiv.org/abs/2606.22417
- Agent Retrieval Bench (2026-07): https://arxiv.org/abs/2607.24882
- "Retrieval-Oriented Code Representations in Agentic Bug Localization" (2026-07): https://arxiv.org/abs/2607.11046
- Radha and Goktas, "Improving BM25 Code Retrieval Under Fixed Generic Tokenization" (2026-05): https://arxiv.org/abs/2605.18561
- Dit et al., identifier splitting and feature location (ICPC 2011): https://www.cs.wm.edu/~denys/pubs/ICPC2011-IdentifierSplitting.pdf
- Nogueira et al., doc2query (2019): https://arxiv.org/abs/1904.08375
- Wang et al., Query2doc (EMNLP 2023): https://arxiv.org/abs/2303.07678
- Gutiérrez et al., HippoRAG (2024-05, NeurIPS 2024): https://arxiv.org/abs/2405.14831
- MRCoder (2026-07): https://arxiv.org/abs/2607.26805
- Sourcegraph, "How Cody understands your codebase" (2024-02-15): https://sourcegraph.com/blog/how-cody-understands-your-codebase
- Hartman et al., "AI-assisted Coding with Cody: Lessons from Context Retrieval and Evaluation" (RecSys 2024): https://arxiv.org/abs/2408.05344
- Tibshirani, "Keeping it boring (and relevant) with BM25F" (Sourcegraph, 2025-04-04): https://sourcegraph.com/blog/keeping-it-boring-and-relevant-with-bm25f
- Cursor, "Improving agent with semantic search" (2025-11): https://cursor.com/blog/semsearch
- Murali et al., CodeCompose (2023-05, FSE 2024): https://arxiv.org/abs/2305.12050
- codebase-memory-mcp (FTS5 camel/snake tokenizer), via: https://instagit.com/DeusData/codebase-memory-mcp/semantic-search-vs-bm25-full-text-search-mcp/

Context size
- Liu et al., Lost in the Middle (2023-07, TACL 2024): https://arxiv.org/abs/2307.03172
- Hong et al., Context Rot (Chroma, 2025-07-14): https://www.trychroma.com/research/context-rot
- Modarressi et al., NoLiMa (2025-02, ICML 2025): https://arxiv.org/abs/2502.05167
- Anthropic, "Effective context engineering for AI agents" (2025-09-29): https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents

Tool design and MCP
- Anthropic, "Writing effective tools for agents — with agents" (2025-09-11): https://www.anthropic.com/engineering/writing-tools-for-agents
- Anthropic, "Introducing advanced tool use" (2025-11-24): https://www.anthropic.com/engineering/advanced-tool-use
- Yang et al., SWE-agent (2024-05, NeurIPS 2024): https://arxiv.org/abs/2405.15793
- "The Devil Is in the Interface" (2026-08): https://arxiv.org/abs/2608.11386
- Gan and Sun, RAG-MCP (2025-05): https://arxiv.org/abs/2505.03275
- MCP specification 2025-11-25, Tools: https://modelcontextprotocol.io/specification/2025-11-25/server/tools
- Claude Code MCP docs (fetched 2026-10-20): https://code.claude.com/docs/en/mcp
- Codex MCP docs (fetched 2026-10-20): https://learn.chatgpt.com/docs/extend/mcp.md?surface=cli
- Cursor 40-tool limit (forum, 2025): https://forum.cursor.com/t/tools-limited-to-40-total/67976
- codebase-memory-mcp issue #69 (2026-03-17): https://github.com/DeusData/codebase-memory-mcp/issues/69
- Serena `initial_instructions`: https://glama.ai/mcp/servers/oraios/serena/tools/initial_instructions
- Gao and Chen, "From Agent Behaviour to Agent-Friendly Documentation" (2026-08): https://arxiv.org/abs/2608.20195
- Vercel, "The second wave of MCP" : https://vercel.com/blog/the-second-wave-of-mcp-building-for-llms-not-developers
- Secondhand reports of Boris Cherny on agentic search vs RAG (2025): https://smartscope.blog/en/ai-development/practices/rag-debate-agentic-search-code-exploration/ ; Jason Liu on Augment's SWE-bench agent (2025-09-11): https://jxnl.co/writing/2025/09/11/why-grep-beat-embeddings-in-our-swe-bench-agent-lessons-from-augment/

Compaction and dedupe
- Anthropic, "Managing context on the Claude Developer Platform" (2025-09-29): https://claude.com/blog/context-management
- Claude API context editing docs: https://platform.claude.com/docs/en/build-with-claude/context-editing
- Micro-compact descriptions (third party): https://claudelog.com/faqs/what-is-micro-compact/ ; compaction thrash: https://startdebugging.net/2026/07/fix-claude-code-autocompact-thrashing-on-large-file-or-tool-output/

Evidence and labels
- Wu, Wu and Zou, ClashEval (2024-04, NeurIPS 2024): https://arxiv.org/abs/2404.10198
- Pan et al., "Not All Contexts Are Equal: Credibility-aware Generation" (EMNLP 2024): https://arxiv.org/abs/2404.06809

Not verified / gaps: Sourcegraph's internal "20%" has no named metrics; the identifier-
tokenizer ablation numbers (0.309 -> 0.563) came from a search summary, the abstract
confirms only the direction; Cursor's current tool cap; exact Claude Code micro-
compaction thresholds; no study found that measures MCP tool call rates in coding-
agent trajectories directly, so recall's own eval has to.
