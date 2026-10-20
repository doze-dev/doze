# Improving the context with the person's own agent

Status: design, 2026-10-20. Claude Code flags checked against the installed
`claude --help` on 2026-10-20; the other CLIs are not installed here and are
unchecked.

The proven layer knows what the code and `doze.hcl` say. It can't say what a
flow means, what the business calls something, which conventions a team keeps,
or where a queue URL goes when it is built from a tenant prefix at runtime.
Generation asks the person's own agent CLI (Claude Code, Codex, Cursor,
Gemini) to fill exactly those gaps, using the login and subscription they
already have. Recall never holds a key and never calls a model itself.

The rule that keeps it safe: **the agent proposes, recall decides.** The agent
returns structured proposals. Recall checks every one against the graph,
writes the ones that pass, and sends the ones that need a person to Review. An
agent can produce a bad proposal; it can't put one into the context unchecked.

## What generation is for

Decision C5 ([context-system.md](context-system.md) §10) limits generation to
work recall can check. The only independent study of generated repo context
found that it made agents worse (−0.5 to −2 points, +20–23% cost), so prose
about the code has to earn its place in the eval before it ships.

The v1 jobs, all driven by what the proven layer could not settle:

| Job | Input recall gives | Output | Accepted when |
|---|---|---|---|
| Edge verdict | an unresolved join site or a doubtful edge, its candidates, the code spans involved | which candidate, or none, with `file:line` evidence | the cited lines exist and contain the call; the chosen candidate was one code found. Lands in the person's uncommitted `proposals.json` as `proposed`; approving it appends a decision to `topology.yaml` ([context.md](context.md) §5) |
| Glossary | identifiers and the prose (READMEs, ADRs, comments, route paths) that mention them | business term ↔ code name pairs | every code name resolves. Entries are indexed as search fields that point at nodes, the bridge from plain words to code names that keyword search otherwise lacks; they are never served as prose |
| Note check | a stale human note and the diff since it was checked | "still true" or "outdated", with the reason | always goes to a person; never edits the note |

Waiting on the eval, in this order:

| Job | Output | Accepted when |
|---|---|---|
| Contract | what a route, topic, queue or table carries, who depends on it, rules for changing it | links resolve; fields named exist in the code's types or the schema |
| Flow | steps and invariants from an entry point along the path the graph found | every wikilink resolves; each step connects to the next in the graph within a few hops |
| Conventions | how this app handles errors, logging, tests | each claim cites two examples that exist. Weak evidence: prose conventions are often ignored, and the project's own lint and test commands (from `brief`) work better |
| Area names | a name and a paragraph per graph community | the agent only names; membership is recall's |

Per-symbol descriptions are cut: they go stale on every commit, and agents
read the code themselves ([freshness.md](freshness.md)).

Generated files rank below proven facts and human notes, carry the agent and
version that wrote them, and are anchored by hash, so recall knows when they
go stale.

## A job, in full

Every job has the same shape, whichever agent runs it:

```
job:      edge-verdict
inputs:   graph ids + code spans, chosen by recall
question: fixed per job kind, versioned
schema:   the JSON the answer must match
checks:   what recall verifies before accepting
key:      hash(inputs) + job version + prompt version + agent
```

The key is the cache: the same inputs with the same prompt are never sent
twice. A run is a list of jobs. Before it starts, the dash shows its scope
(jobs, files, apps) so the person can see what it will spend, and it can be
stopped at any point.

## How the agent is run

The design record chose ACP, the Agent Client Protocol, with recall as the
client: it answers the agent's file and permission requests itself, so the
guardrails are recall's. One integration reaches many agents. The cost is that
Claude Code and Codex speak ACP through adapters installed separately, which
is one more thing for a person to set up.

Most agent CLIs also have a headless mode, and that gives the same guarantee
with nothing extra to install, if the agent's only tools are recall's own:

```
claude -p "<job prompt>" \
  --tools "" \                                   # no built-in tools: no Read, Bash, Edit
  --strict-mcp-config --mcp-config <recall.json> # only recall's MCP server
  --json-schema <job schema> --output-format json \
  --permission-mode dontAsk --no-session-persistence
```

The agent reads code only through recall's MCP tools, which serve spans inside
the stack minus excludes and never secrets. It has no shell and no write tool.
Its answer comes back on stdout, matched to the schema, and recall does the
writing. The scope rules are enforced by recall in both designs. Headless mode
just needs fewer moving parts.

Leaning: **headless first, ACP only if headless modes prove too uneven.** An
agent is listed in the Connect tab only after a contract test proves, on a
fixture, that it:

- reads nothing outside recall's tools,
- runs no command,
- writes no file, and
- returns schema-valid output.

Claude Code can do all four today with the flags above. Codex, Cursor's CLI
and Gemini are checked when their adapters are written. An agent whose
headless mode can't turn its own file and shell tools off is not supported,
rather than supported with weaker guarantees.

## Untrusted input

Code and prose sent to the agent are someone else's text, and may contain
instructions. They are wrapped and labeled as untrusted in the prompt. More
importantly, the agent has nothing it could misuse: no shell, no writes, no
reads outside the stack. The worst a hostile file can do is steer one
proposal, which then has to pass the checks and, for edges and note checks, a
person.

## The loop

1. **Index** settles what code and `doze.hcl` prove, and leaves the rest as
   `unresolved` with its evidence.
2. **Generate** sends agents only the unresolved and doubtful items, the
   names the glossary doesn't cover yet, and stale notes. Runs stay short
   because they only see what code couldn't settle.
3. **Check** each proposal against the graph. Failures are dropped and logged
   with the reason.
4. **Review**: everything that passes its checks waits in the dash's Review
   tab (`a` approve, `r` reject, `e` edit): edge verdicts, glossary rows and
   note checks. Nothing an agent produced reaches a committed file until a
   person approves it ([context.md](context.md) §1).
5. **Remember**: rejections are kept, so the same guess is not proposed
   again.

## What the person does

- Picks their agent once in the Connect tab. It is saved in their doze home,
  never in `doze.hcl`.
- Presses Generate when the header says files are stale, sees the scope, and
  confirms.
- Approves or rejects what lands in Review.

Standalone, the same steps are `doze-recall generate` and
`doze-recall review`; `--agent` picks the agent once, remembered in
`~/.doze-recall/config.toml` ([standalone.md](standalone.md) §7).

Teams can list the agents allowed to read their stack in `doze.hcl`
(`agents = ["claude", "codex"]`). How each agent is launched is built into
doze and not configurable, so the tested path can't be bypassed.

## Open

- When the eval should test the waiting jobs, starting with contracts.
- How to keep a run within a subscription's limits without knowing the
  limits. Claude Code has `--max-budget-usd` for API billing; a
  subscription has no equivalent, so bound runs by job count.
