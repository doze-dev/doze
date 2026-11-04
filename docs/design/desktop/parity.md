# Parity with the TUI

Status: 2026-11-05, end of P2. The flip (P4: bare `doze` opens the app) waits
for every row below to be green and audited, and for the gate at the end.

Each row is one thing the TUI does, with the app's form of it, the test that
holds it and the board it was compared with. "Compared" means the screenshot
the test takes (`test/desk-e2e/.desk-look/`) was put beside the board
rendered at 1280×800 and looked at. Boards: `docs/design/desktop/boards/`.

| # | TUI thing | App form | Test | Board | State |
|---|---|---|---|---|---|
| 1 | Home: meters, grouped table, state·name·what·because·mem·url, selected instance's logs | Home page: four vitals, APPS/MODULES/OUTSIDE/TASKS table, the dock | `home.spec.ts` | Main | **Green.** Not carried: sort (`o`), tree (`t`), filter (`/`), line selection (`v`); the dock stands in for the detail pane |
| 2 | Instance page: logs (levels, search, runs, clock/relative), overview, last run, web console | Instance page with the same tabs, plus a module's console tabs | `home.spec.ts`, `console.spec.ts` | Instance, AppLogs, Task, PgActivity | **Green.** Not carried: `y` copies the URL, not the lines; `Y` (write a file) |
| 3 | All logs: merged, pause, level, search | Logs page: merged with doze's own lines, Show list, level, search; pause by scrolling up | `home.spec.ts` | Logs | **Green.** `space` is not bound; scrolling up pauses, the cap says so |
| 4 | Palette with the CLI form beside each row, Help, notices | ⌘K with `doze …` at the right of each row, `?` Keys sheet, toasts and Recent messages | `parity.spec.ts` | Search, Help, Notices | **Green** |
| 5 | Dialogs: Sleep (and dependants / only / Cancel), Restart, Reset (type the name, Cancel default, the script line), Ask, input | The same sentences and button order, over the dimmed window; a module action with input or a destructive one asks | `parity.spec.ts`, `console.spec.ts` | Confirm, Dialogs, Reset | **Green.** The Dialogs board draws restart with Cancel first and the "what happens" line as prose; the app keeps the dash's order and rows for every dialog |
| 6 | Verbs: wake, sleep, restart, reset, task, pin, up, apply; refusal while the daemon is down | Buttons on the head, the TUI's letter keys, every verb in ⌘K; a refusal is a toast in the dash's words | `parity.spec.ts` (sleep, restart, reset, task), Go `serve` tests (refusal) | Main, Instance | **Green** |
| 7 | Crash loop: "try n of m · next in Ns", last error | The row says it; the page opens on It exited, What doze does next, Its last run | `crash.spec.ts` | Crash | **Green.** Stop retrying, Open in editor, Env, Waiting on it, Told you are tagged new on the board and not built |
| 8 | Banners: connecting, not responding · retrying in Ns, last update Ns ago | The banner at the top right, the page faded under it | Go goldens (`view` tests) | Main (faded state) | **Green in Go; no browser test**: the fixtures cannot drop a daemon from Playwright |
| 9 | Empty folder: the three ways to start | The NewTab page, keys 1-3, Add one thing asks for the name; the starter file is the same bytes as the CLI's | `empty.spec.ts` | NewTab | **Green.** The registry list beside the choices is tagged new and not built |
| 10 | Module consoles from `ConsoleSpec`: row/col, table, stat, detail, stream, actions, note, chart; rates; actions via Admin | Block trees on the instance page's console tabs; the overview's Extend blocks | `console.spec.ts`, Go goldens | PgActivity | **Green.** chart and note blocks are drawn but no fixture has one, so they are held by Go only |
| 11 | Daemon auto-start when opening a project whose daemon is down | `doze open dir` starts it, as `doze dash` did; the empty page's choices start it after writing doze.hcl | `cmd/doze` tests of `face`; by hand | — | **Green by hand** |

## Beyond the TUI, built in P2

- Notifications (D8): a crash, an instance that gave up, a failed task; once
  per episode; silent while on screen; nothing about what was already so when
  the app first looked. `internal/desk/notify`, tested. Delivered through the
  system only from inside `Doze.app`; a bare binary prints what it would say.
- The window's frame is remembered across launches
  (`$DOZE_HOME/desk/prefs.json`); the theme is remembered by the page.
- The tab's red badge counts the instances in trouble.

## Not parity, and not yet built

Map, timeline, ⌥Space, new tab (⌘T), settings (⌘,), console iframe tabs for
doze-aws and doze-kafka, Terminal, StackStopped's choose-and-remember, the
right-click menu, hover-for-why, drag to select log lines. Each is tagged new
on its board.

## The flip gate (checked before P4)

- Every row above green with a browser test, or a Go test where the browser
  cannot reach it, and compared with its board.
- `deskbench.yml` and `bench.yml` green on main for 14 consecutive days.
- No open issue labelled `desktop-p1`.
- One release of dogfooding (release A: the app opt-in via `doze open`).
- Linux: builds and unit tests green; its budgets informational; the owner
  accepts in writing that Linux by-eye verification does not block the macOS
  flip.
