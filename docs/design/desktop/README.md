# doze desktop

Status: plan, 2026-11-02. Nothing is built apart from a spike
(`~/Code/doze-dev/doze-desktop-spike`). The boards are on the canvas at
https://claude.ai/artifact/Uxg4UZCiEDQfYWYfm74nNK (private to its owner).

## What it's for

**The app is for people; the CLI is for machines.**

- **The app** is where a person on their computer looks at their stack:
  - what's awake and why;
  - how the services connect;
  - what's inside each one;
  - what happened when something broke.

  It tells them when something needs them, and is quiet otherwise.
- **The CLI** is what CI, coding agents, scripts and remote machines drive.
  It's built to be predictable and complete, not pretty.

doze, in one line: *OrbStack runs your containers. doze runs your stack, and
understands it.* It looks like OrbStack in shape (a light, fast Mac app with
a menu bar icon and a CLI underneath), but it knows what each service is: its
tables, queues and topics, who uses it, and why it's awake. That
understanding is what the app has to show within a minute of opening it.

## Decisions

| # | Decision | Why |
|---|---|---|
| D1 | A desktop app, built with **Wails v3**, Mac first, Linux next | Go, so it imports doze's packages; a web view, so doze-aws's and doze-kafka's consoles embed as they are. Tauri would give the same result in Rust; GPUI (native, the herdr way) would mean hand-building every widget and no embedded consoles |
| D2 | Shaped as a **menu bar app plus a window** | The menu bar is the ambient part: always there, about as light as the daemon. The window is opened when wanted and fully released when closed |
| D3 | It's built around **the map, inside a service, and the timeline**, on top of the TUI's daily loop | Those are what a terminal can't do, and they're where doze's position (it starts, proxies and watches everything) pays off |
| D4 | **The TUI is retired** | Its unique value is small once the app exists, and it's about 13,500 lines of upkeep. See Retiring the TUI |
| D5 | **The CLI stays first-class, for machines** | CI, coding agents, scripts and remote machines have no screen. It gets `--json` everywhere, meaningful exit codes, every action the app has, nothing that assumes a display or a person, and a short guide for agents. There's no investment in it as a human interface |
| D6 | **One binary**: the CLI, the app and the daemon are the same file | One artifact per platform; nothing to keep in step. See One binary |
| D7 | Built **without cgo**, using Wails' `purego` mode | It keeps doze's release as it is: pure Go, cross-compiled from one Linux runner |
| D8 | Notifications only for a crash, a failed task, or an instance stuck waking | Anything more trains people to ignore them |
| D9 | `doze.hcl` stays a text file | The app opens it in your editor and never edits it through forms |
| D10 | Remote stacks over SSH are later, not first | Worth doing; not needed to prove the app |

## One binary

- **macOS:** the binary lives inside `Doze.app` at `Contents/MacOS/doze`,
  because notifications, the menu bar identity, launch at login and signing
  need a real app bundle. On first launch the app links `/usr/local/bin/doze`
  to it, as VS Code does with `code`.
- **Linux:** the same binary, plus a `.desktop` file; AppImage or a tarball.
- **Servers, CI and agents:** the same binary downloaded on its own. The GUI
  never starts.
- **Which face runs:**
  - a command (`doze up`, `doze logs`) is the CLI;
  - `doze open`, or launching the app, is the GUI;
  - `doze __daemon` is the daemon, as today.

Measured on macOS with Wails v3.1.0 linked into a binary but never started:

| | Plain Go | Wails linked, unused (no cgo) |
|---|---|---|
| Size | 2.4 MB | 16.2 MB (doze today is 33 MB) |
| Time to run and exit | 3 ms | 6 ms |
| Idle footprint | 3.2 MB | 5.6 MB |
| Idle RSS | 4.6 MB | 12.9 MB |

The RSS growth is mostly macOS frameworks being mapped in. doze's bench
budgets the idle daemon by RSS (34 MB; the shop daemon is 24 MB), which leaves
little room. Either the GUI's frameworks load only when the GUI starts, or the
budget moves to footprint, which is the truer number on macOS. D0 decides.

## Retiring the TUI

1. **Now:** frozen. Fixes only, nothing new.
2. **When the app matches it on macOS and Linux** (home, instance pages,
   logs, module consoles, actions with confirmations, crash, setup), a bare
   `doze` changes:
   - on a desktop it opens the app at the current project;
   - with no display it prints the status table, as it already does when
     piped.
3. **One release later:** `internal/dash`, `tools/look` and the TUI boards are
   removed.

`doze status -w` and `doze logs -f` stay as the terminal's live views. What's
lost: browsing module consoles interactively from a terminal with no GUI.
D10 would cover that later.

## What the app shows

The Grounding board lists each thing as one of four kinds:
- **exists:** the daemon or TUI has it today;
- **new:** built from data doze already has;
- **needs work:** new, and needs doze or a module to change first;
- **not doing.**

The work it needs:
- **Project folders recorded** when doze starts a stack, so stopped stacks can
  be listed. Today only running daemons can be found.
- **Which app holds a connection,** in the proxy, so the map's lines are exact.
- **doze-aws streaming its request activity** to the daemon, for the timeline.
- **A terminal pane** (xterm.js), and the module's own `psql` found from its
  toolchain.
- **`doze open`.**
- **Signing, notarisation and auto-update.**

## Order of work

| Step | What |
|---|---|
| **D0** checks | On Linux: does the no-cgo binary run headless when the GTK and WebKitGTK libraries are missing? If not, D6 needs another answer there. On macOS: the daemon's idle RSS with the GUI linked, against the bench budget |
| **D1** the shell | One binary and `Doze.app`; the menu bar (every running stack, the count, the red dot); notifications; the window opened and released; `doze open`; project folders recorded |
| **D2** the daily loop | Home, instance pages, logs, actions with confirmations, crash, setup from a dropped folder: what the TUI does |
| **D3** what a terminal can't do | The map, inside a service, the timeline, the quick panel; connection attribution and doze-aws's activity stream |
| **D4** retire the TUI | Bare `doze` opens the app; then remove the dash |
| **D5** release | Signing, notarisation, auto-update, Linux packaging |

No estimates yet: D0 can change the plan, and D2 is the size of the TUI's work
so far. Each step is done when its boards and the built screens have been put
side by side.

## The spike (2026-11-01)

Wails v3.1.0, macOS arm64:

| | Result |
|---|---|
| Menu bar ready after launch | about 80 ms |
| Menu bar only | 18 MB footprint, about an idle doze daemon's 17 MB |
| Window open | about 223 MB; WebKit's GPU process is most of it |
| A minute after the window closes | about 42 MB |
| Cgo-free build | works |

Not seen by eye yet: the session that built it couldn't take screenshots.
