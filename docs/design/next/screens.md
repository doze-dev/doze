# Screen inventory

Every screen on the canvas, what it shows, and where that comes from. A row
with status **new** has no supplier today.

- **Contract** names the document that supplies the data: config,
  [daemon](daemon-api.md), [net](networking.md), [life](lifecycle.md),
  [console](consoles.md), or dash (drawn by the dash with no new data).
- **Status**: have (works today), wire (pieces exist), new.
- **Phase** is where it gets built ([phases.md](phases.md)).

The canvas is at https://claude.ai/artifact/2wonaARTbNUxwRM6K26Sko and is
private to its owner. Screen names below match the board titles.

## Page 1: the dash

| Screen | Element | Contract | Status | Tier | Phase |
|---|---|---|---|---|---|
| Home | Rows grouped by kind (apps, modules, outside, tasks) | config, daemon | wire | core | 5 |
| | State with glyph and word | daemon | wire | core | 2, 5 |
| | Memory per row and total | daemon `stats` | wire | core | 2, 5 |
| | "of ~2.9 GB if nothing slept", from each service's last-run memory (D15) | daemon `stats`, state | new | should | 2, 5 |
| | "sleeps in 3:12" | daemon | have | core | 5 |
| | "woken by worker" | daemon journal | new | should | 2, 5 |
| | Right pane: a small version of the front tab | dash | new | core | 5 |
| | `zzz` lines in logs | daemon journal | new | should | 2, 5 |
| Command palette | Fuzzy list of verbs | dash | have | core | 5 |
| | The CLI form beside each row | dash, action registry | wire | core | 5 |
| | "restore db from snapshot" | life | new | dream | none |
| Wake trace | Who needs whom, as a tree | config | have | should | 5 |
| | One wake as a timeline with durations | daemon journal | new | should | 2, 5 |
| | Cold versus warm request time | cut (D16) | | | |
| | Earlier wakes list | daemon journal | new | should | 5 |
| No doze.hcl here yet | Three ways to start | dash | new | should | 7 |
| | "found go.mod, …" | detection | new | should | 7 |
| | Other stacks, running or not | `<home>/projects`, `stacks.json` | wire | dream | 7 |
| Home at 80 × 24 | Single-column home | dash | new | should | 5 |

Changes from the canvas:

- "of ~2.9 GB if nothing slept" is computed from each service's memory the
  last time it ran. A service that has never run is left out, and the figure
  is labelled as an estimate.
- The wake trace keeps the timeline and loses the cold and warm comparison,
  which would need doze to time HTTP requests.

## Page 2: a view for each kind

| Screen | Element | Contract | Status | Tier | Phase |
|---|---|---|---|---|---|
| The rule | Front tab by kind, shared keys | dash, console | new | core | 5, 6 |
| | `front`, `console`, `logs` attributes | config | new | core | 1 |
| App | Logs with timestamps and levels | daemon logs | new | core | 2, 5 |
| | Earlier runs | daemon logs | new | should | 2, 5 |
| | Needs, with state | daemon holders | wire | core | 5 |
| | "restarted, you saved orders.go" | life `watch`, journal | new | should | 4, 5 |
| | Level counts | dash | new | should | 5 |
| Database module | Address, shell command, reference | config, daemon | have | core | 5 |
| | "awake because": holders and connection count | daemon holders | wire | core | 2, 5 |
| | Connections per app | net caller identity | new | dream | none |
| | Vitals: CPU, memory, connections, disk | daemon `stats` | wire | core | 5 |
| | Snapshots | life | new | dream | none |
| | Recent: journal and log lines merged | daemon | new | should | 5 |
| Task | Last run output | daemon logs, life | new | should | 4, 5 |
| | Runs when, history with reasons | life, journal | new | should | 4, 5 |
| kafka console | Topics, groups, lag | console (admin trio) | have | core | 6 |
| | Rate | console `Role: rate` | wire | core | 6 |
| | Tail of a topic | console `Tail` | new | core | 6 |
| | "runtime" marker | console inventory diff | wire | should | 6 |
| | Keep in doze.hcl | console `RenderObject` | new | dream | none |
| | Produce a message | console action | new | should | 6 |
| aws console | Resources and services in use | console | wire | core | 6 |
| | The wire | console `Tail` | wire | core | 6 |
| | Caller column | net caller identity | new | dream | none |
| | "asked for upload, you have uploads" hint | aws module | new | dream | none |
| Service with a web console | Console address, open in browser | config `console`, net | new | should | 3, 6 |
| | "used by", "last awake" | daemon holders, journal | new | should | 5 |

Changes from the canvas:

- "api · 4 connections" becomes "4 connections · held by api, migrate" until
  caller identity exists.
- Snapshots leave the database view for now.

## Page 3: logs, copy, confirm, themes

| Screen | Element | Contract | Status | Tier | Phase |
|---|---|---|---|---|---|
| Stack logs | Merged, time-ordered, source-coloured | daemon logs | new | core | 2, 5 |
| | Source toggles and quiet sources | config `logs`, dash | new | should | 5 |
| | Search with highlighted matches | dash | new | core | 5 |
| | Level filter | dash | new | should | 5 |
| Copy mode | Line selection, copy, copy without timestamps | dash | wire | core | 5 |
| | Save to file | dash | new | should | 5 |
| | Copy over SSH (OSC 52) | dash | new | core | 5 |
| | Copy menu on a row | config, daemon | wire | core | 5 |
| Confirm: in use | Who is affected, what happens next | daemon holders | wire | core | 5 |
| | "Sleep db and api" | daemon verb | new | should | 5 |
| Confirm: deletes data | Size on disk, what restarts | daemon | wire | core | 5 |
| | Type the name to confirm | dash | new | core | 5 |
| | Snapshot first | life | new | dream | none |
| Themes | Five accents, live preview | dash | wire | core | 5 |
| | Light variants for all five | dash | new | core | 5 |
| | Follow terminal, plain ASCII, no colour | dash | wire | should | 5 |

Changes from the canvas:

- The canvas theme colours for emerald, amber, cyan and rose were invented.
  The real ones are in `internal/tui/theme.go` and the golden screens use
  those.
- The reset confirmation says the data is gone. The automatic snapshot comes
  with snapshots, if they are built.

## Page 4: doze.hcl and adding

| Screen | Element | Contract | Status | Tier | Phase |
|---|---|---|---|---|---|
| doze.hcl | The four nouns, references | config | new | core | 1 |
| The service block | `run`, `port`, `download`, `$PORT`, `$DATA` | config, life, net | new | core | 1, 3, 4 |
| | Platform map for `download` | life | new | should | 4 |
| More than one command | `setup`, `on_ready`, `stop` | life | new | core | 4 |
| | Per-step lines in `doze up` | daemon journal | new | core | 2, 4 |
| doze add | Catalog of modules | registry `index.json` | have | core | 7 |
| | Version picker, rename | dash | new | should | 7 |
| | Preview of the block and the wiring line | config (hclwrite) | wire | should | 7 |
| | "Found 2 apps not in doze.hcl" | detection | new | dream | 7 |
| doze add mongo | Not a module; three ways to bring it | config | new | should | 7 |
| | "found mongod at …" | PATH lookup | new | should | 7 |
| | "ferret speaks the MongoDB protocol" | cut (D22) | | | |
| | "found bin/mongod in the archive" | life download | new | dream | 7 |
| doze init | Detect apps, compose services, tasks, env | detection | new | should | 7 |

## Page 5: CLI and bad days

| Screen | Element | Contract | Status | Tier | Phase |
|---|---|---|---|---|---|
| doze up | Per-service lines with durations | daemon journal | wire | core | 2 |
| | Summary with addresses | daemon | have | core | 2 |
| doze status | Table with state, address, memory, note | daemon | have | core | 2 |
| doze env | Env for one app | config | have | core | 2 |
| doze run | "woke cloud in 90 ms", "sleeps in 5:00" | daemon journal | new | should | 2 |
| doze sync | Which blocks changed and what restarts | config diff | new | should | 7 |
| | Object-level plan (buckets, topics) | state plan | have | core | 7 |
| doze down | Per-service lines | daemon | have | core | 2 |
| Crash-looping app | `crashing` state, attempt count, next try | daemon | new | core | 2, 5 |
| | Last error lines | daemon logs | new | core | 2, 5 |
| | "STRIPE_KEY is in .env.example, not .env" | cut (D20) | | | |
| | A useful page for requests to a down app, front door only (D21) | net front door | new | should | 3 |
| Port taken | Who holds the port | OS lookup | new | should | 7 |
| | Options, including "its own address" | net | wire | should | 7 |
| doze doctor | Checks, with `--fix` | net `names.Install` | wire | core | 3 |

## What had no supplier, and what was decided

| Item | Decision |
|---|---|
| Estimated memory if nothing slept | Kept, from last-run memory (D15) |
| Cold versus warm request timing | Cut (D16) |
| Connections per app, and the aws caller column | Deferred; experiment after phase 3 (D17) |
| Snapshots, and snapshot-before-reset | Deferred until after phase 7 (D18) |
| Keep in doze.hcl | Marker ships; writing back deferred (D19) |
| The crash diagnosis | Cut (D20) |
| A doze-served error page for a down app | Kept, front door only (D21) |
| Cross-module suggestions | Cut (D22) |

The canvas still shows the cut items. They come off it when the screens are
next revised.
