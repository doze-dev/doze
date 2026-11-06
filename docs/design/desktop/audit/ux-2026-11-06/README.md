# UX audit of the desktop app, 2026-11-06

Six independent auditors, each with one part of the app, drove the built app
(doze 9811be6..ab57826) in WebKit against the scripted fixtures (shop, acme,
crash, modules, badconfig, portclash, long, an empty folder), at 1280×800,
900×620 and 1920×1080, in all ten theme × mode looks. They read the code
behind every screen and compared it with the boards and the docs. Where a
fixture could not show a state (the menu bar, the native menu, daemon down,
"doze.hcl changed"), the finding is from the code and says so.

| Report | Area | Blocker | Major | Minor | Polish |
|---|---|---|---|---|---|
| [library.md](library.md) | arriving, the Library, folders, menu bar | 6 | 13 | 14 | 6 |
| [overview.md](overview.md) | a stack: tiles, list, map, stopped, start sheet, dialogs | 0 | 12 | 17 | 7 |
| [inside.md](inside.md) | a service: overview, logs, consoles, tasks, crash, port clash, timeline | 3 | 12 | 14 | 7 |
| [nav.md](nav.md) | broken config, banners, toasts, Settings, ⌘K, help, keys | 2 | 18 | 15 | 3 |
| [visual.md](visual.md) | visual system and accessibility, every theme and size | 0 | 9 | 13 | 7 |
| [journeys.md](journeys.md) | five end-to-end journeys, the vocabulary, other tools' habits | 3 | 12 | 15 | 3 |
| | **total (with overlap between auditors)** | **14** | **76** | **88** | **33** |

Screenshots are in `test/desk-e2e/.desk-look/ux-2026-11-06/` (git-ignored,
61 MB); the reports name each file.

Spot-checked in the code before writing this: `--line` is both a size and a
colour (tokens.css:6 and :32), Tab is taken by the service page
(instance.tsx:34), `.main` cannot scroll (shell.css:72), no application menu
is set (only the tray's), and the tray's `NeedsYou` is set but never read.
All true.

## What it comes to: eight root causes

Most of the 211 findings are symptoms of a few causes. Fixing the cause
clears the symptoms; patching symptoms one by one would not.

### 1. A switch that is a readout, not a control (the contradiction you hit, still everywhere)

The row switch shows "awake right now", so it moves by itself whenever doze
sleeps or wakes a service, while the CLI help, the design note D18 and the
in-app legend say it means "part of this run". It is announced "is awake" for
waking, crashing and failed services, shown on for a failed one, and the
service page's head switch says "awake" while the card below says it is
crash-looping. A service taken out of the run reads "disabled · wakes on a
connection"; `enabled = false` in doze.hcl and `doze off` are one flag with
one switch that behaves two ways.
*journeys B1, B2, B3 · overview M7, M8, M9 · inside B1, M8*

**Fix:** the switch is `on`/`off` (in this session or not) and moves only when
a person moves it, as in Docker Desktop and OrbStack. Awake, dozing, waking,
asleep, crashing are the dot and the word. `enabled = false` is **paused**
(no switch, a lock and the line in doze.hcl). Wake and Sleep are buttons on
the service page and keys, not the switch.

### 2. One word, several meanings; one fact, several sources

"awake" is counted three ways (strip 4, rows 5, menu bar 7); connections come
from two sources (Library 4, strip 6; "nothing holds it" beside a chart of 2
connections); "down", "off", "run", "ready", "idle" each mean several things;
a Library pill is green while the stack says the domain is not set up.
*journeys vocabulary table, M1–M4, M7 · library M1, M2, M10 · nav M1*

**Fix:** adopt the vocabulary table in journeys.md as the rule; compute each
count once in Go (`display`) and use it on every surface, menu bar included.

### 3. Actions with no in-between state

Starting or stopping a stack changes nothing on the page until the
subprocess ends: the switch snaps back, Up stays clickable and a second
click runs a second `up`. A failed start leaves only a toast. Double-click
starts twice. A crash card's Stop swallows errors.
*overview M1, M2 · library M4, M5, M12 · inside M11*

**Fix:** a pending state in Go per stack and per service (starting…,
stopping…, waking…), pushed at once, disabling the control; a failure stays
on the page as a banner with the output, not only a toast.

### 4. Keys that act on the wrong thing, or not as drawn

Space and Enter act on the *selected* row even when focus is on another
button (Space on "Doctor" stopped shop); a filtered-out row still takes keys;
Tab is hijacked on the service page (keyboard users cannot reach a single
control there); Enter on any button of a task page also runs the task; a
module console's action letters fire hidden page verbs (`r` opened Restart
db); keys drawn in key strips do nothing ("Stop trying `s`"); ⏎ means open on
a running stack and start on a stopped one; esc in a menu also leaves the
page; ⌘1–9 does not match the tabs; "esc back" goes up, not back.
*library B2, M7 · overview M3–M6 · inside B2, B3, M6, M7 · nav M6, M7 · visual M3*

**Fix:** one key layer in app.tsx: page keys only when focus is on the page
body (not on a button, menu, input or frame); Tab is never taken; each page
declares its keys once and the key strip is drawn from that list, so a drawn
key always works; ⌘[ ⌘] are back/forward, esc is up, and they say so.

### 5. The window's frame: menu, scrolling, narrow sizes

No doze application menu is set, so Wails' default appears: Help › Learn More
loads wails.io into the window with no way back, ⌘0 is Actual Size. The
Library cannot scroll; the ▾ menu is clipped by its table; below 1000 px
every sidebar disappears (Settings' Machine and About, the service list) and
content is indented twice; the help sheet cannot scroll.
*nav B1, B2, M15 · library B3, B4, M8 · visual M8 · overview M10*

**Fix:** a real doze menu (App, File: Open Folder ⌘O, Go: Library ⌘0 / Back /
Forward / Up, View, Window, Help: doze docs); `.main` scrolls; popovers
render outside clipping containers; a collapsed sidebar becomes a menu
button, never nothing.

### 6. Promised but not built, or wired only at boot

⌘O / Open folder does nothing while the window is open (the page is only
moved at boot); drag-and-drop is promised in four places and not built;
"Set it up from this repo" says it asks first but writes a postgres doze.hcl
and starts a daemon; Getting started tells people to run `doze shell`, which
does not exist; opening a stopped stack (`doze`, `doze open`) starts it.
*library B1, B5, B6 · journeys M5, M10*

**Fix:** push navigation for an opened folder at any time; build the drop
(Wails file drop on the window); make the empty-folder choices ask before
writing; fix the docs; `doze open` on a stopped stack opens it stopped.

### 7. A broken or changing doze.hcl tells different stories on different screens

Only the stack's Overview shows the error; the Library row, the tab dot and
the service pages stay green. A stopped stack with a broken file offers Up
everywhere while one line says nothing can start. The banner implies a fix
applies itself, the next banner says restart. Neither the daemon-down nor
the "doze.hcl changed" state has a fixture, so neither could be seen.
*nav M1–M5, M13 · library M3*

**Fix:** a config error and a stale daemon are stack-level states shown on
the tab dot, the Library row, the menu bar and every page of that stack, with
Up disabled while the file does not load; add `stale` and `changed` fixtures.

### 8. Accessibility and the visual system

Tiles and rows are buttons with buttons inside (43 axe nodes); dialogs don't
take or trap focus and aren't modal; the map is one image to a screen reader;
page changes are silent while the figures strip re-announces every second;
dim text on the selected row fails contrast in all ten themes; "(one
password)" is invisible on the accent button; off switches and unchecked
boxes nearly vanish in light themes; `--line` is both a size and a colour so
every line-height falls back to normal; six eyebrow styles, six kbd looks,
three switch sizes.
*visual M1–M9, m1–m13 · overview M10, M11 · nav M16 · library M13*

**Fix:** rows as a container with the name as the link and controls beside
it; a Dialog primitive with focus trap; a live region for state changes
only; rename the size token (`--lh`); a contrast test for `--dim` on
`--pick` and for borders (3:1), extending `internal/brand/contrast_test.go`;
axe-core in `test/desk-e2e` as a spec over every page.

## Suggested order

1. **The switch and the words** (causes 1 and 2): the model everything else
   leans on, and what an end developer meets first. About 2 days, Go
   (`display`, `view`) plus the frontend and the help legend and docs.
2. **Keys and focus** (cause 4 and the a11y half of 8): small, mechanical, and
   it stops the app acting on the wrong stack. About 1 day.
3. **The frame** (cause 5): menu, scrolling, narrow sidebar. About 1 day.
4. **Pending states and failures** (cause 3). About 1 day.
5. **Promised features** (cause 6): ⌘O at any time, drop, empty-folder
   choices, docs. About 1 day.
6. **Stack-level config and daemon states** (cause 7) with their fixtures.
   About 1 day.
7. **Visual system and contrast** (the rest of 8). About 1 day.

Roughly 8 days in all, each step with its own Playwright specs and a look at
every changed page.

## Status, same day: what was fixed

All eight root causes were worked through in the suggested order, one
commit each (875eb53..75f1dd6), with the end-to-end suite grown from 62 to
84 checks in WebKit and Chromium and the desk bench run after.

1. **The switch and the words** (875eb53). One deliberate departure from
   the audit: the owner had decided on 2026-11-05 that the switch *is*
   sleep/wake, so it stays the live state rather than "in this session";
   the contradictions inside that model are what was fixed. One rule in Go
   (`display.SwitchOn`) for every surface: on while up or on its way,
   crashing included; off when asleep, failed, off or stopped. `paused`
   (enabled = false, a lock, no switch) and `off` (doze off) replace
   `disabled`; `not run` replaces `ready`. One tally (`display.TallyOf`)
   counts for the strip, the Library and the menu bar; connections are
   clients everywhere; the menu bar shows its dot. A crashing service says
   crashing and shows no stale figures. The https pill is not a link until
   the domain is set up. Docs: `doze shell` is gone, D18 and the legend match.
2. **Keys and focus** (9213c41, 11af14c). One rule for page keys
   (`keys.ts`); a click does not take focus, as on macOS; keys act only on
   a shown row; dialogs, the start sheet and help trap focus and are modal;
   the ▾ menu owns its keys; stopping a stack asks when something is in
   use, forgetting always asks, off asks when something uses the service;
   ⌘1–9 count the tabs shown; esc from Settings goes back. Tiles and rows
   are containers with the name as their button; the map is a group of
   labelled nodes; Library rows and the window title speak.
3. **The frame** (3ab0fe1). A doze menu; the Library scrolls; the ▾ menu is
   not clipped; narrow windows keep their sidebars; help and the map scroll;
   Machine… opens Machine; Settings remembers its part.
4. **Pending states** (cfca124). Busy per stack, pushed at once; no second
   action; a failure stays on the page with its output.
5. **Promised features** (7c4dc59). ⌘O and drop work at any time; a
   never-run doze.hcl appears stopped; `doze open` never starts a stack;
   setting a folder up records it instead of starting a daemon.
6. **Config and daemon states** (295d6f3, 75f1dd6). A broken doze.hcl on the
   tab, the Library, every page and ⌘K; a silent daemon fades and freezes;
   new `changed` and `stale` fixtures; a saved doze.hcl is noticed on every
   page.
7. **Visual system and the rest** (7ff3f96). `--lh`; every theme in both
   modes contrast-tested, text and controls; announcements; and the other
   reports' items: copy names what it copied, toasts wrap, the sleep dialog
   spares pinned services and offers Turn off, ⌘K's near miss fills the box,
   the timeline draws waking, task runs show their own output, logs copy,
   snippets wrap, config errors say what to write, the narrow Library fits.

Found on the way, beyond the audit: Doze.app opened from the Finder never
showed a window (fixed earlier the same day); the fixture server ran real
`doze up` against the person's own `~/.doze` (fixed in cfca124); a Library
change that returned the list to an older pushed state was never sent
(7c4dc59); a lock taken twice hung the Library (fixed before commit).

### Left, and why

- **CLI meanings the app inherits**: "as last time" grows with services
  woken by connections before `doze down` (`cmd/doze/remember.go`); tasks
  have two rules for when they run on Up (journeys M12); `doze run` and
  `doze env` start a stopped stack. These are CLI decisions, for the owner.
- **The words "run" and "idle"** still mean both a process start and the
  session, and the timeout and the state, in CLI help and docs.
- **Visual polish**: six eyebrow styles, several kbd looks, three switch
  sizes, very wide windows, long-name truncation rules (visual m2–m6,
  m11, m12).
- **The daemon-down banner** has no action of its own (the switches are
  frozen and say why; a "restart the daemon" verb does not exist yet).
- **Boards** for screens that changed today (Settings' mode and theme, the
  start sheet, the failure banner, the stale and changed states) are not
  redrawn yet.
- **Desk bench, CPU at rest**: 0.10–0.13% against a 0.10% budget on the
  owner's Mac this afternoon, the same range as the code before the audit
  fixes under the same conditions (measured side by side); the budget is
  marginal on this machine, not newly broken.
