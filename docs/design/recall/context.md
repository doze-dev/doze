# Contract: what people add

Status: design, 2026-10-20; revised the same day after an independent review.
Builds on [graph.md](graph.md) (ids, labels) and
[generation.md](generation.md) (where proposals come from). Nothing here is
implemented.

Code says what the system does. People know why, what must never change,
which queue is FIFO and why the dedupe id is the invoice id. Recall keeps
that knowledge in committed files that point at graph nodes. It shows a note
only when the note's node is part of an answer, and notices when the code
under it changes.

| File | Holds | Committed |
|---|---|---|
| Notes (`*.md`) | facts about code and resources that code doesn't say | yes |
| `recall/glossary.md` | business words ↔ code names, for search | yes |
| `recall/topology.yaml` | people's decisions about edges: approved, rejected, asserted | yes |
| `<ProjectDir>/recall/proposals.json` | agent proposals waiting for review | no; per person, kept across index rebuilds |

## 1. When recall writes

**Recall writes a committed file only when a person presses a key in the
dash**, or, standalone, in `doze-recall review`, which uses the same keys ([standalone.md](standalone.md) §7). Nothing is written in the background, on index, or by an agent.

| Key, where | Writes |
|---|---|
| `c` in Notes | the note's `checked` line |
| `t` in Notes | the note's `about` line (re-anchor) |
| `y` in Notes | a note-check draft's change to `checked`, or a new note the person accepted; never edits a note's body |
| `n` in Notes or Review | a new note file, with `about` filled in and an empty body, then opens `$EDITOR` |
| `a` / `r` in Review | a decision appended to `topology.yaml`, or a glossary row appended to `glossary.md` |
| `e` anywhere | nothing itself; opens `$EDITOR` at the entry |

Every write is atomic: write a temporary file and rename it over the
original. If the file's modification time changed since recall read it, the
write stops and the person is told, so an edit in another window is never
lost. Recall writes LF line endings, keeps a file's existing CRLF if it has
them, and normalizes line endings before any hash.

## 2. Where the files live

| Home | For |
|---|---|
| `recall/` beside `doze.hcl` | the stack: notes about resources and flows across apps, the glossary, `topology.yaml` |
| `.recall/` at an app repo's root | notes about that repo's code |
| `.recall/stack/` at the root, standalone | the stack's files (notes across apps, the glossary, `topology.yaml`), since there is no `doze.hcl`; read like `recall/` when a `doze.hcl` sits at that root ([standalone.md](standalone.md) §7) |

Code ids don't contain app names ([graph.md](graph.md) §1), so a repo's
`.recall/` notes work in every stack that contains it. Stack ids
(`module.cloud:queue.orders`) mean something only next to the `doze.hcl`
that declares them, so notes that name resources belong in the stack's
`recall/`. A `.recall/` note naming a stack id is loaded only by stacks where
that id resolves, and is flagged in the others' Notes tab.

New notes from `n` go to `recall/notes/<slug>.md` when they name a stack id,
and to `<repo>/.recall/<slug>.md` when they name only code. The slug comes
from the first `about` name.

## 3. Notes

```markdown
---
about:
  - "module.cloud:queue.orders"
  - "go:example.com/acme/internal/order.(*Service).Create"
checked: 2026-10-19 @ a3f9c1e
---
orders is FIFO. The dedupe id is the invoice id; never generate a new one on retry.
```

- **`about`** lists full graph ids, quoted. A short name typed by hand
  (`Create`) isn't loaded: the Notes tab flags it and offers the expansion,
  and `t` writes it. Resolving short names silently would let a new `Create`
  elsewhere move the note.
- **`checked`** is a date and the commit it was checked at. A hand-edited
  date without a commit uses the commit that last changed the note file.
- **The body** is short. Answers show the first paragraph, at most 120
  words, and the file path for the rest.

### When a note is shown

A note is shown when one of its `about` nodes is part of an answer: a seed,
a listed item, or a node on a join path the answer walked. Every tool's answer
has a Notes section when notes attach, holding at most 3 notes, those on the
highest-ranked nodes first ([answers.md](answers.md) §3.5). A note is quoted
as data with its file name, and ranks as `told`.

### When a note goes stale

For each `about` node, recall compares its state now with its state at the
`checked` commit, read from git:

- **A symbol:** its signature and body, with positions and comments
  stripped. Edits elsewhere in the file, gofmt, and new callers don't make it
  stale; a change to the declaration does.
- **A resource:** its declared attributes in `stack.json`. New consumers
  don't make a resource note stale.

Because the baseline comes from git, a teammate who clones the repo gets the
same staleness as the author. A stale note moves from Notes to Unconfirmed in
answers, with "stale since a3f9c1e: Create changed". `c` marks it checked at
the current commit.

### When a note is orphaned

When an `about` id no longer exists (renamed, moved to another package,
deleted), the note is orphaned. It isn't shown in answers. The Notes tab lists
it with re-anchor candidates, in this order:

1. the same name in the same module;
2. the same name and signature anywhere in the stack;
3. the same file's declarations with the same signature.

A signature match alone, without the name or the file, is never offered.
Many functions share `func(ctx) error`.

When a module disappears and another of the same type appears with the same
resource names (a rename from `cloud` to `aws`), the Notes tab offers one bulk
re-anchor for every note and decision that named the old module. This comes
after v1 ([phases.md](phases.md)); until then each orphan is re-anchored on
its own.

## 4. The glossary

`recall/glossary.md`, one table:

```markdown
| Term | Means | Points at | Source |
|---|---|---|---|
| checkout | placing an order from the cart | `go:example.com/acme/web.Checkout` `app.api:route "POST /orders"` | person |
| refund | money returned after an order | `go:example.com/acme/internal/refunds.(*Service).Refund` | agent (claude), approved by srini 2026-10-20 |
```

- **Points at** holds ids, each in backticks and separated by spaces.
  Backticks keep markdown from reading `(*T)` as emphasis, and allow commas
  inside route ids.
- **Source** is `person` or `agent (<name>), approved by <who> <date>`.
- **What it is for:** the bridge from a task's words to code names, for
  keyword search ([answers.md](answers.md) §3.2). Each term and its "Means"
  text are indexed as search fields pointing at the ids. A hit adds those ids
  as seeds at reduced weight. The glossary is never shown to agents as prose.
- **Who writes it:** people, like any file. Agent rows from generation
  always go through Review; approving one appends it. Recall never reorders
  or reformats the table.
- **Bad rows:** a row whose ids don't resolve is skipped for search and
  flagged in the Notes tab. The rest of the table still loads.

## 5. topology.yaml

People's decisions about edges that code alone can't settle. Recall never
writes generated content into it. The live picture of who sends to and
receives from what is the Topology tab and the `topology` tool, built from the
graph.

```yaml
# recall/topology.yaml: decisions people made in recall's Review tab.
recall: 1
decisions:
  - from: "go:example.com/acme/billing/jobs.Poll"
    verb: receives
    to: "module.cloud:queue.orders"
    status: approved
    by: srini
    on: 2026-10-20
    why: "queue URL is built from the tenant prefix + JOBS_QUEUE"
  - from: "go:example.com/acme/internal/report.Export"
    verb: writes
    to: "external:finance-warehouse"
    status: asserted
    by: srini
    on: 2026-10-20
    why: "the finance team's loader reads this bucket nightly; not in this stack"
```

- **Schema:** every value that is an id is a quoted string, in block style,
  because route ids contain spaces and quotes.
  - `from` is a code id.
  - `verb` is one of the graph's edge kinds: `sends`, `receives`, `reads`,
    `writes`, `requests`, `calls_external`.
  - `to` is a resource, route or `external:` id.
  - `status` is `approved`, `rejected` or `asserted`.
  - `by`, `on` and `why` are free text. `why` is quoted as data and redacted
    in answers.
- **Keyed by code, not by app.** The app is derived from which apps use
  `from`'s package, so renaming an app breaks nothing.
- **What each status does:**
  - `approved`: a `proposed` or `doubtful` edge a person confirmed. Served as
    `told`.
  - `rejected`: never proposed again; generation skips it, and the eval
    counts it against generation's precision.
  - `asserted`: an edge a person knows and code can't show. An
    `external:<name>` that isn't in the graph is created by the assertion.
- **Drift**, checked on every index, flagged in Review and never changed by
  recall:
  - an approved or asserted decision whose `from` or `to` no longer exists;
  - a rejected decision that code now proves (`exact` or `scoped`). Staying
    `doubtful` is not drift;
  - an approved decision that code now proves. Review offers to remove it as
    no longer needed.
- **Validation is per entry.** An entry with an unknown id or verb is
  skipped and flagged with a suggestion ("`biling` is not used here; did you
  mean `billing`?"). The other entries still load. Only a file that isn't
  YAML at all fails as a whole. Then recall keeps the last version it loaded
  in this session, or loads none on a fresh start, and says so in the header.

### Proposals

Edge verdicts and glossary rows from generation go to
`<ProjectDir>/recall/proposals.json`. That file is per person, not committed,
and survives index rebuilds because it isn't part of `graph.db`. Review lists
them; `a` and `r` turn one into a decision or a glossary row and remove it from
proposals. Teammates see a proposal only once someone has approved it into a
committed file.

## 6. Existing prose

Recall reads, and never writes:

- READMEs;
- ADRs and docs under the `include` globs;
- OpenAPI, AsyncAPI and proto files;
- SQL migrations;
- existing agent files (AGENTS.md, CLAUDE.md, `.cursor/rules`).

Their text is indexed for search, pointing at the nodes it mentions by exact
name. It is never served as notes. To make an agent see something with a
node, write a note.

## 7. In the dash

| Tab | Shows | Keys |
|---|---|---|
| Notes | stale (with what changed), orphaned (with re-anchor candidates), short names to expand, bad glossary rows, and note-check drafts | `enter` open · `c` checked · `t` re-anchor · `y` accept draft · `x` discard · `n` new note |
| Review | proposals, drift on decisions, decisions code now proves | `a` approve · `r` reject · `e` edit · `n` note |
| Topology | every resource with who sends, receives, reads and writes it; one level down, its declaration, every use with how it was found, its notes, its decisions, and what doze-aws doesn't implement | `enter` detail · `a` / `r` / `e` on one entry |

## 8. `doze recall check` in CI

The one headless command ([fit.md](fit.md) §6). It builds what it can of the
graph in the CI checkout, without a daemon: it loads the config in-process
and uses plugins only if they are already fetched. Then it checks:

| Check | Fails the build |
|---|---|
| every note, glossary row and decision parses and its ids resolve | yes |
| decisions that drifted (§5) | yes |
| secret patterns in notes, glossary and `why` | yes |
| parts it couldn't build: modules not downloaded, plugins not fetched, sibling repos not checked out | no; listed, so a reader knows the check was partial |

Exit codes: 0 when nothing fails, 1 when something does. The output lists
every finding with its file and line.

## 9. Security

- Notes, the glossary and decisions are committed, so they go wherever the
  repo goes.
  - The Notes tab warns before writing anything that matches a secret
    pattern.
  - `doze recall check` fails on one in CI.
  - Recall never puts env values into any file it writes.
- Every piece of people's text that reaches an agent is quoted as data and
  redacted ([answers.md](answers.md) §3.8): note bodies, glossary "Means",
  decision `why`. A hostile note in a repo is no worse than a hostile README,
  and is shown as what it is.

## Open

- Whether a stack-independent alias for resources (`queue orders` rather than
  `module.cloud:queue.orders`) is needed for app repos that join several
  stacks.
- Whether the Topology tab should show what changed against main, which the
  committed generated view would have given in pull requests.
