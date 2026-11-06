# UX audit: a stack, day to day (overview page: Tiles / List / Map, running and stopped)

Built binary `dist/Doze.app` with `--fixture shop`, `crash` and `long`, driven in WebKit (and Chromium for the Tab order and accessibility tree) at 1280×800 and 900×620, dark and light.
Screenshots: `test/desk-e2e/.desk-look/ux-2026-11-06/overview/` (abbreviated below as `ov/`).
Code paths are relative to `internal/desk/frontend/src/` for .tsx and to the repo root for .go.

Fixture caveats: in the fixture, wake and sleep land at once (no waking frames), and stack up/down run a real `doze up/down` in a fake folder, so they fail. I could still see what the page shows while a stack action is pending, and what it shows when one fails.

Counts: **0 blocker · 12 major · 17 minor · 7 polish**

---

## Major

### M1. Starting or stopping a stack shows no change on the page until it finishes; only a toast says anything
- **Where:** stopped stack, after clicking a row switch, Up as last time, ⏎, the Up switch or the sheet's Start. Also a running stack after flipping Up off.
- **What the user sees:** a toast says "bringing up worker…". The switch they just clicked snaps back to off, the head switch stays off, the strip still says **Down · last up 3d ago** with **Up as last time** live, and every row still says `down`. At 200 ms, 1 s and 3 s nothing differs. Stopping works the same way: the switch stays on and the figures stay put while the toast says "stopping shop…". (`ov/g-start-worker-200.png`, `ov/g-start-worker-3000.png`, `ov/h-down-shop-*.png`)
- **Why it's wrong:** the switch contradicts the action under way, and nothing stops a second click (another `doze up` or `down` races the first). `Service.Stack` starts a subprocess and returns at once; Home has no "starting" or "stopping" state (`internal/desk/serve/library.go:251-294`).
- **Fix:** add `Home.Pending` ("starting api, web…" / "stopping…") from Go, set when the action starts and cleared on refresh or failure. While it is set:
  - the head switch shows a busy state and is disabled;
  - the strip says "Starting… api, web, db" with the chosen rows marked waking;
  - Up, Choose and the row switches are disabled.

### M2. A failed start leaves no trace on the page
- **Where:** stopped stack, after a start fails.
- **What the user sees:** a red toast with the last line of output, `up acme failed: go: go.mod file not found…`. It doesn't say which service failed. The page still reads "Down · last up 3d ago" as if nothing happened (`ov/g-start-worker-3000.png`).
- **Fix:** keep the failure on the stopped strip ("Last start failed 10s ago: worker: go.mod not found · Logs · Try again") until the next attempt. Name the service whose start failed.

### M3. Esc with the ▾ menu open closes the menu and also leaves the stack for the Library
- **Where:** head ▾ menu, running or stopped.
- **What the user does:** opens ▾, presses Esc to dismiss it, and lands on the Library. Reproduced: after Esc, `h1` = "Library".
- **Why:** the overview's window keydown handler treats Esc as "library", and the menu doesn't stop the event from propagating (`pages/overview.tsx:211-217`, StackMenu in `pages/library.tsx`).
- **Fix:** have the menu handle Esc in the capture phase and call `stopPropagation`, as Dialog does. Also check every other popover on this page.

### M4. With a row's switch focused, space flips a different service
- **Where:** any view, keyboard focus on a row switch (Tab in Chromium, or WebKit with macOS Keyboard navigation on). Selection and focus are separate.
- **What happened in the test:** mongo was selected, the search switch had focus, and space put **mongo** to sleep. The search switch was untouched.
- **Why:** the window handler calls `preventDefault` on space and toggles the *selected* row, which cancels the native click on the focused button (`pages/overview.tsx:84-87`). Enter does the same: it opens the selected row's page while the focused button also fires.
- **Fix:** ignore the page's letter and space keys when `e.target` is a button or switch, or make focus and selection one thing (roving tabindex: focus = selection).

### M5. A filtered-out row still takes the keys
- **Where:** filter typed, selection now hidden.
- **What happened in the test:** web selected, filter "mongo" (one tile left), Esc, then space. **web** went to sleep, unseen. Afterwards web read `asleep · starts when needed`.
- **Why:** `row = rows.find(selected)` searches all rows, not `shown` (`pages/overview.tsx:31`).
- **Fix:** when the filter changes, move the selection to the first shown row, or resolve `row` from `shown` and do nothing when it isn't in it.

### M6. ⏎ means "open" on a running stack but "start the whole stack" on a stopped one
- **Where:** stopped stack, especially List view, where you select a row to read its pane.
- **What the user does:** selects web and presses ⏎ to open it, as they do all day (and as docs/help say: "Double-click or ⏎ opens it", "⏎ open"). That runs Up as last time for the whole stack (`pages/overview.tsx:60-62`).
- **Why it's wrong:** the same key does a cheap, reversible action in one state and an expensive one in the other. Double-click on a down row also does nothing, silently.
- **Fix:** keep ⏎ = open everywhere (a down service's page can show its doze.hcl facts and a Start button). Give Up as last time a key that isn't overloaded (`u`), or require ⌘⏎.

### M7. A service turned off reads "disabled · starts when needed"
- **Where:** running stack after `doze off search` (`ov/e-off-search-tiles.png`).
- **What the user sees:**
  - tile word **disabled**;
  - strip **1 off**;
  - switch aria "search is off";
  - Help: "off: not part of this run; nothing wakes it until you switch it on";
  - board: "off · not part of this run".
  The because text says **starts when needed**, which is false.
- **Why:** `LookOf` returns "disabled" for `Disabled` (`internal/display/words.go:43`). `Because` never checks Disabled and falls through to "starts when needed" (`words.go:205-209`).
- **Fix:** use one word, **off**, for the look. The because should say "not part of this run" (or "paused in doze.hcl" when `enabled = false`, see M8).

### M8. `enabled = false` and `doze off` are one state with one switch, but behave differently
- **Where:** running stack, a service with `enabled = false` in doze.hcl.
- **What the user sees:** the daemon reports Disabled for both cases (`internal/daemon/handler.go:107`). The row switch is clickable, with the title "doze off took it out of this run; the switch puts it back" (`pages/overview.tsx:653`). Clicking it sends `on` then `wake`. `on` only clears the runtime off flag (`internal/control/server.go:246`), so a config-paused service stays disabled and the wake fails or does nothing.
- **Why it's wrong:** the stopped page shows the same service correctly ("paused in doze.hcl", switch disabled), so the switch contradicts itself across states.
- **Fix:** send `Paused` separately from `Off` in the view. For a paused service, disable the switch and use the stopped page's wording.

### M9. Switch and aria words disagree with the row's state: waking, crashing and failed all read "is awake" and show on
- **Where:** row switches, all views.
- **What the user and screen reader see:** worker shows **waking**; its switch aria is "worker is awake", checked (same for events). Crashing api's switch aria is "api is awake", checked (`ov/k-crash-tiles-1280.png`). Code sets `On = !Disabled && word != "asleep"` (`internal/desk/view/home.go:250`), so a **failed** service (gave up after 5 tries) also shows its switch **on**. Help says crashing "then leaves it off".
- **Why it's wrong:** this is the same kind of contradiction as "switch on while the stack sleeps".
- **Fix:**
  - aria-label = name only, with the state as aria-description ("worker, waking").
  - failed → switch off (On=false), with a because like "gave up after 5 tries · switch on to try again".
  - Decide whether crashing counts as on ("wanted on, retrying") and say so in the because.

### M10. Map view: nodes are invisible to screen readers, the filter does nothing, and at 900 px the third column is cut off with no way to scroll to it
- **Where:** Map view.
- **What the user sees and gets:**
  - The map container is `role="img"`, so every node button inside is presentational to assistive tech (`pages/overview.tsx:960`).
  - `MapView` gets `rows`, not `shown`, so typing in the visible Filter changes nothing: "db" still showed 12 nodes (`ov/c-map-900-filter-db.png`).
  - At 900×620 the Outside/Tasks column is clipped (scrollWidth = clientWidth = 900; `.map` has overflow hidden) and can't be reached (`ov/b-map-900-light.png`, `ov/f-acme-map-900-light.png`).
- **Fix:**
  - use `role="group"` with an aria-label, plus a text alternative (a list of "api needs db, mongo, cache");
  - dim non-matching nodes and edges when filtering;
  - scale the drawing to fit width (`MapView.W` exists for that) or allow horizontal scroll.

### M11. Tiles, rows and nodes are buttons nested inside buttons
- **Where:** each tile and list row is a `<button>` containing the switch `<button>`, the address `<button>` and the copy `<button>`.
- **What a screen reader announces:** one button named "web web is awake awake · nothing holds it 213 MB https://dev.shop.example Copy the address of web", with more buttons inside it (Chromium aria snapshot).
- **Why it's wrong:** nested interactive content is invalid HTML and unpredictable in VoiceOver. It is also part of why focus and selection drift apart (M4).
- **Fix:** make the tile or row a `div role="row"/"option"` (or a listbox/grid with roving focus), with the switch, address and copy as sibling controls. Give it a concise accessible name: "web, awake, nothing holds it, 213 MB".

### M12. The sleep-with-dependants dialog's default sleeps more than was asked, and leaves out the board's explanation and the "Turn db off" choice
- **Where:** space on db while api uses it (`ov/d-sleep-dialog-db.png`).
- **What the user sees:** title "Sleep db?". The **default** (⏎) button is "Sleep db and api". The only explanation is "api is running and needs it… 4 connections". The ConfirmSleep board instead:
  - says "Two things are awake because of db. Putting it to sleep puts them to sleep too…";
  - lists each dependant with why it would wake db straight back (runs until stopped, pinned);
  - offers **Turn db off** as the way to keep it down.
- **Why it's wrong:** ⏎ quietly puts an app the developer is working on to sleep. In the run, api (pinned) went to sleep with db, and was then shown **pinned and asleep** (pin glyph on an asleep tile, `ov/h-menu-running.png`). "Sleep db only" is offered without saying it will bounce straight back.
- **Fix:**
  - port the board's sentence and per-dependant lines (Go has `Dependants`);
  - add "Turn db off";
  - default to the narrower action, or keep the wider default but state it in the title ("Sleep db and api?");
  - never put a pinned service to sleep as a side effect without naming that.

Also, both dialogs leave focus on `<body>` with no `aria-modal`, so VoiceOver is not taken into them. The start sheet doesn't trap Tab at all; Tab went to the Filter input behind the scrim. See m1.

---

## Minor

**m1. Focus handling in dialogs and the start sheet.** The sleep and restart dialogs: `document.activeElement` is `<body>` and there is no `aria-modal="true"`. The start sheet: Tab leaves to the page behind (focused `input[aria-label="Filter the services"]`). The sleep dialog's Tab moves a fake highlight rather than real focus. **Fix:** focus the default button on open, trap Tab, set `aria-modal`, and restore focus on close. (`components/dialog.tsx:29-40`, `pages/overview.tsx:381-407`)

**m2. The start sheet shows a box unticked for a service it will start anyway.** Untick api while web stays ticked, and the sheet says "Also starts, because they are needed: **api**, db, cache" while api's box stays empty (`ov/g-sheet-web-only.png`). **Fix:** show an implied state (a dimmed tick plus "with web") for picks that will start because they are needed.

**m3. "Start all" and "Everything" are side by side and nearly the same.** With all apps ticked, the primary reads "Start all" next to "Everything" (up --all). **Fix:** rename to "Start these" / "Everything in doze.hcl", or drop Everything when every pick is ticked. The button label "Start ⏎" with one pick reads as a bare verb: say "Start web".

**m4. The strip's "Up starts api, web and db, as last time" names db, but the sheet never lists modules.** db has no "LAST TIME" tag anywhere (`ov/f-acme-sheet-1280-dark.png`). **Fix:** have the strip line name only what the sheet can show, and mention modules separately ("…and db, cache because they need them").

**m5. The ▾ menu on a running stack offers ways to start, with a key that does something else here.** The label "More ways to start shop" is used while it runs. "As last time · everything declared" and "Everything · all 10" are the same set. The hint `space` belongs to the Library, but on the overview space wakes or sleeps a row (`ov/h-menu-running.png`). **Fix:** while running, show Down and Restart as last time, drop the duplicate, and label it "shop: up, down, restart".

**m6. The List view has no pin indicator.** Tiles and map show the pin glyph; the `List` rows don't render it (`pages/overview.tsx:796-817`). After pinning api the list row is unchanged (`ov/d-pinned-api.png`).

**m7. In the fixture, pinned api still reads "awake · nothing holds it".** The pane says "sleep: never · pinned". The real daemon adds a `pin` holder, so production reads "pinned awake", but the fixture's fake pin doesn't add one (`internal/control/fakedaemon/fakedaemon.go:245`). **Fix:** have the fake add the holder, so board-checks see the real words.

**m8. `s` does nothing on a waking row, while space and the switch put it to sleep.** Same switch, two key behaviours: `s` checks `row.awake` (active or idle), space checks `row.on` (`pages/overview.tsx:101-103, 621-623`). Help says "w s wake · sleep". **Fix:** use one rule. `w` on a waking row also sends a redundant wake.

**m9. Restarting with no dialog gives no feedback.** `r` on web with nothing holding it: no dialog, no toast, no row change at 0, 200 ms, 1 s or 3 s (`ov/d-restart-web-*.png`). A real daemon would show waking briefly, but a fast restart looks like nothing happened. **Fix:** a short "restarted web" toast, or a waking ring until ready.

**m10. Stopping the whole stack takes one click and never asks.** The head switch sits next to ▾ and runs `down` at once, while sleeping one db asks. Fine if intended (data kept), but the stack switch has no Undo either. **Fix:** at least a toast with "Up again".

**m11. The stopped List pane says "needs · needs api".** `DownPane` prints the key "needs" and then `row.because`, which already starts with "needs" (`pages/overview.tsx:466-470`). Same for modules ("used by · api uses it"). It also has no action buttons, so for mouse users the pane is a dead end. **Fix:** print the needs list itself, and add "Start with web".

**m12. Timeline and All logs stay live on a stopped stack and open empty pages.** `L` on acme opens "All logs · following · 0 lines kept" with no "the stack is down" note (`ov/f-acme-L-1280-dark.png`). **Fix:** say "acme is down; logs from its last run" and show them, or disable the buttons with a reason.

**m13. The memory figure shows a bare "0".** Cloud and search awake with no RAM reading yet show "0" in the memory slot (`ov/d-pinned-api.png`, cloud row; `display.MB` returns "0", `internal/display/format.go:18`). **Fix:** show "—" until the first reading, as `display.Mem` does while waking.

**m14. "Awake" counts differ between screens.** The strip's "N awake" counts only active (dozing separate, `home.go:338-345`). The tab and stack summary count dozing as awake (`internal/desk/serve/service.go:314`, `AwakeNow`), and the CPU key says "4 awake, the rest idle", using "idle", which isn't one of the state words. **Fix:** compute one count and use the same word everywhere ("the rest asleep or dozing").

**m15. The Map edge legend contradicts the rows.** api→cache is drawn as "needs, and keeps it awake" while cache reads "dozing · idle: sleeps soon". worker→events is drawn as "needs, not right now" while events reads "woken by worker" and worker "waiting on events" (`ov/run-map-1280-dark-full.png`, `ov/b-map-1280-dark-bottom.png`). The edge tone uses `AwakeNow` on both ends (`internal/desk/view/overview.go:113`). **Fix:** "on" only when the target is held by the source (Holders), "waiting" for booting-on-need. Also, the legend sits at the map's bottom-right and is off-screen until you scroll.

**m16. A filter with no matches shows a blank page.** Typing "zzz" leaves an empty area with no "Nothing matches 'zzz' · esc to clear" (`ov/i-filter-nomatch.png`).

**m17. The docs and Help don't match the stopped-state keys or ⏎ on a task.** Help: "⏎ open · a task: run", but on the overview ⏎ opens a task's page (`pages/overview.tsx:93`). Help and docs describe space as "wake · sleep: the same switch as on a tile", but on a stopped stack space starts the stack. Docs say "⌘L all logs"; the key strip and buttons say `L` (both work). **Fix:** align Help and docs with the code, and add a stopped-stack section to Help.

---

## Polish

- **p1.** "Set it up **(one password)**": the dim part is unreadable on the purple button, especially in light mode (`ov/b-tiles-900-light.png`).
- **p2.** The key strip's right note "from Library: click a stack" is meaningless on a stack page. Say something about this page, or drop it.
- **p3.** A stopped stack says "stopped · last up 3d ago" in the head **and** "Down · last up 3d ago" in the strip. There are two words for one state (stopped vs down; the Library also says "stopped"), and the head's copy gets truncated off first in narrow windows. Pick "down" (it matches rows, switch and aria) and say it once.
- **p4.** On a stopped stack, the task tile (migrate) is solid and bright while every service tile is dashed and dim, so the one thing that can't start reads as the liveliest (`ov/f-acme-tiles-1280-dark.png`). Its "·" glyph is nearly invisible.
- **p5.** The sleep dialog's "What happens" and "Data" rows carry green status dots that look like an "awake/OK" signal (`ov/d-sleep-dialog-db.png`).
- **p6.** Tile and list padding doesn't line up with the Filter (filter at x=16, groups at x=32 at 900 px; 28 and 42 in List at 1280). The filter's focus ring is a square inside a rounded field (`ov/c-map-900-filter-db.png`).
- **p7.** Truncated names and sentences have no tooltip: long service names in tiles and list (`ov/k-long-tiles-900.png`), and the pane's "sleep: Nothing holds it. It sleeps …" (`ov/run-list-1280-dark.png`). Add `title`, or let the pane wrap.

---

## Works well
- A stopped stack now keeps the same rows in the same places, every switch off, the strip turned into a Down strip with Up as last time and Choose (`c`). Its key strip changes with it ("space start with this one", "nothing runs while the stack is down").
- The start sheet's "Also starts, because they are needed" line works out the dependencies correctly, and the empty-selection message ("Tick at least one, or start everything") is clear.
- The Tiles · List · Map switch never moves between views; the view is remembered per stack in Go.
- Crash tiles read well: red tile, "crashing · try 3 of 5", the panic line in mono, the tab badge "1".
- The reset dialog is safe: the name must be typed, Cancel is the default, focus goes into the input, and the `doze reset db --yes` hint is there.
- Narrow (900 px) tiles and list degrade sensibly: the strip drops cores, and the pane stays usable.
- Light and dark both hold up apart from p1.
