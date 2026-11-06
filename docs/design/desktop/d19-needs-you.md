# D19 · Needs you: see what is wrong sooner, and fix it where you are

Status: built 2026-11-06 (the owner said yes to all four questions below). Boards: page "9 · Needs you" on the canvas
(https://claude.ai/artifact/Uxg4UZCiEDQfYWYfm74nNK).

## The problem

The app already knows almost everything that can go wrong, but it says
each thing in one place only, and usually not the place you are looking at:

| What is wrong | Where it shows today | Where you miss it |
|---|---|---|
| a service crashing, a crash loop | its row, its page, the tab's red count, a notification | from another stack's page, once the notification is gone |
| a task that failed | its row, a notification | as soon as the notification fades |
| a stack that failed to start | a banner on that stack's page | from the Library or another tab |
| the daemon not answering | a banner on that stack's page, a yellow tab dot | everywhere else |
| doze.hcl does not load | the Library row, the stack's page | inside another stack |
| doze.hcl changed, restart to apply | the stack's page | everywhere else |
| a domain not set up | the stack's page, the Machine line | inside a stack |
| a machine part missing | the Library's Machine line, Settings › Machine | everywhere but the Library |
| things left behind (zombies) | "N left behind" on the Library's Machine line | everywhere but the Library |
| a stack whose folder is gone | its Library row | everywhere but the Library |

Toasts fade in seconds and say nothing you can act on. The Recent page is a
history, not a list of what is still wrong. ⌘K's "Needs you" section lists
troubled services only.

## The idea

**One list of what is wrong right now, built in Go from every source
above, shown on every page behind a bell, each item with its fix.** Items
leave the list by themselves when the problem is gone. It is a list of
problems, not a feed of messages: nothing to mark read, no count that only
grows.

Two kinds, and only two:

- **Broken now** (red): something you asked for is not happening. A crash
  or crash loop, a failed task, a stuck wake, a stack that failed to start,
  a daemon not answering.
- **Worth doing** (yellow): nothing is broken yet, but something is
  waiting on you. doze.hcl changed and not applied, doze.hcl does not load
  (the old one runs on), a domain not set up, a machine part missing,
  things left behind, a stack whose folder is gone.

**Only red interrupts.** Yellow waits in the bell. That rule is what lets
the bell stay trusted: if it only lights up for real breakage, people
look at it.

## Prior art

- **Xcode's issue navigator and VS Code's Problems panel**: the closest
  model. Derived from state, errors and warnings apart, counts in the
  window chrome, an item disappears when the cause is fixed. We borrow all
  of that.
- **Docker Desktop's bell, GitHub's notifications, Linear's inbox**:
  feeds. Good for messages from people; for machine state they go stale
  (an item about a container that recovered an hour ago). We do not build
  a feed; Recent stays the history.
- **Sentry's "ignore until it happens again"**: the right snooze for
  machine problems. Our "Not now" hides an item until what it says
  changes.
- **macOS Notification Center**: grouped per app, one actionable line,
  Show takes you there. We keep D8: one notification per episode, red
  only, never for what is on screen.

## The surfaces

1. **The bell, in the title bar, on every page** (board NeedsYouBell).
   Beside ⌘K. A red badge with the number of red items; a small yellow
   dot when only yellow ones are open; nothing when the list is empty.
   ⌘⇧N opens it, as does a click.
2. **The panel** (board NeedsYouPanel). Opens under the bell, over the
   page; the page stays where it was. Red first, then yellow, newest first
   within each. Each item: what happened in one line, the stack it is in,
   one detail line, and its fixes as buttons (the same verbs and dialogs
   the pages use: Restart, Stop pid N and start api, Run again, Open
   doze.hcl, Set up (one password), Clean up, Remove…). Show goes to the
   page that explains it. A fixed item turns grey with "fixed ✓" for a
   few seconds, then leaves. "Not now" on any item hides it until it
   changes. The foot links to Recent messages, the history.
3. **Nothing wrong** (board BellEmpty). The panel says so, and says what
   would show up there, so the empty state teaches the rule.
4. **Toasts that can act** (on NeedsYouBell). A red item appearing while
   the window is open gets a toast with its first fix and Show, and stays
   until acted on or closed (red only; yellow items never toast). Other
   toasts fade as now.
5. **The menu bar** (board MenuBarNeeds). The dot on the bat already
   exists for red. The menu gains a "Needs you" section above the stacks:
   up to three red items with their first fix. Yellow items appear as one
   line, "3 things worth doing · Open doze". Notifications get the fix as
   an action beside Show where macOS allows it.
6. **While you were away** (board WhileAway). When the window opens after
   being closed (the menu bar app ran on), a strip says what happened
   meanwhile: "While the window was closed (3 h): api crashed twice and
   recovered · migrate failed in data". Built from the stacks' journals
   and the notices. Got it, or opening the bell, puts it away.
7. **Checks before Up** (board PreFlight). Up from anywhere (switch,
   sheet, menu bar, ⌘K) first runs the cheap checks: the folder is there,
   doze.hcl loads, each declared port is free (and who holds it), the
   domains are set up. All good: it starts at once, with no extra click.
   Anything red (folder gone, file does not load, a port held by another
   process): the switch's sheet opens with the way out for each, and Up
   waits. Only yellow (a domain not set up): it starts, and the item
   lands in the bell.
8. **⌘K's Needs you section** uses the same list, red and yellow.

The pages keep their own banners and pills: the bell is the "anywhere"
view of the same items, not a replacement.

## The catalogue

Every item, where its data comes from today, its fix, and when it clears.
"new" marks work that does not exist yet.

| Item | Kind | Source today | Fix buttons | Clears when |
|---|---|---|---|---|
| `api` crashed / crash loop | red | `display.Troubled`, `Because` (try n of m) | Restart · Show logs · (port held) Stop pid N and start | it is running |
| port held by another process | red | `serve` port holder lookup (lsof/ss, cached) | Stop pid N and start · Edit doze.hcl | it is running |
| `migrate` failed | red | task last run exit ≠ 0 (runs kept) | Run again · Output | a run succeeds |
| `search` did not wake in time | red | notify rule 3 (wake → ready timer) | Restart · Show logs | it is ready |
| `shop` failed to start | red | `serve.failed` StackFailure | Try again · Show | started, or dismissed |
| daemon not answering | red | hub `Conn` not live with a pid alive | Restart the stack · Show | live again |
| doze.hcl changed | yellow | `ConfigChanged` (mtime > loaded) | Restart `shop` | restarted or file put back |
| doze.hcl does not load | yellow | `configError` (LoadShape on stamp change) | Open doze.hcl | it loads |
| domain not set up | yellow | `names.TrustRequest` check, Addr.Unready | Set up (one password) | set up |
| machine part missing | yellow | doctor checks, `fixable` | Repair (one password) · Copy fix | in place |
| things left behind | yellow, one item | `Leftovers()` count (15 s cache) | Clean up · Show | count is 0 |
| folder gone | yellow | `LibStack.Missing` | Remove… | removed, or folder back |

## Rules

- **One source.** `view.Attention` is computed in Go from the hub, the
  service's failures and caches, doctor and leftovers, and pushed as its
  own event when it changes (hashed like every view). The bell, the
  panel, ⌘K, the menu bar and notifications all read it. The page never
  derives an item (D16).
- **Identity.** An item's ID is kind + stack + instance; its fingerprint
  adds what it says (the error line, the try, the count). "Not now" stores
  the fingerprint in prefs; a new fingerprint shows the item again.
- **Interruptions.** Red: tab badge, bell badge, the bat's dot, a toast
  with its fix if the window is open, a notification if it is not on
  screen (D8, once per episode). Yellow: the bell's small dot and the
  panel. Nothing else.
- **Cost.** No new polling: every source is already computed for a page
  or cached (leftovers 15 s, doctor 30 s, config stamps 2 s). Building the
  list is a walk over the hub's stacks, done when they change. The
  checks before Up are run only on Up, and take under 100 ms (the port
  holder lookup is the slow part, and it is cached).
- **Words.** The first line says what happened in plain words, the
  second what doze is doing about it or what is next, as every
  bad-news screen does (the rule on page 5).

## Files

- `internal/desk/view/attention.go`: `Attention{Items, Red, Yellow}`,
  `Item{ID, Fingerprint, Kind, Stack, Key, Instance, Title, Detail, Fixes
  []Button, Show Nav, Since, Fixed}`, `AttentionOf(...)`; golden tests over
  the fixtures.
- `internal/desk/serve/attention.go`: gathers the sources, keeps "fixed"
  items for a few seconds, applies Not now, pushes `attention`;
  `Dismiss(id)`; `serve/preflight.go`: `Preflight(key) []Check` before
  every Up; `serve/away.go`: the summary since the window last closed.
- `internal/desk/prefs`: `NotNow map[string]string` (ID → fingerprint),
  `WindowClosed time.Time`.
- `internal/desk/tray`: the Needs you section from the same list (pure,
  table-tested); `internal/desk/app`: notification actions (verify what
  Wails v3.1 exposes on macOS before promising more than Show).
- Frontend: `components/bell.tsx` (button + panel), `components/away.tsx`,
  the start sheet's checks in `pages/overview.tsx` and the Library switch,
  actionable toasts in `components/toasts.tsx`, `store.ts` (an
  `attention` slice), `transport.ts` (`attention()`, `notNow(id)`,
  `preflight(key)`), ⌘K's section.
- Tests: Go goldens for each item kind; Playwright `needs.spec.ts` over
  the crash, badconfig, changed and stale fixtures (the bell's count, the
  panel, a fix, "fixed ✓", Not now, the empty state); a preflight spec with
  the portclash fixture; deskbench unchanged.

## Size

About 4–5 days. The list in Go with its tests, 1 day; the bell, panel and
acting toasts, 1 day; the menu bar section and notification actions, half
a day; while you were away, half a day; checks before Up, 1 day; specs,
screenshots against these boards and an independent audit, 1 day.

## To decide

1. ⌘⇧N for the bell, or another key?
2. Does a failed task stay red until a run succeeds (proposed), or leave
   on its own after a while?
3. A broken doze.hcl on a running stack: yellow (proposed; the old file
   runs on) or red?
4. Should the bell count yellow items too (a number), or only show a dot
   for them (proposed)?

## Built, and where it differs

Built as above: `view/attention.go` and `view/away.go`, `serve/attention.go`
and `serve/preflight.go`, the bell, panel, acting toasts, away strip and
checks sheet in the page, the menu bar's section (`app/tray.go`), the
notification's fix button, ⌘⇧N in the Go menu. An independent review found
14 issues; all are fixed except the last group below.

- ⌘K lists red items only; yellow ones wait in the bell, so the palette
  stays short.
- An item that leaves waits 5 s before it counts as fixed (a crash loop
  passes through "starting"), and is never shown fixed when its stack was
  mid-action, out of touch, stopped or removed, or when it was put aside or
  dismissed.
- The checks never offer to stop another stack's service, and a port held
  by another user's process (which lsof cannot see) is caught by trying to
  bind it.
- With no window open, the machine report and the leftovers are looked at
  every five minutes, not every half minute.
- What Up starts follows the CLI's rule (`chooseUpTargets`), tasks
  included.

The four left from the first pass, done the same day:
- Starting one service inside a running stack (its switch, Wake) looks
  first at the port it declares (`PreflightInstance`), with Stop pid N,
  then start, or Start anyway; never another stack's service.
- A wake that hangs without failing (still waking after 5 minutes, and
  silent for 2) is a red item and a notification, once per wake. The
  daemon's own ready check (60 s unless doze.hcl allows more) stays the
  source for a start that never answers.
- The menu bar section is `tray.NeedsOf`, table-tested; `app/tray.go`
  only draws it.
- A failed task stays red when its stack stops (its fix becomes Up), and
  after the stack is up again until a run of it succeeds.
