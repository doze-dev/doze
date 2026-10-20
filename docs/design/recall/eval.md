# How we prove recall helps

Status: design, 2026-10-20; revised the same day after an independent review
of its statistics and feasibility. Nothing here is implemented. Evidence in
[research/1](research/1-agent-context-needs.md) §7 and
[research/6](research/6-answer-assembly.md) §5.8.

Recall exists to make agents finish changes they now leave half done. Nobody
has measured that for changes across a declared multi-service stack: no
public benchmark covers producer → queue → consumer or client → route. So
recall has to bring its own evidence. Every tuned number in the other recall
documents (budgets, weights, the glossary bar) is a starting value until this
eval sets it.

The eval is the most expensive part of recall to do properly. §9 says what it
costs in money and in people's time, and §10 asks for the decisions that cost
depends on.

| Level | What it runs | When | Cost |
|---|---|---|---|
| Component tests | extractors against compiler truth, the join on fixtures, incremental = full ([graph.md](graph.md) §9) | every commit | seconds |
| Retrieval eval | the answer pipeline on task text, no agent, scored against each task's required sites | every change to ranking, anchoring, packing or extraction | minutes |
| Agent eval | real agent CLIs doing real tasks, with and without recall | E1 and E2 (§8), then on major changes | days of subscription usage |

## 1. What is measured, and what decides

### The primary measure

Fixed before any run, so the result can't be chosen to fit:

> **On cross-app tasks, in Claude Code on Sonnet, the per-task pass rate with recall
> (arm A2) minus without (A0).**

- **Unit:** the task. Each task gets k runs per arm. Its pass rate is the mean
  of those runs, and the paired difference is A2 − A0 for that task.
- **Estimate:** the mean of the per-task differences.
- **Interval:** 95%, by bootstrap over tasks (resampling tasks, keeping each
  task's runs together).
- **Ship rule for v1:** the interval's lower bound is above 0, and the point
  estimate is at least **+5 points**, the smallest gain worth shipping. The
  run deciding it is sized to detect **+8.5 points** (research/6's effect
  worth having, from the one independent study of a structural index inside
  a coding agent) with 80% power. A point-estimate bar set at the very effect
  the run is sized for would fail half the time even when that effect is
  real.
- **Which run decides:** whichever V1 chooses after E1 (§10). If V1 chooses
  E1 alone, the result ships labelled as a pilot result with its wide
  interval.

One primary measure means no correction for multiple comparisons is needed
for the ship decision. Everything below is secondary: reported with
intervals, used to decide what to fix, and never a pass/fail gate.

### Secondary measures

| # | Question | Measured by |
|---|---|---|
| S1 | Are changes more complete? | per task: required sites touched ÷ required sites (§3), A2 − A0 |
| S2 | When `impact` says complete, is it? | required sites reachable from the target and in scope that a "complete" answer didn't list. Reported as misses over claims, with a one-sided 95% upper bound. Below about 150 claims the bound can't get under 2%, so the count of claims is always shown |
| S3 | Does recall hurt where it shouldn't help? | the same difference on the single-file family, with its interval. No margin: a ±2-point claim would need thousands of tasks |
| S4 | Do agents use it, and is it the server or the pasted lines? | sessions calling recall unprompted, A1 (server only) and A2 (server + lines) |
| S5 | Does stale context mislead them? | A3 vs A2 difference, and the "acted on without checking" rate (§5) |
| S6 | What does it cost? | tokens, turns, wall time per task |
| S7 | Other harnesses | the primary measure repeated in Codex and Cursor's CLI in E2, each reported separately, never pooled |

What ships if Claude Code passes and Codex doesn't: v1 ships, documented as
measured on Claude Code. Codex's result goes into the Connect tab's notes for
that client.

## 2. Stacks

The eval needs stacks that look like the ones doze is for: several services in
one `doze.hcl`, in the languages v1 reads, talking through queues, topics,
tables and HTTP.

| Stack | Role |
|---|---|
| `examples/shop` | the join's fixture in component tests; too small (3 apps, 379 lines, no tests) to host eval tasks |
| `parcel`, the eval stack | **our own, built by agents for this eval** (V4): the corpus for E0, E1 and E2. Six apps in Go and TypeScript for E1; a Python and a Rust app join for E2. Described below |
| Real stacks | external validity, after E2. Candidates must have three or more services in v1's languages, two kinds of link, unit tests that run without services, a history of commits that changed more than one service, and a license that allows it. Jitsu and Hatchet are the first candidates ([research/7](research/7-eval-stack.md)) |

**No backend runs during the eval.** Recall reads code and `doze.hcl`, and
declared queues and topics come from config, so it needs nothing running.
Every task is scored by unit and contract tests that run with `go test`,
`vitest`, `pytest` or `cargo test` and no services. A stack in the eval
therefore needs:

- a `doze.hcl` that declares its apps, modules and resources;
- unit tests that run with nothing started;
- no tests that start containers. A real stack ported later keeps such
  suites out of the visible and hidden sets.

### Why our own stack

Decided 2026-10-20 (V4), replacing the earlier choice to port Jitsu.

- **It exercises what recall is for.** No open-source system checked links Go
  and TypeScript services through doze-aws queues, Kafka topics, shared
  Postgres tables and HTTP at once ([research/7](research/7-eval-stack.md)
  §0). Ours does, by design, and every catalogue shape (arg, literal,
  option, output, identity) appears at least twice.
- **Nothing to work around.** Jitsu brought a cgo Kafka client, Mongo and
  ClickHouse to leave out, and testcontainers suites to delete. Ours is
  written for unit tests that run with no services from the start.
- **No training contamination**, as long as it stays private.
- **It grows with the languages.** The Python and Rust apps are written for
  E2 instead of found.

What it costs is realism: code written by agents is cleaner and more uniform
than code written by teams over years, and that helps the A0 baseline. The
rules below exist to buy that realism back, and the reports name the limit.

### The size it needs

Two things set the size, and neither is "as small as possible".

1. **It must not fit in the agent's context.** If the agent can read every
   file, it finds every consumer without help and the eval measures nothing.
   So the E1 slice holds about **40–50k lines** of hand-written code
   (about 400–500k tokens, twice Sonnet's default window), plus generated
   code (sqlc, Prisma, the OpenAPI client) on top. With the Python and Rust
   apps, E2's stack reaches about 60–80k. The argument holds only at the
   default window, so every run pins it and long-context options stay off.
2. **It must hold the tasks E2 needs.** About 325 cross-app tasks plus the
   held-out quarter is roughly 430. Tasks that reuse the same link teach
   nothing new after a few, so the full stack needs **at least 36
   cross-app links**, and the E1 slice at least 25, (topic, queue, bucket or SNS topic, shared table, HTTP route
   between apps), with no more than about 12 tasks per link. Tasks start
   from different commits in the history, so one link can be met at
   several stages of its life.

It is still far below Sourcegraph's 400k lines, where tools helped most.
S3 reports results by size bucket, and every report says the stack is small
and agent-built.

### The apps

| App | Language and libraries | Owns | Talks to |
|---|---|---|---|
| `storefront` | TypeScript, Fastify, AWS SDK v3 | the public API | calls `orders` over HTTP through a generated OpenAPI client, and `tracker` (E2); uploads labels to S3 `labels` |
| `orders` | Go, chi, pgx, franz-go | tables `orders`, `order_lines` | serves the OpenAPI routes; publishes Kafka `order.events`; consumes `shipment.events` (`kgo.ConsumeTopics`) to update status; reads SQS `holds` through `GetQueueUrl` (E2) |
| `billing` | Go, `net/http` ServeMux, `database/sql` and sqlc, Sarama, aws-sdk-go-v2 | table `invoices` | serves invoice routes that `console` calls; consumes `order.events`; reads the `orders` table directly; sends to SQS `invoices`, its URL from `GetQueueUrl` |
| `dispatch` | TypeScript, KafkaJS, `pg` | table `shipments` | consumes `order.events`; publishes `shipment.events`; publishes to SNS `courier-updates` |
| `notify` | TypeScript, `@confluentinc/kafka-javascript`, AWS SDK v3 | none | consumes `shipment.events`, SQS `invoices` (`GetQueueUrlCommand`) and an SQS queue subscribed to SNS `courier-updates`; reads S3 `labels` |
| `console` | TypeScript, Next.js app router, Prisma | none | reads `orders`, `shipments` and `invoices` through Prisma, and `holds` (E2); calls `billing` over HTTP |
| `risk` (E2) | Python, FastAPI, confluent-kafka, boto3, psycopg | table `holds` | consumes `order.events`; sends to SQS `holds` |
| `tracker` (E2) | Rust, axum, rdkafka, sqlx | none | consumes `shipment.events`; reads the `shipments` table; serves HTTP that `storefront` calls |

Modules: doze-aws (SQS, SNS, S3), Kafka, Postgres. The libraries differ on
purpose, two Kafka clients each in Go and TypeScript, so finding the
consumer of a topic is never one grep for one client's API.

The table gives about 13 links for E1. The history adds the rest as
features, and T.1's plan lists every target link before building starts:

- each HTTP route one app calls on another counts as its own link
  (storefront's carts, checkout, returns and tracking calls; console's
  invoice, refund and payout calls);
- new topics as features arrive: `order.refunds`, `returns`,
  `inventory.reserved`;
- new queues: retries and a dead-letter queue for `invoices`, a `payouts`
  queue;
- more tables read across apps: `order_lines` by billing, `refunds` by
  console and notify.

Every catalogue shape appears at least twice in the E1 slice: `option` in
`orders` and in a second franz-go consumer the history adds to `billing`
(two Kafka clients in one app, from the trap list); `output` in `billing`
and `notify`.

### Buying back realism

Each rule answers one risk from [research/7](research/7-eval-stack.md) §4.

- **Targets set from real code first.** Before building, measure Jitsu and
  Hatchet (read-only clones, never ported): file sizes, functions per file,
  duplication, how many ways one resource is reached (env var, config
  struct, constant, generated client), call depth from handler to queue
  call, share of generated code. The built stack must land inside their
  ranges.
- **A written trap list**, each trap something a required-site list can aim
  at:
  - a second consumer of a topic in an app you wouldn't expect;
  - a queue URL built from parts, or fetched with `GetQueueUrl`;
  - a topic name both in a shared constant and as a literal;
  - a table read by two apps through different libraries;
  - the generated OpenAPI client between Go and TypeScript;
  - helpers that wrap the Kafka or SQS client, two layers deep;
  - a call site behind a feature flag;
  - dead code that still names an old topic;
  - config read in three places, one of them stale.
- **Grown as history, not dropped in one commit.** A small v0 first, then
  80–120 feature commits written from issue-style specs, each with its tests,
  the way a team would land them. Traps arrive during this growth, so they
  look like accretion, and older patterns stay behind next to newer ones.
- **Mixed authors.** Opus builds most of it. Codex writes at least two apps
  once its access exists (before E2), with different prompts per app. The
  model under test, Sonnet, writes none of it.
- **An independent audit.** A separate agent session, which never built the
  stack, checks it against the targets and the trap list. The memory rule
  applies: self-judged realism is too generous.

### Where it lives

doze is a public repository, so `parcel` can't live in it without losing the
contamination advantage.

- **`doze-dev/parcel`**, private: the stack. Its own repository because its
  git history is part of the corpus: tasks pin base commits, and recall reads
  git for staleness.
- **`doze-dev/doze-recall-eval`**, private: the task sets, the held-out quarter
  and the results. Sessions tuning recall never read the held-out folder.
- **doze-recall**, public: the harness (`eval/run/`) and the retrieval
  eval's code. The retrieval eval runs in `doze-recall-eval`'s CI against a
  pinned `doze-recall` build. Only summary reports, never tasks, come back to the public repositories.

The doze daemon runs in every arm, so recall is reached the real way: woken
through the proxy. Nothing connects to the modules, so they stay asleep.

E1's tasks cross languages on purpose: at least half change something in a Go
service that a TypeScript service consumes, or the other way around. A recall
that sees only one side would say partial on exactly these.

The small-stack question (code search lowered reward below 400k lines in
Sourcegraph's own benchmark) is answered by S3 on every stack, not by one
small control stack. The 400k+ bucket, where Sourcegraph saw tools help most,
is unlikely to be reached in v1's eval; reports say so.

## 3. Tasks

A task is:

- a starting commit;
- a prompt in a developer's words;
- visible tests and hidden tests;
- a reference patch;
- the list of required sites.

The design follows DepBench: a partial change passes the visible tests, and
the hidden tests, usually in the consuming app, catch what was missed.

| Family | Example | In E2 |
|---|---|---|
| Cross-app change | "Add a `currency` field to the order event and use it in billing's receipts." Hidden tests in billing and notify | primary; sized by §8 |
| Single-file | well-localized tasks where recall should add nothing | about 40, for S3 |
| In-app propagation, pinned version, diagnose | as in [context-system.md](context-system.md) §1. A diagnose task ships a recorded journal and log run as fixtures, which recall reads from disk as it does for a sleeping stack | 10 each, exploratory: reported, not concluded on |

"Run and verify" tasks are not in the eval, because they are the only family
that needs a running stack. Recall's run and test commands in `brief` are
still measured indirectly: whether the agent ran the right tests before
finishing.

**Hidden checks run without services.** "Did the agent finish the change?" is
a question about code, so code-level checks answer it. There are two kinds.

**Contract tests across the boundary.** A test only in the consumer, decoding
a hand-written event, would pass when only the consumer changed. It would
also fail correct solutions that chose different names. So a cross-app
hidden test runs in two steps, through a file in place of the topic:

1. The **producer step** calls the producer's own code (in its language) to
   build the message the change concerns, and writes it to a file.
2. The **consumer step** feeds that file to the consumer's own handling code
   and checks the observable result: the receipt shows the currency, the row
   gets the column.

A change that crosses three apps runs one step per hop, each reading the
file the step before wrote. The harness passes each step an absolute path
in `CONTRACT_DIR`, because `go test` runs inside the package's folder.

It passes only when both sides changed and agree with each other, whatever
names the agent picked. A task's prompt fixes only what a developer would be
told (the behaviour), not field names.

**Static checks for wiring.** Some required sites are read by no test:
`doze.hcl` wiring, SQL migrations, env files. A hidden static check covers
them:

- `doze lint` on the stack;
- a check that parses the migrations with the Postgres grammar and asserts
  the column exists;
- a check that the consumer's env names a topic `doze.hcl` declares.

### Required sites

The reference patch is one correct solution, not the only one. Its changed
declarations aren't the right gold, because a patch can include incidental
edits. So each task lists **required sites**: the places any correct solution
must change.

- **Code declarations** that exist at `base`, in any of v1's languages.
- **Other places**: `doze.hcl` wiring, SQL migrations, env files, fixtures.
- **Declarations the patch adds** don't exist at `base` and aren't listed.

Authors record each site as `path:line` plus the symbol's name. They never
see recall, so they can't write graph ids. The harness turns sites into ids
afterwards, in its own step, using recall's id grammar.

A site is required when reverting just that hunk from the reference patch
makes a hidden test or a hidden static check fail. This is checked
mechanically where hunks separate cleanly, and by the reviewer agent
otherwise.

### Validity checks, run on every task before it enters a set

1. The hidden tests fail at `base`.
2. They pass with the reference patch, 5 runs out of 5, with no services
   running. Flaky tasks are fixed or dropped.
3. A partial patch (the primary site's hunk alone) passes the visible tests
   and fails a hidden one. Otherwise the task doesn't test completeness.
4. The hidden checks are not in the worktree the agent gets. They are copied
   in only for scoring. Test suites that need services are deleted from the
   worktree in every arm alike; `parcel` has none.

### Who writes tasks

**Agents write the tasks** (decided 2026-10-20, V3). There is one person on
recall, and the evidence about task authors (whoever tunes recall shouldn't
write what it is judged on) still applies to agents. So authoring is walled
off from tuning:

- **Separate sessions with nothing of recall in them.** Authoring runs in a
  checkout of the eval stack only, with a fresh configuration home. No
  recall MCP server, no access to the doze repo, and never a sight of a
  recall answer.
- **A different model from the subject.** Opus authors; the eval runs
  Sonnet. That doesn't remove the bias of agents writing for agents, but it
  stops a model writing tasks it would itself find easy.
- **Patch first, prompt second.** The authoring agent makes a real change to
  the eval stack (the reference patch), writes hidden tests for the consuming
  app, and only then writes the prompt a developer would have written. It
  lists required sites by reverting hunks one at a time.
- **A second agent reviews every task**, adversarially, in its own session:
  - does the prompt leak the answer?
  - are the required sites right?
  - would the hidden tests catch a partial change?
  - is the task one a developer would actually be given?
- **The four mechanical validity checks decide entry.** No task enters a set
  on any agent's word.
- **The owner reads a sample:** 5 tasks of every 20, before the set is
  frozen.
- **Frozen sets, and a held-out quarter.** The held-out tasks live where the
  sessions tuning recall never read them, and the retrieval eval never runs
  on them.

The remaining risk is stated in every report: agents wrote the stack, the
tasks and the solutions, so the tasks may lean toward what agents find
natural.

### Task format

```yaml
# doze-recall-eval: tasks/<id>/task.yaml
id: parcel-order-gift-note
family: cross-app
stack: parcel
base: 3f9c1e2
prompt: |
  Customers can add a gift note at checkout. It should print on the packing
  slip that dispatch generates.
visible_tests: ["cd orders && go test ./..."]
hidden:                                                   # kept outside the worktree
  contract:                                               # one step per hop, files in $CONTRACT_DIR
    - "cd storefront && pnpm vitest run hidden/gift-note-request.test.ts"  # writes request.json
    - "cd orders && go test -run TestHiddenGiftNote ./..."                 # reads request.json, writes event.json
    - "cd dispatch && pnpm vitest run hidden/gift-note-slip.test.ts"       # reads event.json
  static: ["doze lint", "migrations: orders has a gift-note column"]
required:                                                 # path:line + symbol, by the author
  - {at: "storefront/src/routes/checkout.ts:…", symbol: "…"}             # illustrative
  - {at: "orders/api/openapi.yaml:…", symbol: "…"}
  - {at: "orders/internal/http/create_order.go:…", symbol: "…"}
  - {at: "orders/migrations/…_gift_note.sql", symbol: "…"}
  - {at: "orders/internal/events/order.go:…", symbol: "…"}
  - {at: "dispatch/src/slips/render.ts:…", symbol: "…"}
reference: reference.patch
checks: {fails_at_base: true, reference_passes: 5/5, partial_fails_hidden: true}
```

## 4. Arms

| Arm | Setup |
|---|---|
| A0 | No recall. The `recall {}` block and every `recall/` and `.recall/` folder are removed from the worktree, the `doze-recall` binary is absent, and `doze recall` is disabled. The daemon runs with nothing started, as in every arm |
| A1 | Recall registered as an MCP server; nothing in AGENTS.md |
| A2 | A1 plus the pasted lines from [answers.md](answers.md) §10 |
| A3 | A2 with context made false before the run: stale notes, wrong `told` notes in main sections, injected `doubtful` edges |
| A4 | A2 with a generated glossary, once generation exists |
| A5 | Standalone recall, E2 only: A2's setup with `doze.hcl` removed, a `.env` file in each app generated from the resolved config, and recall started by the agent through `doze-recall serve` ([standalone.md](standalone.md) §9). No backend runs in any arm, so A2 − A5 is what the declarations add |

**The leak check.** CodeGraph's own benchmark was contaminated when its
baseline reached the tool through the shell, in 26 of 28 runs. Every A0
transcript is scanned for any use of recall's files, binary or address. A run
with a leak is void.

**Same starting point.** A1–A5 index the worktree before the agent starts, so
first answers aren't "catching up". A0 waits the same length of time.

### Harnesses

- **Claude Code for E0 and E1.** It is the plan the owner has.
- **Codex and Cursor's CLI in E2.** The owner gets access to them before E2
  (decided 2026-10-20: not before, since nothing earlier needs them). Cursor
  joins if, by then, its headless mode and MCP configuration can be isolated
  per run.
- Each run gets a fresh, empty configuration home (`CLAUDE_CONFIG_DIR`,
  `CODEX_HOME`), so no one's settings, memory, plugins or global CLAUDE.md
  leak in. Only the arm's MCP config is present.
- Sonnet, pinned by full name with `--model` and recorded per run, on the
  Claude Code subscription (§9).
- The research found the harness alone moves results by up to 16 points with
  the same model, so harnesses are never pooled.

## 5. What is recorded per run

| Measure | How |
|---|---|
| Pass | hidden tests pass and visible ones still do |
| Completeness | required sites touched ÷ required sites |
| False complete | required sites reachable from an `impact` target, in scope by [context-system.md](context-system.md) §4.8, missing from an answer that said complete |
| Adoption | recall called at all; turn of the first call; calls per tool |
| Grep after recall | grep for a name within two turns of a recall answer that listed it |
| Acted on Unconfirmed without checking | the agent edited a `path:line` that appeared only under Unconfirmed, with no read of a range covering that line after the answer and before the edit. Split by whether the item was true or false (A3 knows which) |
| Waste | repeated identical calls; cursor use; invalid-argument errors |
| Cost | input and output tokens, turns, wall time |

Transcripts are kept. A person reads a sample from every family and arm.
Many redundant calls mean pagination or limits are wrong. Many invalid
arguments mean descriptions are wrong.

## 6. Retrieval eval

The cheap loop that keeps ranking honest between agent runs. It uses the same
tasks, minus the held-out set, with no agent:

- `brief` on the prompt alone: anchor precision@1, and recall@5 of required
  sites projected to files, split by "the prompt names an identifier" and
  "plain words only". The bar from [answers.md](answers.md): about 80% file
  recall@5 on plain-word prompts before the glossary becomes required.
- `impact` on each required code site: recall and precision of its listed
  sites against the task's other required sites, and false complete claims.
- Context yield: the share of answer tokens that point at required sites.
- Tokens per answer at p50 and p95, truncation rate, questions instead of
  answers.
- The budget sweep (concise, detailed, 2×) that sets answers.md §2's
  defaults.

The pipeline is deterministic, so CI gates on exact metrics per frozen task
set: any drop fails the change, unless the change also updates the task set
and says why. Gating starts once there are 50 tasks; below that it reports
only.

## 7. On a person's own stack

Two signals need no labelled tasks. Neither leaves the machine; both show in
the dash.

- **Review outcomes.** The share of `proposed` edges people approve. It
  measures generation's precision where it matters.
- **Observed edges** (after v1). An edge doze's servers saw that code didn't
  find is a measured miss of the static join. Its rate estimates the join's
  recall with no labelling at all.

## 8. Order and sizes

| Step | What | Exit |
|---|---|---|
| E0 | `parcel`'s E1 slice built and audited; the task format and validity checks (built first, in P0.4); the retrieval eval in CI; 10 tasks | the retrieval eval runs in CI in report-only mode |
| E1 | The eval stack, first slice; 20 cross-app tasks; Claude Code on Sonnet; arms A0 and A2; 2 runs each: **80 runs** | **a pilot**, with no ship decision unless V1 chooses E1 alone (§1). It shows whether recall helps a lot, measures the between-task variance that sizes E2, and measures how many runs fit in a usage window. It can't show a small effect, and can't separate the server from the pasted lines (A1 waits for E2) |
| E2 | Cross-app tasks as sized by E1, plus the single-file and exploratory families; A0–A2 and A5, and A3 if A2 showed an effect; Claude Code, Codex and Cursor's CLI | the primary measure decides v1, when V1 chooses E2 |
| E3 | Each release: the retrieval eval in CI; a 30-task agent run on a rotating subset | **a smoke check** for breakage (adoption collapsing, crashes, cost jumps), not a statistical gate; a full E2-style run on major changes |

**How many cross-app tasks E2 needs**, before E1's data. Assume pass rates
around 40–50% and about 30% of tasks where the arms disagree. The paired
difference then has a standard error of about √(0.30/n):

| Cross-app tasks | Standard error | Smallest effect found 80% of the time |
|---|---|---|
| 20 (E1) | 12 points | about 34 points |
| 40 | 8.7 points | about 24 points |
| 100 | 5.5 points | about 15 points |
| 230 | 3.6 points | about 10 points |
| 325 | 3.0 points | about 8.5 points |

Detecting the +8.5-point effect the run is sized for, with 80% power, takes
roughly 325 cross-app tasks. More runs per task help less than more tasks,
because the variation between tasks dominates. If that many tasks can't be
built, the honest alternative is to accept a 15-point detectable effect with
100 tasks and say so in the result (§10).

## 9. Running it, and what it costs

**The harness** is bun scripts in `doze-recall/eval/run/`, following the doze-dev
rule of bun for tooling. Each run:

1. Make a fresh git worktree at `base`, apply the arm's removals or
   injections.
2. Load the stack in a doze daemon with a fresh `DOZE_HOME`. Nothing is
   started: the modules stay asleep, and only recall wakes when the agent
   connects.
3. Index (A1–A4), or wait the same time (A0).
4. Run the agent CLI headless with the prompt, in a fresh configuration home.
5. Copy in the hidden tests, run them, and collect the transcript, diff and
   results.
6. Stop the daemon and remove the worktree and `DOZE_HOME`.

With no backends, a run costs a worktree, an idle daemon, recall and the
agent. Runs can go several at a time, limited by the subscription's pace
rather than the machine.

**On a subscription** (the default plan). Runs use the subscription's usage
limits instead of money:

- The model is pinned with `claude -p --model <full model name>`, which
  works on a subscription, so API billing isn't needed for pinning.
- The harness runs one run at a time by default, and more in parallel only as far as the plan's limits allow. When it hits a usage limit, it waits
  for the window to reset and resumes where it stopped, so a run set can
  span days.
- How many runs fit in one usage window is unknown until measured. E1's first
  10 runs measure it, and the plan for the rest is sized from that.
- **Sonnet for the whole eval** (decided 2026-10-20), pinned by its full
  name, `claude-sonnet-5-5`, so a new Sonnet release can't change results
  mid-set. It stretches the subscription's limits further than Opus would.
  The results then speak for Sonnet only, and every report says so.
- The retrieval eval and component tests use no model at all, so they cost
  nothing on any plan.
- Codex and Cursor need their own plans, obtained before E2.

**Money**, only if API billing is ever used instead. These are rough
estimates. Sourcegraph's $0.51–0.73 per task was Haiku 4.5, and Sonnet costs
a few times that, so plan on $1–3 per run.

| Run | Runs | Likely cost on the API |
|---|---|---|
| E1 (20 tasks × 2 arms × 2 runs) | 80 | $80–250 |
| E2, Claude Code (about 325 tasks × 3 arms × 3 runs) | about 2,900 | $3,000–9,000 |
| E2, Codex | about the same again | |

**Agent time and the owner's attention**, the larger cost:

- The eval stack (§2) and its tasks (§3) are written by agents on the same
  subscription, with Opus, so authoring competes with the pilot's runs for
  usage limits. The plan staggers them: author, then run.
- The owner's time is supervision: setting each authoring batch going,
  reading 5 tasks in 20, and reading the reviewer agent's rejections. Roughly
  a day per 20 tasks.
- How much authoring a usage window holds is measured on the first 5 tasks,
  the same way run pace is.

**Results** go in the private `doze-recall-eval` repository, under `results/<date>-<harness>/`:

- the frozen task set's hash;
- the analysis script, written before the run;
- per-run rows, the summary with intervals, and sampled transcripts.

Negative and flat results are reported the same way as positive ones.

## 10. Decisions this needs

| # | Decision | Options |
|---|---|---|
| V1 | How strong a claim v1 ships on | decided after E1, from its measured effect and pace: (a) about 325 cross-app tasks, detecting about +8.5 points; (b) about 100, detecting about 15 points, stated plainly; (c) E1 only, a pilot result with wide intervals |
| V2 | What pays for runs | **decided 2026-10-20:** the Claude Code subscription, Sonnet, E1 sized to 80 runs. API billing only if the subscription proves too slow for E2 |
| V3 | Who writes tasks | **decided 2026-10-20:** agents, in sessions walled off from recall, Opus authoring and a second agent reviewing, the owner reading a sample (§3) |
| V4 | The eval stack | **decided 2026-10-20, revised the same day:** build our own, `parcel` (§2), sized to stay outside the agent's context and to hold E2's tasks, grown as history with a trap list, audited against Jitsu and Hatchet's measures. Replaces porting Jitsu. A real ported stack after E2 for external validity |

## 11. Threats to the result

| Threat | Guard |
|---|---|
| Training contamination | `parcel` and its tasks live in private repositories and are never published; only summary reports reach the public doze repo |
| An agent-built stack too clean or too uniform | realism targets measured on Jitsu and Hatchet, the trap list, growth as history, mixed authors, an independent audit (§2); every report says the stack is small and agent-built |
| Agents starting modules | nothing needs them, but either arm may run `doze up`; the harness records it per run so it can be compared |
| Tasks written to suit recall | authoring sessions that never see recall, a different model from the subject, a reviewer agent, frozen sets, a held-out quarter |
| Agents writing for agents | Sonnet writes none of the stack; stated as a limit in every report; a real ported system after E2 for external validity |
| The baseline reaching recall | A0 strips recall; every A0 transcript is checked for leaks |
| Model and harness drift | pin and record versions; compare arms only within one run |
| Small samples | the pilot sizes the main run; intervals always shown; what a run couldn't detect is stated |
| Flaky tests | 5/5 reference passes before a task enters a set |
| Grading | hidden tests decide pass; no model grades outcomes |

## Open

- Whether the tool-name test (plain `impact` vs `recall_impact`,
  [answers.md](answers.md)) and the client display check (text vs
  `structuredContent`) run inside E2 or as small side runs. They can't share
  the primary measure's budget without diluting it.
- When generation's waiting jobs (contracts first) get an arm. Not before
  E2's primary result.
