# doze next: the redesign

Status: phase 0, written 2026-10-10. Nothing here is implemented yet.

This directory is the plan for the next doze: a new config language, one
registry, outside services, and a rebuilt dash. The screens are drawn on the
Doze Dream Design Canvas (https://claude.ai/artifact/2wonaARTbNUxwRM6K26Sko,
private to its owner). These documents describe what has to exist underneath
them.

## How to read this

| Doc | What it answers |
|---|---|
| [gap-analysis.md](gap-analysis.md) | What the code does today, and what each part of the design needs that it doesn't have |
| [config.md](config.md) | Contract 1: the `module` / `service` / `app` / `task` language |
| [daemon-api.md](daemon-api.md) | Contract 2: states, events, logs and the control protocol |
| [networking.md](networking.md) | Contract 3: names, addresses, ports, and doze-names |
| [lifecycle.md](lifecycle.md) | Contract 4: how a service and a task run |
| [consoles.md](consoles.md) | Contract 5: how a module describes its console |
| [screens.md](screens.md) | Every canvas screen, the data it needs, and where that comes from |
| [phases.md](phases.md) | The build order, units of work, exit measures and estimates |

The facts about today's code come from a survey of `doze`, `doze-sdk`,
`doze-modules` and `doze-names` on 2026-10-10, with all four checkouts level
with origin. A handful of claims were re-checked by hand; the rest are cited
with file paths so they can be re-checked when a phase starts.

## The design in one page

- **Four nouns.** `module postgres "db" {}` comes from the doze registry.
  `service "mongo" {}` is anything from outside. `app "api" {}` is your code.
  `task "migrate" {}` runs once.
- **One reference rule.** `kind.name.attribute`: `module.db.url`,
  `service.mongo.address`, `app.api.url`, `task.migrate`.
- **One registry.** No mirror, no per-module source, no bring-your-own host.
- **A service is a command.** `run` and `port` are enough. `download` fetches
  the program first. `setup`, `on_ready` and `stop` cover multi-step cases.
  doze gives the command `$PORT` and `$DATA`. Containers are just a command
  the user writes (`docker run …`).
- **The dash has one frame.** Every row opens in it; only the first tab (the
  front) differs by kind. Logs for an app, last run for a task, overview for a
  database, a console for kafka and aws, a web link for things with their own
  UI.
- **Bare `doze` opens the dash.** Everything the dash can do is also a CLI
  verb.

## Tiers

Every feature on the canvas is in one of three tiers. Core ships first and
nothing in a lower tier may block it.

| Tier | Meaning | Examples |
|---|---|---|
| core | The design does not work without it | four nouns, one registry, services, `$PORT`, the frame and fronts, logs, copy, confirm, themes, kafka and aws consoles |
| should | Clearly valuable, has a known way to build it | tasks with `inputs`, `watch`, wake journal, stack-wide log search, `doze init` detection, `console =` web links, narrow layout |
| dream | Drawn on the canvas, but needs an invention or a risky change | caller attribution, wake trace with cold and warm timings, snapshots, "keep in doze.hcl", other-stacks list, TLS for `.doze` names |

[screens.md](screens.md) tiers each screen element.

## Decisions

Decided, by the project owner, during design:

| # | Decision |
|---|---|
| D1 | One registry only. Remove `modules { mirror }`, `module { source }` and the registry-hosts design. |
| D2 | Outside dependencies use a flat `service` block: `run`, `port`, optional `download`. |
| D3 | Multi-step services use `setup`, `on_ready`, `stop`, each a string or a list. |
| D4 | Modules take a keyword: `module postgres "db" {}`, referenced as `module.db.url`. |
| D5 | No container option. A container is a command in `run`. |
| D6 | The dash uses one frame with a per-kind front tab. |
| D7 | The TLD is `.doze`, for doze, doze-aws and doze-kafka. Decided 2026-10-10. It is already shipped, it is the brand, and it needs no migration. |
| D8 | The address split between module and standalone is intended. As a module, aws answers at `<name>.<stack>.doze` and `aws.<stack>.doze`; standalone it answers at `aws.<instance>.doze` and `127.0.0.1:4566`. Decided 2026-10-10. |
| D9 | The dash is built on the latest stable Charm libraries (Bubble Tea, Lip Gloss, Bubbles), whatever that is when phase 5 starts. Decided 2026-10-10. |
| D10 | There is no old syntax to support. doze has never gone live, so bare module blocks, `process`, `use` and the `modules` block are removed outright. No deprecation window and no `doze migrate`. Decided 2026-10-10. |
| D11 | No registry override of any kind. The mirror setting, `DOZE_MODULES_MIRROR` and the self-hosting docs are removed. `DOZE_<TYPE>_PLUGIN` (a locally built module, which the integration tests use) and `DOZE_MODULES=off` stay. The registry's signing key is compiled into doze instead of trusted on first use. Decided 2026-10-10. |
| D12 | An app that declares a `port` goes behind the proxy, like a service. doze also listens on `localhost:<port>` when that port is free. `direct = true` opts an app out. Decided 2026-10-10. |
| D13 | One loopback address per service. The pool grows from 55 to about 245 by using the rest of `127.0.0.x`. Decided 2026-10-10. |
| D13a | Closed by D29. Measured on macOS 27: 250 more loopback aliases put mDNSResponder at 100% of a core and a lookup over a second; 4,000 took a lookup to 150 s and configd was killed. A pool of aliases cannot grow. |
| D14 | Nothing is live: not doze, doze-aws, doze-kafka or doze-names. No change in this plan has to keep an older binary or an older file format working. Decided 2026-10-10. |
| D29 | A service's address is one of about 32,000, on every system. Linux and Windows bind `127.0.x.y` directly. macOS uses `198.19.0.0/17` on one interface held by a small daemon that setup installs (a copy of whichever doze program ran setup); a service listens on a private port and the daemon rewrites packets between that and its address, with no connection state. Tools call `Lease.Listen` or `Registry.Listen` in doze-names and do not know which system they are on. Supersedes D13. Chosen by Srini 2026-10-10 after a study of OrbStack, Tailscale, Telepresence, kubefwd and DDEV; proven in doze-names CI on macOS 14, 15 and 26 (2,000 services on one port, mDNSResponder idle, 2.6–4 Gbit/s). |
| D30 | Themes are cut. One palette, drawn well; `NO_COLOR` stays. Light variants and the other four accents go with them. Decided 2026-10-12. |
| D31 | The module overview has no snapshots panel until the daemon has snapshots to show (D18 stands). The reset confirmation says the data is gone. Decided 2026-10-12. |
| D32 | Levels are one line on the app's logs tab: counts of err, warn and info, and the lines that carry no level. The filter (`f`) does the work. Decided 2026-10-12. |
| D33 | The wake trace screen is cut. Its cause is in the table row ("woken by worker") and in the journal lines under it. `t` on Home is the dependency tree. Decided 2026-10-12. |
| D34 | Copy mode is cut. `y` copies the selected instance's address on Home and Focus, and the lines shown on a logs tab, over OSC 52. Selecting text is the terminal's (shift-drag while the dash holds the mouse). Decided 2026-10-12. |
| D35 | Wrap is cut. A long log line ends in an ellipsis, and `y` copies it whole; `w` stays wake. Decided 2026-10-12. |
| D36 | Per-source toggles and "quiet sources" on the stack's logs are cut. Search and the level filter do the work, and config has no per-source quiet setting. Decided 2026-10-12. |
| D37 | The empty screen lists no other stacks yet. The data is under `<home>/projects`, but the daemon does not serve it, so listing them is a daemon change first. It is a menu of the three ways to start. Decided 2026-10-12. |
| D38 | A module that declares no port takes the well-known one for its kind of server (postgres 5432, valkey 6379, kafka 9092, aws 4566, mariadb 3306, kvrocks 6380, ferret 27017). Ports are still never invented: temporal, an app and a service say their own, and two of a kind on one port are a reported conflict unless domains are on. Decided 2026-10-12. |
| D39 | The dash tells the truth about the daemon: a dropped or silent connection fades the page, says `daemon not responding · retrying in Ns` or `last update Ns ago`, reconnects with backoff, and refuses verbs meanwhile. Until the first frame it says `connecting`, never an empty stack. Decided 2026-10-12. |
| D40 | `r` asks like `s` does when something holds the instance. `l` is always this instance's logs and `L` the whole stack's; `?` lists every key from the same table the bar is drawn from. Decided 2026-10-12. |
| D41 | Copy says what was done: `sent … to the clipboard`, and when the terminal is not one known to honour OSC 52 it says `Y` writes a file instead. Decided 2026-10-12. |
| D42 | A failure is red, with `✗`, and stays until esc or 12 s whatever else is pressed; every message is kept (palette: recent messages). A success fades in 4 s. Decided 2026-10-12. |
| D43 | The dash is under WCAG 4.5 for text and 3.0 for borders on the grounds it draws on, and a test holds it there; dim is lifted on a selection. Decided 2026-10-12. |
| D44 | Glyph sets: `unicode`, `narrow` (CJK locales: only ambiguous-width glyphs replaced), `ascii` (non-UTF-8 locale, `TERM` dumb or linux, `DOZE_ASCII=1`). Chosen by `DOZE_GLYPHS` or detected; layout is unchanged. `TERM=dumb` also turns colour off. Decided 2026-10-12. |
| D45 | Totals are marked `≈`; Home's order holds 3 s after a key or click; under 60×16 the dash asks for a larger window; leaving it says the stack is still running. Decided 2026-10-12. |
| D46 | The meters show only what is consumed, never a ceiling that is a guess: `of ~X` (D15) is gone. Each bar is the whole of what the stack consumes, cut by instance, biggest first, with a line naming each colour and its size; CPU is said in cores (1 core = one core fully busy), not a bare percent. The key needs 34 rows; below that the bars and totals remain. Decided 2026-10-16. |
| D47 | The dash reads its window into one profile (width class, height class) that every part asks; no scattered thresholds. See adaptive-layout.md. Decided 2026-10-16. |
| D48 | Size is a budget spent in priority order, never a cliff: bars, then the key under them, then every table row, then more log lines. Decided 2026-10-16. |
| D49 | At 176 columns and up Home is master and detail (W-Home): the table at its natural width on the left, the selected instance's vitals and logs on the right. Decided 2026-10-16. |
| D50 | Columns stop at their content; extra width is spent on information, never on blank space. At 220 and up the table gains a memory trend. Decided 2026-10-16. |
| D51 | Home's detail charts cpu and memory only; connections are counted once, in the meters above (an instance's own page still charts them). Decided 2026-10-16. |
| D52 | The dash has five themes: violet (the default), teal, burgundy, amber and slate. Meaning does not change with the theme (good, warn and bad stay green, yellow and red-ish, and are kept apart from the accent); every theme passes the contrast test. Chosen from the palette (`:` then `theme`), `DOZE_THEME`, and remembered in the user config dir. Decided 2026-10-16. |
| D15 | "N MB of ~M if nothing slept" stays, using each service's memory from the last time it ran. Services that have never run are left out. Tier: should. Decided 2026-10-10. |
| D16 | Cold versus warm request timing is cut. The wake timeline stays. Decided 2026-10-10. |
| D17 | Per-app connection counts and the aws caller column are deferred. "Held by" ships from dependency holds. A two-day experiment after phase 3 decides keep or cut. Decided 2026-10-10. |
| D18 | Snapshots are deferred until after phase 7. Until then the reset confirmation says the data is gone. Decided 2026-10-10. |
| D19 | The "made at runtime" marker ships. Writing a resource back into doze.hcl is deferred. Decided 2026-10-10. |
| D20 | The crash diagnosis is cut. The crash screen shows the last error lines, the attempt count and what triggered the restart. Decided 2026-10-10. |
| D21 | A doze-served error page for a down app ships for the port-less front-door address only. Tier: should. Decided 2026-10-10. |
| D22 | Cross-module suggestions ("use ferret") are cut. Decided 2026-10-10. |
| D23 | The fixture stack is `examples/shop`: three small Go programs (api, worker, web) with the postgres, valkey, kafka and aws modules. Go only. mongo, mailpit and the tasks join in phase 4. Decided 2026-10-10. |
| D24 | Of the six bugs in the gap analysis, four were to be fixed in phase 0. Three were: the unverified warm cache, the wrong Linux resolver address, and the blank reset confirmation. The fourth, the restart race in sleep-all, was checked and is not a bug. The APPS mouse bug goes with the dash rebuild; the address leak is fixed in phase 3. Decided 2026-10-10. |
| D25 | The old dash keeps working until the new one replaces it, with no new features. Decided 2026-10-10. |
| D26 | Windows is out of scope for this plan. Decided 2026-10-10. |
| D27 | The website is rewritten at each milestone, for what that milestone shipped. Decided 2026-10-10. |
| D28 | The defaults written into the contracts stand: `stack`, `run`, `dir`; `ready = http(…)` with a default of "the port answers"; SIGTERM then SIGKILL; a failed `on_ready` leaves the service running but not ready; state names in code match what is shown; JSON over the unix socket with a handshake; 5 runs and 10 MB of logs per service; platform keys `mac`, `mac_intel`, `linux`, `linux_arm`. Decided 2026-10-10. |

Nothing is open as of 2026-10-10. New questions get an `O` number here, with
the phase they block.

## Method

- **Contracts before screens.** Almost every screen is a view of the five
  contracts. A screen with no supplier in [screens.md](screens.md) is new work
  or a cut.
- **Golden screens.** Each canvas screen becomes a snapshot test: a fake
  daemon feeds fixed data and the rendered dash is compared at a fixed size.
  `internal/tui/gallery_test.go` already renders ten screens at two sizes and
  checks widths; it is the seed.
- **One fixture stack.** The "shop" stack from the canvas becomes a real
  example under `examples/shop`, and every phase must keep it running end to
  end.
- **A measured exit for each phase.** There are no benchmarks or memory checks
  in the repo today. Phase 2 adds idle daemon weight and wake latency to CI,
  the way doze-aws enforces idle heap, before anything is built on top.
- **A decision log.** The two tables above. A decision moves from open to
  decided with a date and one line of reasoning.

## Prior art

Written from memory, not re-checked. Each phase re-reads the relevant tool
before copying anything from it.

| Tool | What it does that matters here | What doze does differently |
|---|---|---|
| process-compose | YAML process runner with a TUI, readiness probes, dependency ordering | No lazy boot, no names, no managed engines |
| mprocs, overmind | Run a set of commands with one pane each | Logs only. The current dash layout is modelled on mprocs. |
| Tilt | Resource list with a per-resource page, live update on file change | Kubernetes-first, browser UI |
| .NET Aspire dashboard | Resources, structured logs, traces and a per-resource console in one place | Browser UI, tied to .NET orchestration |
| k9s | One frame, a different view per resource kind, `:` command mode, `/` filter | The closest model for "one frame, many kinds" |
| lazygit, lazydocker | Dense panes, a consistent key bar, confirmations that say what will happen | Reference for key bar and confirm wording |
| puma-dev, pow | Boot an app on first request to its `.test` name, stop it when idle | HTTP only. The closest prior art to doze's wake-on-connect. |
| systemd socket activation | The supervisor owns the listening socket and starts the service on first connection | The same mechanism as doze's proxy, at OS level |
| devenv, devbox, mise | Declare services and tools per project | Always-on services, no proxy, no dash |
| Laravel Valet and Herd, DDEV, OrbStack | Local names for local services | Useful for how they install resolver configuration and what goes wrong |
