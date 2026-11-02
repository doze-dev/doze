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
understands it.* It looks like OrbStack in shape (a light, fast desktop app with
a menu bar icon and a CLI underneath), but it knows what each service is: its
tables, queues and topics, who uses it, and why it's awake. That
understanding is what the app has to show within a minute of opening it.

## What changes for someone using doze

| | Today | With the app |
|---|---|---|
| Typing `doze` alone | Opens the TUI | Opens the app at this project. With no display, prints the status table |
| The TUI | The main way in | Frozen now; removed one release after the app matches it |
| `doze up`, `logs`, `wake` and the rest | | The same, with `--json` and proper exit codes everywhere |
| Installing | A binary | `Doze.app`, or AppImage, deb and rpm, with the CLI inside and linked onto the PATH. Servers and CI still take the bare binary |
| `.doze` names | `doze doctor --fix` in a terminal | One system prompt on first launch |
| doze-aws's and doze-kafka's consoles | A browser tab | Inside the app; the browser still works |
| Menu bar and notifications | None | The bat, and notifications for a crash, a failed task or a service stuck waking |

What doesn't change: `doze.hcl`, the modules, and one daemon per stack with
its control socket. The app is a client of those daemons, as the TUI is, so
**quitting the app leaves stacks running**.

## Decisions

| # | Decision | Why |
|---|---|---|
| D1 | A desktop app, built with **Wails v3**, for **macOS and Linux together** from the first release | Wails is cross-platform; doze already runs on both. Go, so it imports doze's packages; a web view, so doze-aws's and doze-kafka's consoles open in it as they are. Tauri would give the same result in Rust; GPUI (native, the herdr way) would mean hand-building every widget and no consoles inside the app. Windows is out while doze itself doesn't support it |
| D2 | Shaped as a **menu bar (tray) app plus one window** | The tray is the ambient part: always there, about as light as the daemon. The window is opened when wanted and fully released when closed. On Linux the window and notifications must stand on their own: see Linux |
| D3 | It's built around **the map, inside a service, and the timeline**, on top of the TUI's daily loop | Those are what a terminal can't do, and they're where doze's position (it starts, proxies and watches everything) pays off |
| D4 | **The TUI is retired** | Its unique value is small once the app exists, and it's about 13,500 lines of upkeep. See Retiring the TUI |
| D5 | **The CLI stays first-class, for machines** | CI, coding agents, scripts and remote machines have no screen. It gets `--json` everywhere, meaningful exit codes, every action the app has, nothing that assumes a display or a person, and a short guide for agents. There's no investment in it as a human interface |
| D6 | **One binary**: the CLI, the app and the daemon are the same file | One artifact per platform; nothing to keep in step. See One binary |
| D7 | Built **without cgo**, using Wails' `purego` mode | It keeps doze's release as it is: pure Go, cross-compiled from one Linux runner |
| D8 | Notifications only for a crash, a failed task, or an instance stuck waking | Anything more trains people to ignore them |
| D9 | `doze.hcl` stays a text file | The app opens it in your editor and never edits it through forms |
| D15 | **Start what you choose, and remember it** | Up starts the services you tick and what they need, as `doze up api web` already does. `doze down` records which were up, and the next Up offers the same set, with Up everything beside it. The app also brings back its tabs, each tab's page and the window. Anything left asleep still wakes when something connects, and anything can be slept or started once the stack is up |
| D10 | Remote stacks over SSH are later, not first | Worth doing; not needed to prove the app |
| D11 | **One instance of the app; each stack is a tab** in its one window | Opening doze again, or `doze open` in another project, brings the window forward and opens that stack's tab. See One window |
| D12 | **Installed by drag and drop, then set up in the app on first launch**: one admin prompt for the command line and `.doze` names | As OrbStack does it. The same flow repairs the setup later and works on Linux, where an AppImage has no install step. See Installing |
| D13 | **doze-aws's and doze-kafka's consoles get a whole tab**, unchanged; or the browser, by setting | They're standalone products with their own top bar and rail, and they need the space. A full tab shows them as they are; links into them are their existing URLs; the only module work is taking the theme from the address. See The consoles |
| D14 | **Light by rule, checked in CI**: one web view, no frontend framework, live data only for what's on screen, the window released when closed | WebKit can't be avoided; abusing it can. See Staying light |

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

## Linux

Both platforms ship together, so every step is done on both. What differs:

| | macOS | Linux |
|---|---|---|
| Web view | WKWebView, part of the system | WebKitGTK, a library the user's system must have; packaged as a dependency (deb, rpm) or bundled (AppImage) |
| Tray | Always shown in the menu bar | StatusNotifierItem: KDE and Ubuntu's GNOME show it; stock GNOME (Fedora, for one) hides it unless the user adds an extension. The app can't rely on it: the window opens on launch when no tray is available, and notifications carry the "needs you" moments |
| Notifications | Notification Center, needs the app bundle | The desktop's notification service over D-Bus |
| Global shortcut (quick panel) | Works | X11 yes; on Wayland a global shortcut needs the desktop's portal, and not every desktop offers one. Fallback: the quick panel from the tray menu and the window |
| Packaging | `Doze.app` in a signed, notarised disk image or a Homebrew cask | AppImage, deb and rpm, each with a `.desktop` file |

## One window, a tab per stack

Wails v3.1 has this built in (`application.Options.SingleInstance`, on
macOS and Linux). A second launch hands its arguments and working folder to
the running app (`SecondInstanceData`) and exits. So:

- `doze open` in `~/code/data` brings the window forward and opens or selects
  the **data** tab;
- launching from the Dock or the app launcher only brings the window forward;
- it applies only when the GUI starts. CLI commands from the same binary never
  touch it.

Tabs run across the top of the window, one per open stack, each with its
state dot and a red count when something needs you. ⌘1–9 switches; ⌘W closes
the tab and leaves the stack running; ⌘T opens a new tab, which lists recent
projects and takes a dropped folder. Inside a tab, the sidebar switches
between Services, Map and Timeline and lists the stack's instances. Stacks
without a tab are still in the menu bar.

## Installing and first launch

As OrbStack does it: a disk image you drag to Applications, or
`brew install --cask`, then a short setup inside the app the first time it
opens. Not a `.pkg` installer, because:
- setup inside the app can say what each step does and let you skip it;
- the same screens repair the setup later, if an OS update removes part of it;
- it works the same on Linux, where an AppImage has no install step and root
  scripts in deb or rpm packages are the wrong place for it;
- when the CLI has already done it, the app sees that (`names.Check()`) and
  skips the step.

The steps:

1. **Welcome.** The bat and one line on what doze does.
2. **Command line and `.doze` names, one admin prompt.**
   - Links `doze` onto the PATH (`/usr/local/bin` is root's, which is why it
     shares the prompt).
   - Installs the names setup from doze-names, the same script
     `doze doctor --fix` runs: the `dev.doze.netd` helper and
     `/etc/resolver/doze` on macOS; the `doze-names` service, a sysctl file
     and, where needed, a dnsmasq file on Linux. doze-aws and doze-kafka share
     it, so this sets them up too.
   - The screen says what will change, with "see the script"
     (`names.Options{Print: true}`), then the system's prompt: the password or
     Touch ID dialog on macOS, polkit (`pkexec`) on Linux.
   - Skipping means services are reached on localhost ports, not `.doze`
     names.
3. **In the background.** No admin: start at login, for the menu bar; allow
   notifications, explained as only for a crash, a failed task or a service
   stuck waking.
4. **First project.** Drop a folder (detect proposes a `doze.hcl`), pick a
   recent one, or open the shop example.

Every step can be run again from Settings, with an Uninstall for the names
setup. `~/.doze` itself needs no privileges. On servers, `doze doctor --fix`
stays the way in.

macOS also has a newer way to install a helper (SMAppService, approved in
System Settings). It needs a signed app, so it's for after D5, if at all.

## The consoles

doze-aws and doze-kafka each serve their own console: Go templates, htmx and
Alpine, about 1 MB of assets each, with a top bar, a services rail and
light and dark themes set by `data-theme`. They stay in their modules,
unchanged.

**A console gets a whole tab.** It opens as its own tab in the tab strip,
right after its stack's tab (`shop`, then `cloud`), and fills the window
below it: its own top bar, rail, pages and keys, exactly as in a browser.
The app adds nothing inside the tab. ⌘W closes it; the service keeps
running. Instance pages for these services have an **Open console** button
instead of a console tab.

**Or the browser,** by choice: a setting, "Open consoles in doze / in your
browser". The links are the same either way, because they're plain URLs.

**How a console is found:**
- **As a module** (`module = "aws"` in `doze.hcl`): doze runs and sleeps it
  like any instance. Opening its console wakes it first.
- **Standalone** (`doze-aws` run on its own): it registers its names in the
  shared doze-names registry, where each entry says its owner (`doze-aws`,
  `doze-kafka`). The app lists those under Standalone, and their tab stands
  alone, with no stack before it. It can't sleep or wake them, because doze
  isn't running them.

**Links:**
- **Into a console** are its own routes, which exist today: the `orders`
  queue on the map or in the timeline opens `/sqs/orders` (doze-aws) or
  `/topics/orders` (doze-kafka) in the console's tab.
- **Out of a console** ("worker received this" opening `worker` in doze) need
  a `doze://` link the app registers. A console shows them only when the app
  opened it. Optional, and later.

**Theme:** the app passes light or dark, and the console uses it for that
tab. Both consoles already switch on `data-theme`; they need to accept it
from the address, not only from their own toggle and the system setting.

**Loading it lightly:** the tab holds the console in a frame inside the app's
one web view. It loads when the tab opens and is released when the tab closes
or has been unused for a few minutes. WebKit may give a frame from another
address its own process; D1 measures it.

The work, in each module: accept the theme from the address. Everything else
the consoles already have.

## Staying light

The engine is WebKit (WKWebView, WebKitGTK); that can't be avoided. The rest
can:

1. **One window, one web view.** Tabs are part of the page, not a web view
   each.
2. **No frontend framework, no build step.** Go renders HTML, with htmx and
   Alpine, as the consoles do. Fonts and scripts ship with the app; nothing
   comes from the network.
3. **Live data only for what's on screen.** Stats and logs stream for the
   tab you're looking at; a hidden window streams nothing. The menu bar talks
   to the daemons from Go, with no web view.
4. **Closing the window destroys the web view.** With no window, the app is
   about a daemon's size (18 MB in the spike).
5. **Nothing that keeps the GPU busy:** no blur, no looping animation, no
   WebGL. The map is SVG; charts redraw once a second while visible; long
   logs draw only the visible rows.
6. **Budgets in CI,** like the daemon's bench: menu bar only, window open, a
   minute after closing, and CPU at rest.

The spike's open window was about 223 MB, most of it WebKit's GPU process.
Rules 1, 4 and 5 shrink it; nothing removes it. The window's budget is set
from measurements after D1, not guessed now.

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
- **The consoles take the theme from the address** (doze-aws, doze-kafka);
  `doze://` links out of them are optional and later.
- **The first-launch setup:** the admin prompt (macOS dialog, polkit on
  Linux) running doze-names' existing script.
- **Resource budgets** for the app in CI.
- **Signing, notarisation and auto-update.**

## Order of work

| Step | What |
|---|---|
| **D0** checks | On Linux: does the no-cgo binary run headless when the GTK and WebKitGTK libraries are missing? If not, D6 needs another answer there. The spike run on Linux too: the tray on KDE and on GNOME, the window's memory, startup. On macOS: the daemon's idle RSS with the GUI linked, against the bench budget |
| **D1** the shell | One binary and `Doze.app`; one instance, a tab per stack; the first-launch setup; the menu bar (every running stack, the count, the red dot); notifications; the window opened and released; `doze open`; project folders recorded; the resource budgets in CI |
| **D2** the daily loop | Home, instance pages, logs, actions with confirmations, crash, setup from a dropped folder: what the TUI does |
| **D3** what a terminal can't do | The map, inside a service (console tabs for doze-aws and doze-kafka, and standalone ones listed), the timeline, the quick panel; connection attribution and doze-aws's activity stream |
| **D4** retire the TUI | Bare `doze` opens the app; then remove the dash |
| **D5** release | macOS signing and notarisation; AppImage, deb and rpm; auto-update on both |

Every step from D1 on is done on macOS and Linux together: a board isn't
matched until it's been compared on both.

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
