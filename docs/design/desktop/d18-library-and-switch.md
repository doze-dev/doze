# D18 proposal: a front door, and a switch on every row

Status: proposal, 2026-11-05, for the owner's reaction. Nothing here is
built. The whole canvas (https://claude.ai/artifact/Uxg4UZCiEDQfYWYfm74nNK)
was redrawn from the ground up the same day at the owner's request: 33
boards on 8 pages (Begin; a stack day to day; inside a service; tasks;
when something is wrong; acting and asking; menu bar, settings, machine;
themes, sizes, long text), every frame titled with where it comes from.
Decisions taken in the redraw, pending his reaction: no terminal pane
(nothing to add over a real terminal); no "+" tab (the Library, ⌘O and a
dropped folder are how a stack arrives); the menu bar shows the bat alone
with at most one dot, never a number; the five TUI palettes plus light
are the themes; ⌘K is "needs you, recent, three things to do" when empty
and a verb-plus-name grammar when typed; the map and timeline are not
drawn until designed. Later the same day, after a rendered look: the
stack overview became tiles grouped by role over one quiet strip of
figures, with no sidebar on the landing page, and a quiet list beside
it; then the map was drawn (columns by role, a line per need, bright
when it keeps the other awake). **The owner kept all three: Tiles, List
and Map are the overview's views, switched in the header and remembered
per stack.** No tree view. The quick panel was dropped altogether;
spacing is never shrunk to fit a frame. App addresses are shown
port-less, since the front door on :80 already routes by host. **The window
opens at full screen size by default, as Postman does**: the owner's
call, "it functions better when there is more screen area". The frame
is remembered after that. It answers two things he raised once the CLI stopped being the
human interface: there is no logical starting point (how does one begin,
bring a project in, see every stack), and the listing is a terminal table
with verbs around it rather than a control surface. Everything is tagged
the way the Grounding board tags things: **exists**, **new** (from data
doze already has), **needs work** (doze or a module must change first).

## The problem, stated plainly

The app is organised around a stack tab, and a stack tab is reached by
`doze open` in a folder. That is the CLI's model wearing a window. Someone
who installs the app sees a bat in the menu bar and, with nothing running,
nothing else. The TUI never had this problem because a terminal is already
inside a folder; the app has no folder, so it needs a place of its own.

The second half: the table is the TUI's table. State, name, what, because,
memory, URL. To do anything to a row you reach for a key or the palette.
Every other desktop tool of this kind puts the control on the row.

## Prior art, and what to take

- **TablePlus, Xcode, Tower, Fork**: a welcome window that is a library of
  recent things, with open and create beside it. The library is the start
  page, not a dialog. Take: the library is the first tab and never closes.
- **Docker Desktop**: compose stacks grouped, a start/stop control on each
  row and on each container inside, a resource figure beside it. Take: the
  switch on the row. Leave: the weight, the ads, the three-level sidebar.
- **OrbStack**: a flat list of machines and containers, one control per
  row, sub-second to anything. Take: the flatness and the restraint.
- **Linear, Raycast**: keyboard-first, a command palette, density that
  rewards a developer. Take: j/k/space on the list, ⌘K for everything.
- **Proxyman, Postico**: the inspector pane for the nerdy detail, beside
  the list rather than instead of it. Take: an inspector, not a modal.

## The front door: the Library

The window opens on **Library**, a pinned first tab (⌘0, or the bat in the
top left). It lists every stack doze knows: running ones first with live
figures, stopped ones below with when they last ran. The data **exists**:
running daemons are found by their sockets; stopped ones by
`projects.List`, which records every folder doze has started.

```
┌─────────────────────────────────────────────────────────────────────┐
│ ● Library   ● shop   ○ data   ○ acme                     ⌘K  ⌘?  ⚙ │
├─────────────────────────────────────────────────────────────────────┤
│  Stacks                                     [ Open folder ] [ New ] │
│                                                                     │
│  ◉ shop        ~/code/shop        3 of 5 awake · 412 MB   [ ON  ]   │
│    https://dev.acme.com · web.shop.doze · 2 need you                 │
│  ◉ data        ~/code/data        1 of 3 awake · 90 MB    [ ON  ]   │
│  ○ acme        ~/work/acme        stopped · up 3 days ago [ OFF ]   │
│  ○ playground  ~/tmp/playground   stopped · up 2 weeks ago[ OFF ]   │
│                                                                     │
│  Drop a project folder anywhere in this window to add it.           │
│                                                                     │
│  Machine        names ✓  daemon ✓  domains: dev.acme.com ✓   Doctor │
└─────────────────────────────────────────────────────────────────────┘
```

- **The switch on a stack row is Up and Down.** On starts what it started
  last time (D15, exists); a long press or the ▾ beside it offers
  "everything" and "choose". Off is `doze down`. The row shows the count
  awake, memory, the addresses worth clicking, and the red count of things
  that need you. Click the name to open the tab.
- **Bringing a project in** is three gestures that end in the same place:
  drop a folder on the window or the Dock icon; **Open folder** (⌘O);
  `doze open` in a terminal, which the single instance already routes to
  the window (exists). A folder with a `doze.hcl` becomes a stack row and
  its tab opens. A folder without one gets the Empty page (exists): "found
  2 apps, 1 module", with its three ways to start. The library row appears
  the moment the stack first runs (exists).
- **New** makes a folder and a starter `doze.hcl` there, or copies the
  shop example into `~/doze/examples/shop` and starts it, so a first launch
  has something alive within a minute. The example as a real project is
  **needs work**: today the shop fixture is a fake daemon for tests.
- **Empty state**: one sentence and the two buttons, no illustration.
- **Machine** at the bottom is the one-line health of the setup (names,
  the daemon, domains), with Doctor beside it: the P3 setup page's steps,
  each with Repair, which is where `dns-setup`, the daemon update and a
  domain's trust prompt live for app users (exists as checks, **new** as a
  page, the elevation hook is P3).
- **The tray** lists the same rows with the same switch, running or not,
  so a stack can be brought up without the window (exists for running
  stacks; stopped ones are **new**, from the same list).
- **Forget** removes a stopped stack from the library and nothing from
  disk (**new**; the record is one file).

## Inside a stack: the list as a control surface

The sidebar stays (Services, Map, Timeline, then the instances). The
Services page changes from a table you read to a list you operate.

```
│ APPS                                                                 │
│ [ON ] 📌 web     awake   up 4m   84 MB   :3000  https://dev.acme.com  ⧉ │
│ [ON ]    api     dozing  idle 50s  —     :8080  api.shop.doze          ⧉ │
│ [OFF]    worker  off     —         —     —      needs pg, valkey         │
│ MODULES                                                              │
│ [ON ]    pg      awake   up 4m   120 MB  :5432  pg.shop.doze    psql  ⧉ │
│ [ON ]    valkey  asleep  —        —      :6379  held by api              │
│ OUTSIDE                                                              │
│       ○  stripe  reachable                      api.stripe.com           │
```

- **The switch is the state, and flipping it wakes or sleeps.** Decided by
  the owner on 2026-11-05 and kept on 2026-11-06 against the UX audit's
  "part of this run" recommendation: on while the service is up or on its
  way (awake, dozing, waking, crashing), off while it is asleep, failed,
  turned off (`doze off`, greyed; turning it on brings it back and wakes
  it) or the whole stack is stopped. A service paused in doze.hcl
  (`enabled = false`) has no switch, a lock instead. The switch can move by
  itself, since doze sleeps idle services: that is the state changing, and
  the word beside it says why. One rule in Go, `display.SwitchOn`, for
  every surface.
- **The pin** (📌) is `pin` (exists): never sleeps. Shown only when set or
  on hover, so the list stays quiet.
- **State word and because** stay; they are what no other tool has. The
  because column gets a tone, as now.
- **Address with copy** (⧉) on every row, and the https address where a
  domain is declared (exists). `psql` opens the module's own client in the
  terminal pane when that lands (P5).
- **Keyboard**: j/k move, space flips the switch, p pins, enter opens,
  / filters, ⌘K for anything. Letters off while a console frame has focus
  (exists as a rule).
- **Views**: table, tree (by needs, exists as data), and the map (P5).
  A segmented control in the toolbar, remembered per stack.
- **Filter**: a text filter and a "needs you" toggle that hides the quiet
  rows. Sort by state, name, memory.
- **Inspector**, not a modal: a right-hand pane for the selected row with
  the nerdy facts in one column: pid, bind address, the public address,
  the ports (public and private on macOS), env vars it was given with
  copy, last restart and why, the needs and holders both ways, the
  certificate for a domain and when it expires. All of this **exists** in
  the status and the instance view; the pane is **new**. Space toggles
  it, like Quick Look.
- **Density**: a compact mode for a stack with forty instances, remembered.

## What this changes in the plan

- P2 is done; this is **P2.6, before P3's install flow**, because the
  setup page in P3 should land inside a Library that already exists.
- Sizing, honestly: Library page with the stack switch and the tray rows
  2 days; the per-instance switch with the new Off verb 1.5 days (daemon
  verb, CLI `doze off <name>`? or `doze down <name>`, control protocol,
  tests); inspector pane 1.5 days; views, filter, keyboard, density 1 day;
  boards and an audit 1 day. **About 7 days.** The example project is
  P3-adjacent and not in this figure.
- D11 is amended rather than reversed: one window, a tab per stack, and a
  Library tab that is always first.
- Nothing here edits `doze.hcl` (D9 holds): the switch changes what runs,
  not what is declared.

## Open questions for the owner

1. The Off verb's name on the CLI, since the CLI gets every action the app
   has (D5): `doze off web` reads best beside `doze up web`.
2. Should the stack switch's default be "last time" (D15) or "everything"?
   D15 says last time, with everything one click away; this keeps it.
3. The example project on first launch: worth its half day in P3, or
   leave the Empty page to carry a new user?
