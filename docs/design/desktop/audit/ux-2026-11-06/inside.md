# UX audit — inside a service, its logs, and the stack's history

Scope: the instance page (app, module, outside service, task), asleep / waking / dozing / crashing states, port clash, module console tabs, framed web console, all logs, Timeline.
Method: built binary `dist/Doze.app/.../doze open --serve` with fixtures shop, crash, modules, portclash, long; WebKit through Playwright at 1280×800 and 900×620, dark and light. I read the code and compared each page with its board and with website/src/content/docs/cli/app.md.
Screenshots: `test/desk-e2e/.desk-look/ux-2026-11-06/inside/*.png` (SS/ below means that folder).
Paths are under `internal/desk/frontend/src/` unless they say otherwise.

Counts: **3 blocker · 12 major · 14 minor · 7 polish**

---

## Blockers

### B1. The head switch says "awake" (on) for a crashing, failed or waking service
- **Where:** instance head on every non-task page. Seen on the crash fixture's api, which is crash-looping (try 3 of 5), and on shop's events, which is waking.
- **What the user sees:** next to "process · go run ./cmd/api · crashing", the red dot and the red banner, the switch reads **awake** and is on. Its accessible name is "api is awake", aria-checked=true. On events the head shows "kafka · waking", the switch reads "awake", and the primary button is **Wake**.
- **Why it is wrong:** this is the same kind of contradiction the owner flagged today. A crash-looping process is not awake. Clicking the switch "off" calls `ask(sleep)` on a crashing service, which is meaningless.
- **Evidence:** pages/instance.tsx:24 `const on = !off && word !== "asleep"` and :113–114, which make every word other than asleep or disabled "on". SS/crash-api-overview-full.png, SS/shop-events-overview.png, SS/portclash-api.png.
- **Fix:** take the switch's label and state from Go, one rule for the head and the rows. Suggested mapping: awake or dozing → on/"awake"; waking → on/"waking…" with a busy state and no Wake button; crashing or failed → a third "trouble" state (off/red, label "crashing"), or hide the switch and keep Stop trying / Try now; asleep → off. The aria-label should name the control ("Keep api awake"), and aria-checked should carry the state.

### B2. Tab never moves keyboard focus on a service page
- **Where:** every instance tab, and the module console when it has one table.
- **What the user does:** presses Tab to reach a button, a Copy link or the filter. Focus stays on BODY and the page tabs flip between Overview and Logs instead. Measured: four presses left focus on BODY each time, while the selected tab went Logs → Overview → Logs → Overview.
- **Why it is wrong:** a keyboard-only or screen-reader user cannot reach Copy for the URL, the language tabs, Pin, Restart, Stop holder, Edit doze.hcl, the runs table or the needs chips. WCAG 2.1.1, and it hijacks the platform's most basic key.
- **Evidence:** pages/instance.tsx:34–39 (`case "Tab": e.preventDefault()` on window, for any target except inputs). components/console.tsx:68–75 does the same in the capture phase.
- **Fix:** move tab switching to ⌘⇧[ / ⌘⇧] or ←/→ while the tablist has focus (the WAI-ARIA tabs pattern), and leave Tab to the browser. If a single-key next-tab is still wanted, use a key that is not Tab.

### B3. Enter on any focused button on a task page also runs the task
- **Where:** a task's page, for example migrate.
- **What the user does:** focuses "Copy output" (or Edit doze.hcl, Copy, a row's Output) and presses Enter. Observed: the "copied to the clipboard" toast and **"running migrate…"** both appeared.
- **Why it is wrong:** tasks are often not idempotent (migrate, seed). A keyboard user who only wanted to copy re-runs a migration.
- **Evidence:** pages/instance.tsx:64–66 (window keydown, Enter → run, with no check that the target is a button or link). SS/shop-migrate-enter-on-copy.png.
- **Fix:** ignore Enter when `e.target` is a BUTTON, A or [role=tab], or anything focusable other than body. Better still, bind ⌘⏎ to Run.

---

## Major

### M1. A crashing service shows live-looking vitals and "Nothing holds it… it sleeps after 5:00"
- **Where:** crash fixture, api overview.
- **What the user sees:** "84 MB resident", "0.02 cores", "2 connections" with full charts, a card titled **"Awake because"** saying "Nothing holds it. It sleeps after 5:00 with nothing connected", and Recent showing "nothing yet". The fixture sets RAM, CPU and PID to 0 because the process is not running.
- **Why it is wrong:** these are stale series drawn as current, which is the "asleep with live charts" class of contradiction. A developer working through a crash reads them as "it's up and serving 2 clients". The crashes are also absent from Recent.
- **Evidence:** internal/desk/view/instance.go:233–241 (vitals from `st.Series` whatever the state); pages/instance.tsx:252 (header picks "Awake because" for every word except awake and disabled). SS/crash-api-overview-full.png.
- **Fix:** in Go, set Vitals only when `display.AwakeNow(v)`. Otherwise send a "last seen" variant with a dimmed chart and "not running · last reading 12:45:42". Replace the holders card on a crash with the board's "This episode" (try 1 exit 2 after 1.1 s, try 2…). Put crashed/failed events into Recent.

### M2. A task's failed run cannot be opened; "Runs · 3" and every row's Output show only the newest run
- **Where:** migrate, which has 3 runs: run 3 ok, run 2 exit 1, run 1 ok.
- **What the user does:** clicks **Output** on the red exit-1 row to find out why it failed. It switches to the "Runs · 3" tab, which shows the same 3 lines as Last run ("14 migrations already applied…") and "3 lines kept". `[` and `]` change nothing, and no run selector appears because the log has only one run's lines.
- **Why it is wrong:** the tab promises runs, and the table lists a failure with no way to read it. The TaskRan board selects the row (`.rr.sel`) and shows that run's output.
- **Evidence:** pages/instance.tsx:527 (every row's Output calls `onRuns` = setTab("logs") and does not say which run); pages/logs.tsx:152 (run segment only when `runs.length > 1`). SS/shop-migrate-row2-output.png, SS/shop-migrate-runs.png.
- **Fix:** a row's Output selects that run (pass its seq to LogsPage as the run filter) and highlights the row. When a run's lines were not kept, say so in the dock ("run 2's output was not kept: doze keeps the last N runs' lines").

### M3. The Last run "Output · run 3" card draws every run's lines
- **Where:** a task's Last run tab.
- **What happens:** the card's title names one run, but the embedded list asks for `run: 0`, which means every run. Copy and Copy output copy only the max run. With more than one run kept, what is shown and what is copied differ.
- **Evidence:** pages/instance.tsx:499 (`<LogsPage … bare />`, where runIdx defaults to 0, all runs) against :467–477 (copyOutput filters to the last run). I confirmed this in code; the fixture keeps lines for one run only, so I could not show it on screen.
- **Fix:** pass the run explicitly to the bare list, the same as M2.

### M4. No way to copy log lines (instance Logs, Runs, All logs)
- **Where:** every log dock.
- **What the user does:** wants to paste the panic and the lines around it into an issue or a chat. There is no Copy visible, no c to copy a line and no ⌘A. The list is virtualised with absolutely positioned rows, so a mouse selection covers only what is rendered.
- **Why it is wrong:** debugging a crash at speed is exactly this job. The InstanceLogs and AllLogs boards both have "Copy visible", "c copy line" and "⌘A copy all visible". The crash card's Copy is the only copy and covers only 2 lines (M5).
- **Evidence:** pages/logs.tsx:123–206 has no copy affordance. Board InstanceLogs.dc.html and AllLogs.dc.html. SS/crash-api-logs.png.
- **Fix:** add "Copy visible" (the filtered list, fetched in full from Go with n:0) to the toolbar, plus a focused line with c and ⌘A, as the boards draw them.

### M5. The crash card's Copy copies 2 lines while showing 6
- **Where:** crash and portclash, "What it said".
- **What happens:** the card shows started / panic / goroutine / main.loadConfig() / exit status 2 / panic. Copy puts `panic: STRIPE_KEY is required\nmain.loadConfig cmd/api/config.go:41` on the clipboard: the error and its "where", not the lines shown. The panic line is also drawn twice, once from lastRun and again as the red `crash.error` line.
- **Evidence:** pages/instance.tsx:229 (copies `[error, where]`) and :239 (appends the error again). SS/crash-api-overview-full.png.
- **Fix:** Copy copies exactly the lines shown, with times. Drop the appended error line when it already appears in lastRun, or draw it as "doze: exited with …" so it does not look like a second panic.

### M6. Keys that are drawn are not the keys that work, on crash and logs pages
- On a crash the head says **Stop trying `s`**, but pressing s does nothing: the handler only sleeps when `awake`, and does not call `off` (instance.tsx:44–46, :118–119). Measured: no dialog and no change.
- The crash page's key bar says **`w` wake** and has no `s`/`r` for Stop trying / Try now (instance.tsx:184). Pressing w sends "wake" to a crash-looping service.
- An app with a URL has no Sleep button (instance.tsx:137), but the key bar still lists **`s` sleep** (SS/shop-web-overview.png).
- **`y` copy address** is listed on every non-task page, including ones with no address (cache in shop). Pressing y gives no toast and no feedback.
- On the instance Logs tab the bar keeps showing the overview's keys (esc, tab, y, w). The logs keys that work there (/ filter, 1–4 level, t, space pause, [ ] run) are never listed, though the All logs page lists them.
- **Fix:** have Go send one key table per page and state (the console already does this with `con.keys`), and have both the buttons' kbd hints and the handler read from it, so the two cannot drift.

### M7. On a module console, a lowercase action letter triggers a hidden page verb
- **Where:** data/db "activity" and data/events "console".
- **What happens:** the console lists actions in capitals (C cancel query, T terminate session, P produce a message, K keep…), and they fire only on the uppercase key. A lowercase press falls through to the instance page's handler: **r** opened "Restart db? 3 connections open…" (measured), **p** pins events (instance.tsx:50–51), **s** asks to sleep, **w** wakes. None of these appear in the console's key bar.
- **Why it is wrong:** someone who presses "p" for "Produce a message" pins kafka awake without knowing it. Someone who presses "r" expecting nothing gets a restart dialog over their console.
- **Evidence:** components/console.tsx:77–83 (needs `e.key === e.key.toUpperCase()`); pages/instance.tsx:28–71 is still listening. SS/modules-db-console-r.png.
- **Fix:** match action keys case-insensitively, or show them as ⇧C. While a console tab is open, the instance handler should ignore letters the console doesn't list, or list them in the key bar.

### M8. Pin never shows "Unpin"
- **Where:** instance head.
- **What happens:** the page decides it is pinned with `holders.some(h => h.text === "pinned")`, but Go names the pill **"pinned awake"**. The button therefore always says "Pin awake", and pressing it on a pinned service unpins it while the label still invites pinning.
- **Evidence:** pages/instance.tsx:81 against internal/desk/view/instance.go:371. The board AppOverview shows "Unpin p".
- **Fix:** send a `pinned bool` from Go and read it.
- **Not confirmed on screen:** the fixture's pin is a no-op, so I could not show this live. Code inspection shows the bug.

### M9. A waking service is shown as fully awake on the Timeline and offered "Wake"
- **Where:** shop, events, which has been waking since 12:42.
- **What happens:** the head says "waking", the switch says awake, the primary button is Wake (`!awake && !off`, instance.tsx:142), and the card says "Awake because worker needs it… With neither…". On the Timeline its lane is a solid green "awake" bar to now. The legend's lighter "starting, up to ready" part is drawn only once a Ready event exists (timeline.tsx:67). The lane's dot is grey because dotClass has no "waking" (timeline.tsx:117–128), and worker likewise shows grey while the sidebar shows it waking.
- **Evidence:** SS/shop-events-overview.png, SS/shop-timeline.png, SS/crash-timeline-900-light.png.
- **Fix:** an open span with no ready is all "starting" (blue) up to now. Add waking (blue) to dotClass, ideally from Go's tone. On the page, a waking service shows "waking…" with no Wake button.

### M10. Timeline details are hover-only
- **Where:** Timeline.
- **What happens:** "how long, and why it woke" lives in `title` on span elements that cannot take focus (0 of 6 spans are focusable). The legend tells you to "hover a stretch". Crash marks also have only title and aria-label on a non-focusable span.
- **Why it is wrong:** keyboard and screen-reader users cannot get the one thing the chart exists to show, and on macOS the title tooltips are slow and easy to miss.
- **Fix:** make each span a button (or a list item in an sr-only list per lane) with its title as the accessible name, and show it in a details line under the chart on focus or click. "What happened" could also filter to a lane when its name is clicked, instead of only navigating.

### M11. Port clash: Stop gives no feedback when it fails, and its action label differs from its title
- **Where:** the PortClash card.
- **What happens:** "Stop bun" calls `stopHolder` with no catch. Go returns errors such as "that process no longer holds the port; nothing was stopped" or "that is doze's own…" without `Say`-ing them, so the click does nothing visible. On success, nothing on the card changes until the restart lands about 2 s later. The option is titled "Stop pid 28569 and start api" but the button says "Stop bun".
- **Evidence:** pages/instance.tsx:625; internal/desk/serve/portclash.go:85–117.
- **Fix:** show the result: disable the button with "stopping bun…", toast the Go error, and say "started api" or "api still can't listen". Label the button "Stop bun and start api".

### M12. "From your code" is cut off, and its Copy button covers the code
- **Where:** module overview, Snippets.
- **What happens:** the Node line `const url = process.env.DATABASE_URL ?? "postgres://shop@db.shop.doz…` is clipped at the card edge at 1280 and 900, with no visible scrollbar on macOS. At 900 the floating Copy button sits on top of line 1 ("starts y[Copy]"). The URL row in Connect is also truncated ("…5432/db?sslmode…").
- **Why it is wrong:** the developer who wants to connect right now sees an incomplete URL, and the most important string on the page has no full-text view.
- **Evidence:** SS/shop-db-overview.png, SS/modules-db-overview-900-light.png. `.code` has overflow-x:auto with no visible affordance.
- **Fix:** wrap long lines in the snippet (`white-space: pre-wrap; word-break: break-all` for the URL), reserve a right gutter for Copy, and give the URL row a title or wrap.

---

## Minor

1. **"With neither, it sleeps…"** is printed with one pill (events: "worker needs it") and with a pinned holder, where it is false: pinned never sleeps. *view/instance.go:387.* Say "When nothing holds it, it sleeps after 5:00", and when pinned say "Pinned: it stays awake until you unpin."
2. **"Awake because" / "nothing: it sleeps when the idle time is up"** on a dozing module (cache) reads as "awake because nothing". *instance.tsx:252, instance.go:382–383.* Title it "Sleeps in 1:48" when dozing. SS/shop-cache-overview.png.
3. **Two "console" tabs that behave oppositely.** A module console says "it never wakes db". A framed web console (mailpit "Console") wakes the service just by being opened, and the tab doesn't say so. The ModuleAsleep board titles it "Console · wakes it". *instance.tsx:202–203.*
4. **The web console never leaves "waking it for the console…"** when the frame fails to load. The head still says asleep, and in dark mode the frame is a blank white rectangle. There is no timeout and no error. *instance.tsx:570–602.* SS/shop-mailpit-console.png. Add a 10 s timeout with "it did not answer at …; Open in browser / see Logs", and paint the iframe background dark until load.
5. **The search count is always "N of N".** "8 of 8" for "orders" in All logs, "2 of 2" for "conn" in db logs. Total is the filtered count, so the figure says nothing. "lines kept" also changes meaning with the filter: Errors shows "1 lines kept", while the board says "3 of 212 lines". *logs.tsx:140, 176.* Show matches of the unfiltered total, and keep "kept" for the unfiltered count.
6. **Grammar in counts:** "1 errors", "1 warnings". *logs.tsx:174–175.*
7. **The Errors filter drops the panic's stack lines**, which have no level: you see "panic: STRIPE_KEY is required" without goroutine or main.loadConfig. SS/crash-api-logs-errors.png. Either keep unlevelled lines that follow an error line, or offer the board's "show the lines around them".
8. **No run boundaries in "All 2 runs".** Lines from run 1 and run 2 flow together. The board has a run header ("run 3 · started 13:50:40 · ready in 1.2 s · 212 lines"). Also, "All 2 runs" sits next to the banner's "try 3 of 5", which reads like a mismatch.
9. **An empty log dock has no empty state.** An asleep service's Logs shows a blank box with "▼ following · new lines appear at the end" and "0 lines kept". SS/shop-search-asleep-logs.png. Say "No lines yet: search has not run since the stack came up."
10. **Wrong hint texts.** The instance key bar's "from Overview: a row's name, or ⏎" appears on every tab, including the task and crash pages. The console's "from the Overview tab: Console, or ⏎ on an asleep module" is false: ⏎ does nothing on an asleep module (instance.tsx:64–66). *instance.tsx:187, console.tsx:125.* The docs list **⌘L** for all logs while the page says "L". Both work, but pick one to teach.
11. **Run and Time segments have no aria-pressed** (Level has it), so a screen reader can't tell Relative from Clock or This run from All runs. The extra "run 2 of 3" button made by `[` is an inert `.on` button. *logs.tsx:153–169.*
12. **Needs chips for unknown services are buttons that do nothing** when clicked. *instance.tsx:277.* Render them as plain text.
13. **The asleep page drops "From your code" and the env var**, even though it says "works while asleep: the first connection wakes it". The developer who opens a sleeping db to connect gets the rows but not the snippets. ModuleAsleep's board also shows "Env var SMTP_URL set on api, worker". SS/shop-db-after-sleep.png.
14. **The "Started by" column shows a filename** ("0015_orders…" is a truncated "0015_orders_index.sql changed"), and the Timeline says a task "woke" ("migrate woke (0015_orders_index.sql changed)"). Tasks run; they don't wake. Use "file changed: 0015_orders_index.sql" and "ran", as the TaskRan board's "you · Run again".

## Polish

1. **CPU figure repeats its unit:** "0.01 cores  cores · 1% of one". *view/instance.go:239–240.* Make Sub "1% of one core".
2. **The PortClash card's subtitle is uppercased** with its h3 ("· API CANNOT LISTEN WHILE BUN, PID 28569 HAS IT"). Every other card resets the subtitle's text-transform. *instance.tsx:615.* SS/portclash-api.png.
3. **Tab title casing:** "Overview", "activity", "console", "Logs". Capitalise the module-published titles.
4. **Duplicate try count on the crash banner:** "try 3 of 5;" (lower-case after a full stop) inside the sentence and again as a pill. The board's wording reads better: "doze will try 3 more times… next in 8 s".
5. **The console frame butts against the tab bar's underline** with no gap (SS/modules-db-console0.png). At 900 the log dock's left edge is indented further than the toolbar's (SS/crash-api-logs-900-light.png).
6. **The "copied to the clipboard" toast sits over the key bar** and the dock's corner (SS/crash-api-logs.png).
7. **The head path shows unexpanded `$PORT`** ("mailpit --smtp 127.0.0.1:$PORT"). Show the resolved port, or "the port doze gives it".

## Gaps against the boards (decide whether to build or drop from the boards)
- AppOverview: "Given to it" (PORT, DATABASE_URL, … as .env) is missing. It is the best answer to "what does my app see?"
- Crash: "What doze can tell you" (command, data dir, port from doze.hcl) and "This episode" (each try, its exit, the next one) are missing.
- ModuleAsleep: "last awake 40m ago, for 12m", "nothing here needs it right now", and SMTP / Console rows are missing.
- InstanceLogs and AllLogs: Copy visible, n next match, "show the lines around them", ⏎ open that service, service chips. (M4 covers copying.)

## Works well
- Module Connect card plus "From your code": the same URL in six forms, Copy export for the env var, and the honest "the first connection wakes it" note. Strong for the "connect to the db now" developer.
- The Sleep and Restart confirmations name what is affected (api and 3 connections), state that data is kept, and offer "Sleep db only".
- The crash banner and "What it said" put the panic first and the red tab badge leads straight there. ⌘K "Needs you" also opens it.
- The PortClash card names the holder (bun, pid) and is careful about doze-owned holders: Go re-checks before signalling and refuses doze's own.
- Module consoles (postgres activity, kafka topics/groups/tail, aws wire) are dense, readable, keep their selection, and say plainly "re-read every 2s while awake; it never wakes db".
- Logs: level, time and run segments, / to filter with highlighting, space to pause, "following / paused" status.
- The light theme and the 900-wide layout hold up apart from the snippet and Copy overlap (M12).
- Timeline's "What happened" list is clear, and lane names open the service.
