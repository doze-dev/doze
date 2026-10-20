# Keeping the context current

Status: design, 2026-10-20.

An agent that is told something out of date behaves worse than one told
nothing, because it trusts the answer. So the rule is: **the proven layers are
current within seconds, the AI layer may lag, and nothing is ever silently
stale.** Every item in every answer says how fresh it is.

## What changes, and how each kind stays current

Knowledge goes stale at different speeds and costs different amounts to
refresh, so each kind gets its own mechanism.

| Knowledge | Changed by | Kept current by | Cost |
|---|---|---|---|
| Declared stack | edits to `doze.hcl` or another producer's sources | re-reading the stack file when it changes; `stale` when its sources changed after it was written | trivial |
| Code facts | every save, including the agent's own edits | re-indexing the file by content hash, then re-resolving what depended on it | milliseconds per file |
| Big jumps | branch switch, pull, rebase, new worktree, edits while recall slept | a catch-up on wake against the last indexed commit | seconds; near zero for a branch seen before |
| Dependencies | lockfile changes | re-extracting the external symbols the code uses, cached by `module@version` | once per version, machine-wide |
| `topology.yaml` decisions | people | never changed by recall; flagged as drift when their ids vanish or code contradicts them ([context.md](context.md) §5) | free |
| Human notes | the declarations or resources they are about | compared with their state at the note's `checked` commit, read from git, so every clone agrees ([context.md](context.md) §3) | free |
| Generated files | any change to their inputs | computed staleness, then a scoped regeneration (below) | an agent run |

## Change detection

There are three candidates for noticing that files changed.

| | Poll size and mtime | Ask git | Kernel watcher |
|---|---|---|---|
| How | stat every file each interval, as `internal/watch` does | `git status --porcelain` and `git diff --name-only <indexed commit>` | FSEvents on macOS, inotify on Linux |
| Large repos | a stat per file per second; expensive at 100k files | git's own index and untracked cache do the work | cheap |
| Branch switch while asleep | a full re-stat | one diff between two commits | missed while asleep |
| Ignores | must apply `.gitignore` itself | already applies it | must apply it itself |
| Catch | none | dirs outside git need a fallback | Linux keeps no history; on macOS, FSEvents works without cgo through purego (kqueue, rejected for `watch`, is a different API) |

Measured ([research/3](research/3-breadth-speed-memory.md) §5): FSEvents
through purego, built with `CGO_ENABLED=0`, delivered file-level events in
about 50 ms and **replayed every change made while it was not running, from a
saved event id, in 13 ms**. On Kubernetes' 31k files, `git status` took 140 ms
(40 ms with the untracked cache) and a stat of every tracked file 43 ms.

Leaning: **FSEvents on macOS, inotify on Linux, git to reconcile** (on Linux
after sleep, and on macOS when FSEvents reports it dropped events), polling
only for small folders outside git. Recall's background process runs the
watcher itself, and starting that process counts as waking
([running.md](running.md) §5).

While recall sleeps nothing runs. It saves one event id per root; on wake it
asks the OS what changed since then.

## Three moments that keep answers honest

### On wake

1. Load the graph from its flat snapshot (tens of milliseconds for a million
   edges) and start serving immediately.
2. Ask the change feed what changed since the saved event id, or on Linux
   diff the last indexed commit and the dirty and untracked files against the
   current ones, per app.
3. Re-index the changed files in the background, then bump the generation
   number.

An agent's first call never waits for the whole catch-up:

- Files an answer cites are re-indexed first, as in every answer (below).
- Until the catch-up ends, answers say `partial: catching up (n files)`, so
  `impact` never claims to be complete over code it hasn't read.

### Before every answer: read your own writes

An agent edits `orders.go` and immediately asks `impact` on `Create`. If
recall answers from before the edit, the agent is misled by its own change,
which is the worst kind of out of sync. So before answering, recall stats only
the files the answer would cite. Any that changed since they were indexed are
re-indexed in line (a Go package re-check took a median of 1 ms and at worst
126 ms in [research/3](research/3-breadth-speed-memory.md)), and only then
is the answer built. Checking a few dozen files costs far less than the
answer itself.

### While awake

The change feed delivers changes after a quiet interval, as `watch` does, so a
burst of saves or a formatter run is one re-index, not hundreds.

## How freshness is shown

- **Every answer carries a generation number and the commit it reflects.**
  Recall never leaves out items because a session received them before:
  clients compact and clear earlier tool results without telling the server.
  Answers are small enough to repeat whole ([answers.md](answers.md) §7).
- **Proven items are current or absent.** Code facts are never served stale
  after a catch-up: they are re-indexed before an answer cites them.
- **Stale notes and generated files move to the Unconfirmed section**, with
  "stale since `a3f9c1e`: the code it describes changed" and the check to
  run. Agents adopt wrong context readily and labels inside the text barely
  slow them, so stale items never sit beside facts. Hiding them completely
  would lose the human knowledge in them.
- **`doze-recall status` shows the counts**: files stale, notes to check,
  entries in drift. Under doze, the dash's header strip also shows the total.
- **The eval measures it**: the share of answer items that were stale, per
  task. If that number grows, the generated layer is anchored to things that
  change too fast.

## The generated layer: how it avoids lagging too often

Generated files are the one layer that can't refresh itself without an agent
run, so most of the work is choosing what to generate.

1. **Generate only what changes slowly.** Flows, contracts, glossary and
   conventions describe how the system works, which changes over weeks.
   Per-symbol descriptions describe code, which changes every commit. They go
   stale constantly and agents can read the code themselves, so they are cut.
2. **Anchor to stable things.** A flow is anchored to the route, topic and
   handler symbols it passes through, not to line ranges. Moving code inside a
   function does not make it stale. Changing a signature or an event's fields
   does.
3. **Staleness is computed, not guessed.** Each file records a hash over the
   proven facts it is anchored to: the anchored symbols' signatures, a
   contract's fields, the edges between flow steps. It is stale when that hash
   changes, and recall knows exactly why.
4. **Regenerate small.** A run takes only stale files. The agent sees the
   previous file and the diff of the facts that moved, and is asked to change
   only what they invalidate, so the pull-request diff stays small.

When regeneration runs is open:

| Trigger | For | Against |
|---|---|---|
| Manual, with `doze-recall generate` (or the dash's Generate) | the person chooses when to spend their subscription | stale files pile up if nobody presses it |
| On merge to main | the committed files track main | needs an agent in CI, which recall otherwise avoids |
| When the stale count passes a threshold | automatic, bounded | spends subscription without asking each time |

Leaning: manual in v1, with the stale count shown by `status` and at the top
of `review` (and in the dash header under doze) so it is hard to ignore. Revisit with numbers from the eval.

## Never

- Serve a code fact from before a file change the answer depends on.
- Wait on an agent run to answer a question.
- Delete a human note because its code moved. It is flagged, or orphaned and
  offered for re-anchoring.
- Change a decision in `topology.yaml`. It is flagged as drift.
- Write a committed file except when a person presses a key in
  `doze-recall review` or the dash.
