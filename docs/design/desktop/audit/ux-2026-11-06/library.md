# UX audit: arriving and the Library

I tested the built binary (`dist/Doze.app`, built 12:43 after the latest source edits) in `--serve` mode under WebKit at 1280×800, 900×620 and 640×400, dark and light. I used the shop, crash and long fixtures, plus a folder with no doze.hcl, a package.json-only folder and a doze.hcl that has never run. States the fixtures cannot reach (no stacks, a daemon that stopped answering, a row run by an older doze, many stacks, "Also on this machine", Set up) were made by rewriting the `/api/library` answer in Playwright. Calls to `/api/stack` were intercepted so nothing really started or stopped. The tray menu and the window were read from code because they cannot run beside the user's app.

Screenshots: `test/desk-e2e/.desk-look/ux-2026-11-06/library/` (named `NN-*.png` below).

**Counts: 6 blocker, 13 major, 14 minor, 6 polish.**

---

## Blockers

### B1. Open folder… / ⌘O does nothing while the window is open: no empty-folder page, no new project
- **Screen:** Library, with the window open (that is, always, since ⌘O lives in the window). The same happens with `doze open <dir>` while the window is already open.
- **What the user sees:** they pick a folder with no doze.hcl. The window stays on the Library and nothing happens: no page, no toast. Picking a folder whose doze.hcl has never run does the same. Verified: `33-after-cmdO-empty.png` and `34-after-cmdO-hcl.png` are the Library unchanged.
- **Why:** `API.ChooseFolder` → `Service.Open(dir)` only sets `pendingDir`. `push()` (serve/service.go:229-244) navigates only when `KeyFor(dir)` finds a stack the hub already knows. The `{page:"empty"}` nav exists only in `Pending()` (service.go:680-686), which the page reads once at boot. A doze.hcl that has never run is not in the hub at all, so it can never resolve. The frontend's `chooseFolder()` ignores the dir that comes back (store.ts:218).
- **Effect:** the first thing the empty Library asks a new user to do does nothing. The "three ways to start" page works only when the window was closed and `doze open <dir>` boots it.
- **Fix:** in `Service.Open`, when the window is live (`emit != nil`) and `noConfig(dir)`, emit `Nav{Page:"empty", Dir:dir}` straight away. For a folder with a doze.hcl the hub does not know, either show the stopped-stack page from the file (`declaredIn` already reads it) or emit a nav to a "found doze.hcl; Up starts it" page. As a stopgap, `chooseFolder` could `go({page:"empty",dir})` itself when the dir has no known key. Add an e2e test that opens a folder after boot.

### B2. Space and Enter act on the selected row even when focus is on a button: stops the wrong stack
- **Screen:** Library, keyboard.
- **What happens (verified with intercepted calls):**
  - Focus **Doctor** and press Space: `{"action":"down"}` is sent for **shop**. The running stack stops, and Doctor never opens.
  - Focus **acme's switch** while shop is the selected row and press Space: `down` is sent for **shop**. The user asked to start acme and stopped shop instead.
  - Focus **Settings** and press Enter: the user ends up on shop's page, not Settings. Focus **acme's ▾** and press Enter: also shop's page, and no menu opens.
- **Why:** library.tsx:20-54 puts a window-level keydown handler on Space, Enter, Backspace, j and k. It only skips INPUT and TEXTAREA, not BUTTON or `[role=switch]`. Space calls `preventDefault`, so the focused button never activates. Enter is not prevented, so the button's own click and `go(row)` both run and the last one wins. Clicking the ▾ or a switch does not select its row either, so the "selected" row and the control being used can be different stacks.
- **Fix:** return early when `e.target` is inside `button, a, [role=switch], [role=menu], select`. Make each row focusable (`tabIndex`, `aria-selected`) and select it on focus, so the selection follows real focus. Clicking a row's switch or ▾ should select that row.

### B3. The Library cannot scroll: rows past the window are unreachable
- **Screen:** Library with more stacks than fit (about 9 at 1280×800, about 4 at 900×620). The empty Library at 900×620 also cuts off its keys bar.
- **Evidence:** `main.main` has `overflow-y: hidden` with scrollHeight 1071 and clientHeight 758. A mouse wheel leaves scrollTop at 0 (`55-after-wheel.png`). Pressing ↓ thirteen times selects a row below the fold, but nothing scrolls it into view (`56-after-arrows.png`). The Machine line, "Also on this machine" and the bottom rows are unreachable (`50-many-top.png`).
- **Why:** shell.css:72 `.main { overflow: hidden }`. The `.main.scroll` rule (shell.css:73) exists, but app.tsx:148 never adds the class. The empty-folder page has the same problem when the Found list is long.
- **Fix:** add `scroll` to `<main>` for library, empty and settings, or give `.lib` its own scroll area with the Machine line pinned. Call `scrollIntoView({block:"nearest"})` on the selected row whenever the selection changes.

### B4. The ▾ menu is clipped by the table: for the last row it is invisible, so Forget, Everything and Choose what starts cannot be reached
- **Screen:** Library, the ▾ beside any switch.
- **Evidence:** on the running row only the "UP / As last time" line shows; the rest is cut off at the table's edge (`04-menu-running.png`). On the last row (acme) nothing at all is visible (`05-menu-stopped.png`), so a stopped stack can be forgotten only with ⌫ (B5). The library e2e test passes because `toContainText` ignores overflow clipping.
- **Why:** shell.css:290 `.lib { overflow: hidden }`, and the `.pop` is absolutely positioned inside the row.
- **Fix:** render the menu in a portal (or `position: fixed` from the button's rect), flip it upward near the bottom edge, and drop `overflow: hidden` from `.lib` (keep the rounded corners with `clip-path` or by rounding the first and last rows). Add a test that checks the menu is visible (`toBeInViewport`).

### B5. Drag and drop is promised in four places but not built
- **Screen:** Library ("Drop a project folder anywhere in this window to add it"), empty Library ("Drop a project folder here", a big dashed drop zone), the empty-folder page's key note ("Open folder, a drop, or doze open"), and the docs (app.md:29, "Drop a project folder on the window").
- **Evidence:** there is no drop handling anywhere. No `EnableFileDrop`/drag-and-drop option in window.go:43-56, and no `onDrop`/`dragover` in the frontend (grep). Dropping a folder does nothing at best. At worst WKWebView navigates to the dropped `file://` URL, which I could not test outside the real window.
- **Effect:** the new-user path. The biggest element on the empty Library is a drop target that does nothing.
- **Fix:** turn on Wails v3 file drop on the window, handle the dropped directory with the same `Open(dir)` path (after B1), and show a drop highlight on the window. Until then, remove the drop wording and the dashed zone.

### B6. "Set it up from this repo" in an empty folder says "it asks instead" but silently writes a postgres doze.hcl and starts a daemon
- **Screen:** the empty-folder page, choice 1, when nothing was recognised (`21-folder-emptyproj-1280.png`). This is also what the `1` key does.
- **Why:** `setupFolder` (cmd/doze/empty.go:19-55) runs `doze init` with no TTY. `init` skips the wizard when `!canPrompt()` and scaffolds `offers[0]`, which `postgresFirst` makes postgres (init.go:56-66, 112). Then `startDaemon`. The page's own banner promises "Nothing runs until you bring the stack up", and the docs say "nothing runs until you choose". After any of the three choices the stack shows up with its switch on.
- **Fix:** when nothing is recognised, choice 1 should open the wizard as a dialog (the three questions are fixed), or be hidden with "nothing recognised; use 2 or 3". Either do not start the daemon after setup, or reword the banner to "writes doze.hcl and brings the stack up; services sleep until something connects".

---

## Major

### M1. The menu-bar bat never shows "something needs you"
- The MenuBar board draws a red dot on the bat when something crashed and a dimmed bat when nothing runs. `tray.Model.NeedsYou` is computed (tray.go:74) but never read: app/tray.go only sets the template icon once (line 37), and nothing anywhere uses `NeedsYou`.
- A crash with the window closed shows nothing in the menu bar. Notifications cover part of this, but only once per episode.
- **Fix:** keep a second template image with the dot, or overlay it, and swap it in `refreshTray` when `m.NeedsYou` is true. Dim the bat when no stack is running.

### M2. The tray and the Library count "awake" differently
- The Library row says "4 awake · 2 waking · 1 dozing · 3 asleep". The tray line and its tooltip say "7 awake". `tray.Build` uses `display.Awake(state)`, which counts active, idle and booting (display.go:16-17). The Library counts `LookOf(v).Word == "awake"` (view/library.go:238).
- This is the same kind of contradiction as the service switches: two faces of the same app disagree about the same stack at the same moment.
- **Fix:** have the tray use the same fig as the Library (`figOf` or a shared helper in display), for example "4 awake · 551 MB", or count with `LookOf(v).Word == "awake"`. Add a test that compares the tray Sub with the Library Fig on the same fixture.

### M3. A stack whose daemon stopped answering shows stale live figures with only a yellow dot
- **Screen:** Library row with `Conn != Live` (made in `50-many-top.png`, row "billing").
- The row still says "4 awake · 2 waking · 1 dozing · 551 MB · 4 connections", with green address dots and the switch on. The only sign is a yellow 6-px dot. The hub keeps the last instances on `ConnDown` (hub/stack.go:113-117), and `libStackOf` adds no words for it (view/library.go:283-284). The tray says "daemon not responding" for the same stack.
- **Fix:** when `!Live`, set Fig to "daemon not answering · retrying" (or "connecting…") and Sub to "figures from 12s ago", dim the address dots, and use the same words as the tray.

### M4. Double-clicking a switch starts the stack twice and also opens it
- Verified: a double-click on acme's switch sent `up-last` twice and navigated to acme. A double-click on ▾ or on an address pill also navigates, and on the pill the URL opened twice ("opened http://api.shop.doze" toasted twice, `40-after-switch-click.png`).
- **Why:** the row's `onDoubleClick` (library.tsx:160) fires for double-clicks on its controls, and the switch has no in-flight guard. In the real app each `up-last` runs `doze -y up --last` as a separate process.
- **Fix:** in the row's dblclick, ignore targets inside `button`. Add a pending state per stack so a second toggle is ignored until the first finishes (see M5).

### M5. The switch gives no feedback between click and state change
- After a click on acme's switch, `aria-checked` stays false and the row still says "stopped" until the hub sees the daemon. Only a toast, "bringing acme up as last time…", says anything. For a slow `doze up` (pulling images, migrations) the row looks as if the click was lost, which invites a second click (M4).
- **Fix:** mark the stack as starting or stopping in Go (`Service.Stack` already knows) and push it, so the row shows "starting…" or "stopping…" with the switch in a mid or disabled state until the command finishes.

### M6. ⌫ forgets a stack instantly, with no confirmation and no undo
- library.tsx:44-46: Backspace on the selected stopped row calls `forget` straight away (verified: the call was sent). Combined with B2, the selected row may not be the one the user is looking at. In WebKit, an unprevented Backspace also navigated the test page back to blank (`35-after-backspace.png`). That is probably harmless in WKWebView, but `preventDefault` is missing.
- **Fix:** add a confirmation ("Forget acme? Its files stay where they are") or an Undo toast, and call `preventDefault`. Because of B4, ⌫ is currently the only way to forget the last row.

### M7. The ▾ menu cannot be used from the keyboard
- Opening it does not move focus into it. ↑/↓ move the Library's row selection, not the menu items (verified: ArrowDown plus Space with acme's menu open sent `up-last` from the row handler, and focus stayed on `body`). There is no roving focus and no focus return on Esc. The menu shows `c` for Choose what starts…, but the Library has no `c` key (only overview.tsx:62 does). The docs say "Choose what starts… (`c`)".
- **Fix:** focus the first item on open, let ↑/↓ move between items, Enter activate and Esc return focus to ▾. Either add `c` to the Library handler for the selected row or drop the hint from the menu.

### M8. The window is too narrow for its own table at the minimum size (640): switches disappear; at 900, "+N more" and "crashing" are cut off
- At 640×400 (MinWidth 640) the switch and ▾ columns fall outside `.lib`'s clipped box, so the Library cannot start or stop anything (`03c-lib-dark-640.png`). The grid minimums (shell.css:291) add up to about 760 px.
- At 900×620 the address pills wrap into `max-height: 50px; overflow: hidden` (shell.css:305). The third pill and the "+2" are hidden, so nothing tells the user there are more (`03-lib-dark-900.png`).
- In the crash fixture at 900, the fig line truncates to "3 aslee…". "1 crashing" is last in `figOf`'s order (view/library.go:305-310), so the most important word is the first to be lost (`26-lib-crash-900.png`).
- **Fix:** add a narrow layout below about 1000 px (stack the Running figures under the name, drop the Last column, keep switch and ▾ always visible). Put crashing first in `figOf`. Always render "+N" (count the hidden pills, or cap at what fits).

### M9. The "Doctor" button opens Settings → General, not the Machine part
- **Screen:** Library Machine line, "Doctor" (`43-doctor-click.png`). The tooltip says "What doze set up here, part by part", but it lands on Mode and Colour theme. The Machine part ("1 to do") is one more click. The tray's Settings… lands on the same page.
- **Fix:** go to `{page:"settings", part:"machine"}`. Settings already has the part state.

### M10. The Library's "https://…" pill is green and clickable while the Machine line says that domain is "not set up yet"
- In the shop fixture the first pill is `https://dev.shop.example` with a green dot and link styling. Below it, the Machine line says "domains · dev.shop.example · not set up yet" (`01-lib-dark-1280.png`). Clicking it opens a browser at an address that will not resolve or will fail TLS.
- **Fix:** when the domain's setup is not in place, show the pill dimmed with a warning dot, and make its click go to Settings → Machine (or show "set up domains first"). Go knows both facts and should merge them in `addressesOf`.

### M11. A row "run by an older doze" has a working switch that runs the CLI in the wrong folder
- When `Dir == ""` the row shows "run by an older doze" with the switch on (`50-many-top.png`, row "legacy"). It offers no guidance and nothing to do. `Service.Stack` sets `cmd.Dir = st.Dir` (empty), so `doze -y down` runs in the app's working directory. Launched as `doze` from a project, the app inherits that project's folder, so turning off the old stack would stop a different one. This is from code (serve/library.go:270-271) and not run.
- **Fix:** for rows without a dir, disable the switch and ▾ and say what to do ("restart it with this doze: `doze down` then `doze up` in its folder"), or stop it over its socket.

### M12. On the empty-folder page, "1–3 pick" runs the choice instantly, with no busy state, so it can run twice
- Pressing `3` sent `setup empty` straight away. A second `3` sent it again while the first was still in flight, and `2` opened the Add dialog on top (verified with a delayed `/api/setup`). The keys bar says "1-3 pick" and "⏎ choose", which suggests picking selects and Enter commits (`60-empty-pressed-3.png`).
- **Fix:** make the digits select only and Enter choose, or relabel them "1-3 choose". Disable the tiles and show "writing doze.hcl…" on the chosen tile until Setup returns.

### M13. Screen readers get no row selection and status by colour only
- Rows are `role="row"` but not focusable and have no `aria-selected`, so Space, Enter and ⌫ act on a selection a screen reader cannot perceive. Cells have no `role="cell"`, and the whole row is read as one long string (aria snapshot).
- Status dots are `aria-hidden`: the row dot, the tab dot (trouble or live), and the Machine line's green or yellow per part. "network resolver route" is announced with no ok or not-ok state.
- The switch label carries state ("acme is down" plus "off"), so VoiceOver says "acme is down, switch, off" without saying what toggling does.
- **Fix:** label the switch `Run ${name}` and let `aria-checked` carry the state. Add visually hidden text for each dot ("ok" / "not set up"). Use the grid pattern with focusable rows.

---

## Minor

1. **⌘1–9 numbers do not match the tabs.** app.tsx:60-65 indexes `stacks` (every stack, running first), while each tab's tooltip numbers the visible tabs (titlebar.tsx:49). ⌘2 opened the stopped acme, which had no tab (`32-cmd2.png`). The docs say "⌘1–9 a stack's tab". Fix: index `open` (the visible tabs) in the key handler.
2. **⌘N is shown but not wired.** "New stack… ⌘N" on the empty Library (library.tsx:146) has no handler, and the button only opens the same folder picker as Open folder. Fix: bind ⌘N to a picker that allows creating a folder and then goes straight to the empty page's choice 3, or drop the shortcut.
3. **Three different names for a stack with no `stack =`.** The Library and tabs use the folder name (view/library.go:213-217, service.go:307). The tray uses "a stack" (tray.go:50-51). The tab falls back to "stack" (titlebar.tsx:52). Fix: one helper in display.
4. **The tray and the Library order stopped stacks differently.** The tray sorts by most recent first (tray.go:100-106), the Library by name (hub.go:263-268). A user scanning both sees different orders. Pick one; recency is better for both.
5. **The Last column mixes two meanings.** "up 24s" means it has been up for 24 s, while "up 3d ago" means it was last up 3 days ago, in the same column (view/library.go:269, 279). Fix: "running 24s" vs "last up 3d ago", or a header like "Up since / last up".
6. **The "Running" column header sits over "stopped".** For stopped rows the cell under RUNNING says "stopped". Fix: rename it "State" or "Now".
7. **Vocabulary drift for a stack that is not running.** The Library fig says "stopped", the stack page heading says "Down", the switch label says "is down", the menu says "Down — stops everything", and Help's "The words" defines neither. Fix: pick one word for the stack state ("stopped") and keep "Down" for the verb; add it to The words.
8. **"Set up (one password)" in the Library is "Repair (one password)" in Settings,** with "Setting up…" vs "Repairing…" for the same action (library.tsx:321, settings.tsx:143), and the docs say Repair. Use one name.
9. **Machine line wording is jargon.** "network · resolver route" vs the board's "names · network daemon" and the docs' "the names, the network daemon". A first-timer cannot tell what "resolver route" is. Use the docs' words, with the detail in the tooltip.
10. **The keys bar shows row keys on the empty Library.** "↑↓ move · space up / down · ⏎ open · ⌫ forget" appear with no rows (`20-emptylib-dark-1280.png`). Show only ⌘O and ⌘, when there are no stacks.
11. **The crash pill on a row is not clickable.** "● api is crashing · try 3 of 5" is a `span` (library.tsx:186-190); the one thing that needs the user does not take them there. Make it a button to the instance page. Likewise "+2" is a dead span with no way to see the rest.
12. **The pending folder tab cannot be clicked back to.** Once the user leaves the empty-folder page for the Library, its tab vanishes, and the only way back is ⌘O again (which is broken, B1). Keep the pending tab until it is closed with ⌘W/esc, or list "opened, no doze.hcl" folders in the Library.
13. **The tray's "Open doze ⌘⇧D" looks like a global shortcut,** but it is only a menu key equivalent (app/tray.go:109). Nothing registers a global hotkey, so ⌘⇧D does nothing from another app. This is from code; I could not test it. Register it globally or drop the accelerator.
14. **Settings highlights the Library tab** (`library={onLibrary || where.page==="settings"}`, app.tsx:140), so the selected tab says "Library" while the page is Settings (`43-doctor-click.png`). Either deselect all tabs on Settings or give the head a back affordance.

## Polish

1. **⌘O renders as ⌘0.** In the kbd font, "⌘O" (open folder) and "⌘0" (Library) look identical: head button, keys bar and Help, side by side in Help (`10-zoom-head.png`, `11-zoom-keys.png`, `41-help.png`). Use a font with a distinct O/0 for kbd, or write "⌘ O".
2. **Toasts cover the Machine line's Doctor / Set up buttons and the keys bar** at bottom right (`40-after-switch-click.png`). Raise them above the Machine line, or move them to the top right.
3. **The empty-folder page shows the raw path, not ~** (`e.dir`, empty.tsx:65), while the Library rows use `~` and `EmptyOf.Title` already has the tilde'd one. At 900 the h1 truncates to "nodep…" while the path takes the room (`22-folder-nodeproj-900.png`). Show the tilde path and let it truncate first.
4. **At ≤1000 px, `.lib` gets 16 px of inner padding** (shell.css:407), so the header band and selected row stop short of the table's edges, and the Found table and tiles do not line up (`03-lib-dark-900.png`, `22-folder-nodeproj-900.png`). Drop `.lib` from that padding rule.
5. **The "Or start empty" example is not the file choice 3 writes.** It shows `stack = "shop"` / app web / postgres, while choice 3 writes the commented `StarterFile`. Show the real starter, or say "for example".
6. **The window background is hard-coded dark** (`BackgroundColour 0x14161a`, window.go:51), so light-mode users get a dark flash before the page paints. Choose it from the saved mode.

---

## Works well
- The visual design is calm and consistent in both modes. Light mode keeps contrast on the row figures, pills and switches (`02-lib-light-1280.png`).
- The focus ring is clear on tabs, buttons, pills, switches and ▾ when keyboard navigation is on (`30-altfocus-9.png`). Tab order is sensible: tabs → ⌘K → head → pills → switch → ▾ → Doctor. On macOS, WKWebView skips buttons on Tab unless the system "Keyboard navigation" setting is on; that is worth a line in Help.
- Long names and paths truncate well: the path is cut from the left so the project folder stays visible, and long domains ellipsize inside their pills (`25-lib-long-1280.png`).
- The empty-folder page (when it is reached) explains what was found and what each choice writes, and esc returns to the Library with the pending tab gone and nothing written.
- The help strip's logic is honest: a step stays done once done, the strip hides once every step is done, and Got it dismisses it.
- Failures of `doze up/down` from a switch come back as a red toast with the CLI's last line (serve/library.go:280-289), and Forget says "everything is still on disk".
- The `?` Help overlay does block the Library's Space handler (verified).
