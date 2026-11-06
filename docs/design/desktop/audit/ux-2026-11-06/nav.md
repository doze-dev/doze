# doze desktop: when something is wrong, settings, getting around (UX audit)

Scope: broken doze.hcl (running and stopped), the "doze.hcl changed" banner, the domain setup banner, daemon-down/stale banners, toasts, Settings (General, Machine, About), ⌘K, the ? sheet, and navigation keys.
Method: `doze open --serve` with the fixtures badconfig, shop, portclash, crash, modules and long, driven by Playwright WebKit at 1280×800, 900×620 and 640×400 (the minimum window), in dark and light. I also read the code (app.tsx, store.ts, palette/help/toasts/titlebar, overview ConfigBanner, settings.tsx, view/palette.go, view/configerror.go, serve/{service,library,repair}.go, app/{app,window,tray}.go and the vendored Wails fork's default menu), and compared against the boards.
Screenshots: test/desk-e2e/.desk-look/ux-2026-11-06/nav/ (SHOTS below means that folder).
To test a broken stopped stack and a fixed file, I edited the temporary fixture copies of doze.hcl under /var/folders/…/doze-fixture-*/work. I did not touch the repo.

Counts: **2 blocker · 18 major · 15 minor · 3 polish**

---

## Blockers

### B1. Help › Learn More in the macOS menu bar replaces the doze window with wails.io, and there is no way back
- **Screen:** the native app menu bar, which appears whenever the window is open (activate(true) makes doze a regular app).
- **What happens:** doze never sets an application menu (app/app.go has no `Menu`; tray.go only builds the bat's menu). The Wails fork therefore installs `DefaultApplicationMenu()` (~/go/pkg/mod/github.com/doze-dev/wails/v3@v3.1.0-doze.1/pkg/application/application_darwin_purego.go:452, menu_darwin_shared.go:13). Its Help menu has one item, **Learn More**, which calls `Window.Current().SetURL("https://wails.io")` (roles.go:157). The window has no browser controls, and the page's ⌘[ listener is gone once the page is replaced, so the only way out is to close the window. The rest of that menu is also unbranded: View › Reload / Force Reload (and Toggle Developer Tools in non-production builds), Actual Size on ⌘0, File › Close Window on ⌘W, and the Wails About panel. The binary contains the string "wails.io" (`strings dist/…/doze | grep -c wails.io` → 5).
- **Why it matters:** this is the first menu a lost user opens. It shows another product's site, kills the window's state, and offers no route back.
- **Fix:** set an application menu yourself with `app.Menu.SetApplicationMenu`. Keep App (About doze, Settings… ⌘,, Hide, Quit) and Edit as Wails has them. Add a **Go** menu (Library ⌘0, Back ⌘[, Forward ⌘], Up esc, All logs ⌘L, Open Folder… ⌘O) that emits the same `nav` events the page uses, a Window menu, and Help (Keys ?, doze.dev docs opened in the browser, never `SetURL`). Drop Reload, or keep it only in dev builds. This also gives every key a menu item that VoiceOver users can discover.

### B2. Under 1000 px wide there is no way to reach Settings › Machine or About, or anything in a service's sidebar
- **Screen:** Settings, and the instance and logs pages, in any window narrower than 1000 px. The first-run default when no screen is known is 960 wide (window.go:47); the minimum is 640; a half-screen window on a 1440 laptop is 720.
- **What happens:** shell.css:403–404 `@media (max-width:1000px){ .side{display:none} }` hides every `.side`. The comment says "the sidebar is a rail", but no rail is drawn. In Settings that removes the General/Machine/About list, so Machine, Repair, Copy fix, Copy report and About cannot be reached by mouse or by key. The Library's "Doctor" button lands on General (see M17), so the user is stuck there. On an instance page it removes "‹ Overview" and the service list. The "gone service" page still says "The sidebar lists what it does" even though the sidebar is hidden.
- **Evidence:** SHOTS/shop-settings-general-900-dark.png (no section list), SHOTS/min-settings.png, SHOTS/instance-900.png (no way up on screen except the tab), SHOTS/reopen-gone-instance.png.
- **Fix:** below 1000 px, turn Settings' sections into a segmented control or tab row under the heading, and turn the service sidebar into a real icon rail or a "‹ shop" breadcrumb in the page head. Also make the settings section part of `Where` (see M17) so ⌘K and links can open Machine directly.

---

## Major

### M1. A broken doze.hcl on a running stack shows only on that stack's Overview page; the Library, the tab and every other page say all is well
- **Screen:** badconfig fixture. Library, tab strip, a service page, logs, timeline.
- **What the user sees:** the Library row for shop is green ("4 awake…") and the tab has a green dot with no badge. Inside a service or on the logs page there is no banner. Only Overview shows the red ConfigBanner.
- **Why it's wrong:** someone who saves the file while looking at api's logs never learns that the save was rejected. The ConfigError board draws the tab as "shop 1", with a badge.
- **Evidence:** SHOTS/bad-library.png, SHOTS/bad-tab1.png. The Go side sets `home.Config` only (serve/service.go:346–350). Instance, Library and StackSummary have no config field.
- **Fix:** add `ConfigBroken` to StackSummary and the Library row so the tab gets a badge and the row a red sub-line ("doze.hcl does not load · running the last good file"). On instance, logs and timeline, show a one-line banner with "Show the error" (go to Overview) and ⌘E.

### M2. A stopped stack with a broken doze.hcl offers to start it everywhere while one line says "nothing can start"
- **Screen:** acme (stopped) after a bad line was appended to its doze.hcl, in the shop fixture.
- **What the user sees:**
  - Overview: "doze.hcl does not load · nothing can start until it does", then a raw, truncated one-liner: "invalid config — Error: Unsuitable value type on /var/folders/…/doze.hcl line 34, in app "broken": 34: port …". There is no excerpt and no highlighted line, unlike the running case.
  - The Up switch and ▾ are enabled.
  - The keys bar says "space start with this one · ⏎ up as last time · c choose what starts".
  - ⌘K "acme" offers "Up acme · as last time: api, web, db".
  - The Library says "acme · 0 declared" and "last time: api, web, db", with an enabled switch.
- **Why it's wrong:** this is the kind of self-contradiction the owner has flagged. The page says nothing can start, and every control around it offers to start. The same error is also presented two different ways depending on whether the stack runs.
- **Evidence:** SHOTS/brokenstopped-home.png, SHOTS/brokenstopped-library.png, SHOTS/brokenstopped-pal.png.
- **Fix:** let `StoppedHome` reuse `ConfigErrorOf`, so it gets the same ConfigBanner card with excerpt and line. While the file does not load, disable Up, the ▾ menu, the start keys and the palette's "Up acme" row, each with the reason. In the Library show "doze.hcl does not load" instead of "0 declared".

### M3. With doze.hcl broken, "the stack keeps running as it was" is true only until the user touches the Up switch
- **Screen:** running stack with the ConfigBanner.
- **What happens:** the Up switch, ▾ (up-all, choose), and palette "Down shop" all stay live. Down works. Bringing it back up runs `doze up --last` on the broken file and fails (serve/library.go:249–289). The only feedback is a toast "up-last shop failed: <last line>", which is truncated (M11). The stack is now down, and the page's own promise is false.
- **Fix:** while `home.config` is set, confirm before Down ("doze.hcl does not load: shop cannot come back up until it does. Stop anyway?") and disable up/restart with that reason.

### M4. The ConfigBanner implies the fix is applied automatically; after fixing, a different banner says the opposite
- **Screen:** ConfigBanner, then the `changed` banner.
- **What the user reads:** first "Nothing is applied until the file loads. Fix it in your editor; this page reads it again on save." Once the file loads, the code shows "doze.hcl changed 2m ago, after the stack read it. The stack runs the file as it was when it came up… Restart the stack" (overview.tsx:201–213, view/configerror.go:107–115). The board's copy promised "doze reads it again on save and applies the difference", which the daemon does not do.
- **Note:** I could not render the changed banner. The fixture's `Loaded` is zero, so `ConfigChangedOf` returns nil: after I fixed the badconfig file, the error banner disappeared and nothing replaced it. Add a fixture that sets `Loaded`.
- **Fix:** say up front what will happen: "Fix it and save; then Restart the stack to apply it (data stays)." Show the Restart button disabled, with "waiting for doze.hcl to load", inside the error banner, so it is the same control before and after the fix.

### M5. The config error message does not say what to write
- **Screen:** ConfigBanner card.
- **What the user sees:** "Unsuitable value type · Unsuitable value: a number is required". The word "Unsuitable" appears twice, and neither the attribute nor the fix is named.
- **Board:** "port wants a number, got the string "8081". Write port = 8081."
- **Evidence:** SHOTS/bad-tab1.png. view/configerror.go:52–70 passes the HCL summary and detail through unchanged.
- **Fix:** humanise the common HCL diagnostics (type mismatch, unknown attribute, missing brace, duplicate block) into "‹attr› wants ‹type›, got ‹value›. Write ‹suggestion›." Drop the Summary when it repeats the Detail.

### M6. ⌘1–9 does not match the tabs
- **Code:** app.tsx:62–66 uses `stacks[n-1]`, the whole list including stopped stacks. The tab strip shows only running stacks plus the current one (titlebar.tsx:27), and each tab's tooltip numbers that filtered list (`⌘${i+1}`, titlebar.tsx:52). The hub sorts running first, then by name (hub/hub.go:263), so numbers also move whenever any stack starts or stops.
- **Seen:** ⌘2 opened acme, a stopped stack that is not a tab. A tab for it then appears (SHOTS/nav-cmd2-stopped.png).
- **With two or more stopped stacks:** standing on the second stopped one, its tab tooltip reads ⌘2 but ⌘2 opens a different stack.
- **Docs and help:** both say "⌘1–9 a stack's tab".
- **Fix:** index the same `open` list the title bar draws, from one shared function. Consider keeping tab order stable (order opened) rather than re-sorting on start/stop.

### M7. "esc", "back", "⌘W" and "⌘[" mean different things on different screens, and Settings cannot return you to where you came from
- **Keys bars:**
  - Settings: "esc back".
  - Instance: "esc back".
  - Overview: "esc library".
  - Help sheet: "esc up a level…"
  - ⌘[ is "back · forward, the way you came".
- **Actual behaviour:**
  - esc always goes up (store.ts:477 `upFrom`). From Settings it always goes to the Library, even when you pressed ⌘, from inside a service: shop › api › ⌘, › esc lands on Library (audit run "esc from settings").
  - ⌘W runs the same `up()` on every page (app.tsx:85), but the help sheet says ⌘W only "close[s] a folder with no doze.hcl; a stack keeps running". On the Library, esc and ⌘W do nothing at all (they preventDefault, then upFrom returns null), so the keyboard cannot close the window.
- **Native (from code, needs a check in the real app):** WKWebView gives key equivalents to the page first. The page's listener skips ⌘W when the palette, help or a dialog is open, or when an input has focus. In those states ⌘W falls through to the default File › Close Window (B1) and closes the window: same key, different result depending on where focus is.
- **Fix:**
  - Treat Settings as a place you return from: esc there should go back (history), or esc everywhere should go to `history.at(-1)` when the last move was ⌘, or ⌘K.
  - Use one word per action in every keys bar: "esc up" vs "⌘[ back".
  - Give ⌘W one meaning: close the window, the macOS convention, or "close tab" on a stack tab and close the window on the Library. Document it identically in Help and app.md.

### M8. ⌘K: a verb plus a stack name fails, and the error message contradicts itself
- **Queries:** "up shop", "down acme", "restart shop".
- **Result:** "Nothing called shop in shop or acme. Closest: seed (process, in shop). ⏎ open seed". The footer advertises "verbs: up · down · sleep · wake · restart…".
- **Evidence:** SHOTS/shop-pal-restart_shop-1280-dark.png and -900-dark.png. audit run f: "down acme → closest cache [⏎ open cache]".
- **Fix:** in view/palette.go's verb path, match stack names for the stack verbs (up, down, restart, sleep all, wake all). When nothing matches, search stacks and verbs as well as instances, and never say "Nothing called X" when X is a listed stack.

### M9. ⌘K: typing a stopped stack's name and pressing Enter starts it, and there is no way to open its page from ⌘K
- **What happens:** `byName` adds only `upRow(st)` for a stopped stack (palette.go:183–186). The section is titled "Open" but the row is "↑ Up acme", and ⏎ brings acme up. The footer says "a name alone opens it with its actions".
- **Evidence:** SHOTS/shop-pal-acme-1280-dark.png.
- **Fix:** for stopped stacks make "acme · stopped" (⏎ opens its page) the first row, with Up as an action (tab or ⌘⏎).

### M10. ⌘K: a verb with a misspelled name runs the verb on a guess
- **What happens:** "restart apu" → "Nothing called apu… ⏎ restart api". "sleep dbb" → "⏎ sleep db". Enter on the nothing row runs `p.nothing.do` (palette.tsx:63–65; palette.go:514–520). It restarts a service the user did not name, without confirming.
- **Fix:** on a guessed target, Enter should select the suggestion (fill the input with "restart api") rather than act. A second Enter then acts.

### M11. Every copy says "copied  to the clipboard", with no word for what was copied and a double space
- **Code:** store.ts:296 `await say(\`copied  to the clipboard\`)`. The `what` argument is accepted and dropped.
- **Affects:** Copy fix, Copy report (c), "Copy doze trust", palette copy, and every address copy. All produce the same toast, which also lands in recent messages.
- **Evidence:** audit run c: toast after Copy fix = "copied  to the clipboard".
- **Fix:** `say(\`copied ${what}\`)` → "copied the command", "copied the report".

### M12. Failure toasts are cut to one line, so the "what to do" part is lost, and the place that would keep them can't be reached
- **Toasts:** shell.css:354 sets `.toast .t b { white-space:nowrap; text-overflow:ellipsis }` with `max-width:420px`. Clicking "Set it up" without the admin prompt available showed "this doze cannot ask for an administrator's passwo…". The cut-off half was "copy the fix into a terminal" (SHOTS/setitup-browser.png). The same applies to "up-last shop failed: <last line of the CLI>".
- **Recent messages:** the page (pages/recent.tsx) says "from ⌘K › recent messages", but no palette row produces it (view/palette.go has no such row) and nothing navigates to page "recent". It is unreachable.
- **Fix:** wrap failure toasts up to 3 lines and keep them until dismissed. Add a "Recent messages" row to ⌘K (and the Go menu) and a "more…" link on a failure toast that opens it.

### M13. The daemon-down and stale banner is one terse line with no action, and the switches under it stay live
- **Status:** read from code only. No fixture puts a stack in ConnDown or stale, so I could not render it.
- **Built:** app.tsx:146–153 draws `banner.text` from display/banner.go:23–38: "daemon not responding · retrying in 4s" or "last update 38s ago". It has no explanation, no **Retry now** and no **Show the daemon's log**. It appears only on home and instance (app.tsx:93), not on logs, timeline, recent or the Library row. Overview does not fade or disable anything: switches still show on/off and space/w/s/p/r are live. Instance fades only its head (instance.tsx:84). Actions then fail with "sleep db failed: the daemon is not responding: nothing was sent".
- **Board (DaemonDown):** explains that the services are probably still running, greys the numbers with "38 s ago", disables switches with "Switches and actions are off until the daemon answers; the addresses still work", and has Retry now and Show the daemon's log.
- **Why it matters:** this is the same "switch says on, reality unknown" contradiction the owner already hit.
- **Fix:** add a `daemondown` fixture. When `!banner.ok`, render the board's banner with Retry and Log actions, add `.stale` to the page, disable switch and verb keys with a reason, and show the banner on every page of that stack.

### M14. In Settings, the tab strip says you are in the Library
- **Code:** app.tsx:117 `library={onLibrary || where?.page === "settings"}`, so the Library tab is drawn selected (aria-selected=true) while Settings is shown, and the page's h1 is "General".
- **Board:** has a separate "Settings" tab.
- **Evidence:** SHOTS/nav-settings-from-stack.png, SHOTS/shop-settings-machine-1280-dark.png. The audit log shows tabs ["[Library","shop"] on settings.
- **Fix:** draw a transient "Settings" tab, selected, as the boards do, or select no tab and title the page "Settings › General".

### M15. The ? sheet does not fit short windows and cannot scroll
- **Measured:** at the 640×400 minimum the `.hp` box spans top −228 to bottom 628 with `overflow:hidden`. The "Anywhere" heading and ⌘K/⌘0 rows are off-screen and unreachable.
- **Evidence:** SHOTS/min-help.png.
- **Fix:** `max-height: calc(100vh - 32px); overflow:auto`, and stack the three columns below about 760 px.

### M16. Screen readers: page changes are silent, overlays don't manage focus, and ⌘K's selection isn't announced
- **Page changes:** after every navigation (⌘1, esc, ⌘[, ⌘,, Enter into a service) `document.activeElement` stays BODY (audit run b, every row). Nothing is announced and the heading does not get focus.
- **Help:** a `role=dialog` without `aria-modal`. Focus stays on BODY and Tab does nothing (it captures all keys, help.tsx:7–13). There is no close button, only "esc close" text.
- **Palette:** rows are `role=option` buttons inside `role=group`, with no listbox. The input has no `role=combobox`, `aria-controls` or `aria-activedescendant`, so ↑↓ moves a highlight VoiceOver cannot hear. Focus is not restored after closing.
- **Doctor:** `role="table"` with no row or cell roles.
- **Toasts:** failures are not `role=alert`.
- **Fix:** move focus to the page h1 (tabIndex −1) on each `go`, and set `document.title` to "shop › api — doze". Give Help `aria-modal`, initial focus on a Close button, and a focus trap. Use the ARIA combobox and listbox pattern for ⌘K, with `aria-activedescendant` on the input. Give the Doctor rows proper row and cell roles (or a real table). Use `role=alert` for bad toasts.

### M17. "Doctor", the menu bar's Settings… and ⌘, all open General, and the Machine section is forgotten on back and forward
- **Code:** library.tsx:324 `go({page:"settings"})`. SettingsPage takes a `section` prop that app.tsx never passes, and `part` is local state.
- **What happens:** the Library's Machine line shows "domains · not set up yet" and a **Doctor** button. Clicking it shows Mode and Colour theme. The audit run "fwd to settings (machine kept?)" shows h1 General, not Machine.
- **Words:** one concept has five names. "Machine" (line and section), "Doctor" (button), "Set up (one password)" (Library), "Repair (one password)" (Settings), "Set it up (one password)" (domain banner). Also "Setting up…" vs "Repairing…", and "one password" vs "one admin prompt" vs "one sudo" in the same banners.
- **Fix:** add `section` to `Where` and to the nav event. Make the Library button "Machine ›", landing on Machine. Pick one verb, "Set up", and one cost phrase, "one admin prompt", everywhere.

### M18. The domain banner's "Set it up" does more than it says, and appears even where it cannot work
- **Code:** overview.tsx:220 calls `repair()`, which runs every failing part: dns-setup, `trust` in every stack with domains, and the /usr/local/bin symlink (serve/repair.go:56–80). The tooltip says "The same as doze trust", and the Copy button copies only `doze trust`.
- **Where it can't work:** Settings hides Repair when `!repairable` (settings.tsx:152). The banner always shows the button, and in that case it fails with a toast (SHOTS/setitup-browser.png).
- **Board:** had "Show the script" and explained what the prompt does. The built banner says only "missing: route dev.shop.example · one admin prompt".
- **Fix:** scope this button to this stack's trust step, hide it (leaving Copy) when not repairable, and add the board's one-sentence explanation plus "The stack works at web.shop.doze meanwhile".

---

## Minor

1. **⌘O and ⌘0 look identical** in the kbd/mono font. The Library button "Open folder… ⌘O" reads as ⌘0 (the Library key), and the ? sheet shows two rows that both read "⌘0". (SHOTS/bad-library.png, SHOTS/shop-help-1280-dark.png.) Fix: use a kbd font with a slashed or dotted zero, or write "⌘ O".
2. **The ? sheet and app.md are incomplete.** Help lacks ⌘E (open doze.hcl, on the ConfigError board's Help), T (Timeline) and c (choose what starts, copy report). app.md lacks ⌘W and ⌘E. Help is hard-coded in help.tsx instead of coming from Go like other facts (D16), so it drifts. Generate both from one Go table.
3. **Several keys silently do nothing.** ⌘L does nothing on the Library or Settings, ⌘3 does nothing with two stacks, and ⌘K, ⌘0 and ⌘, do nothing while the ? sheet is open (help.tsx swallows every key). Either act ("⌘L: pick a stack first" toast, or open the last stack's logs) or let navigation keys close the sheet and proceed.
4. **⌘K cannot reach the app's own places.** "settings", "Library", "doctor", "theme", "help" and "recent" all return "Nothing called settings in shop or acme". "help" suggests "Closest: seed ⏎ open seed". Add rows for Library, Settings, Machine, Keys (?) and Recent messages.
5. **⌘K's no-match logic and headings are odd.**
   - The closest-match rule accepts nearly anything: "zzz" suggests api, because edit distance 3 ≤ max(3, …) (palette.go:507).
   - Verb typos suggest services: "slep" suggests seed. It should suggest "sleep".
   - "Did you mean ↓ Sleep everything in shop" appears even when the query matched exactly (SHOTS/shop-pal-sleep_db-1280-dark.png). Rename that section "Also".
6. **⌘K's footer hint is clipped** at 1280 and 640: "…a verb then a name: s" (SHOTS/shop-pal-empty-1280-dark.png, SHOTS/min-palette.png).
7. **"(one password)" inside the primary button is nearly invisible**, dim text on accent, especially in light mode (SHOTS/bad-home-1280-light.png) and also in dark. Use the button's own foreground at reduced opacity, or move it outside the button.
8. **Settings › General says it saves to prefs.json, but one switch doesn't.** The subtitle says "saved as you change them · ~/.doze/desk/prefs.json", but "Reopen the page I had" is stored only in the WebView's localStorage (store.ts:313–329), and the last page too. Clearing WebKit data or a reinstall loses them, and the file the UI names doesn't contain them. Save via Go like Mode and Theme.
9. **The Startup subtitle is opaque:** "the bat is light; the window is not kept". Say "Starts in the menu bar only; the window opens when you click the bat."
10. **The Machine section can't be refreshed.** There is no "Check again" or "checked N s ago" as the Doctor board has. After running the copied fix in a terminal, the user can't tell whether the page updated.
11. **Keyboard reach is limited.** WebKit's default Tab skips buttons, so Repair, Copy fix, Mode, Theme, Restart the stack and Set it up have no key unless macOS Keyboard Navigation is on. With Option+Tab, the focus ring on a theme swatch is identical to the selected ring (SHOTS/settings-alttab.png). The tabs are `role=tab` without arrow keys, and the Mode radios lack arrow keys. Add arrow-key handling, a distinct focus style, and keys for Repair (⏎ on Machine, per the board).
12. **Some stack-action errors are never shown.** stackAction only logs synchronous errors (store.ts:178–185): "choose at least one", "no such stack", "stop it first…". Route them to `say(…, true)`.
13. **ConfigBanner details.**
    - The banner line always says "doze.hcl changed…" (ConfigErrorText) even when the error is in a sibling *.doze.hcl, while the card heading names the right file.
    - ⌘E opens `file://…` with no line number, and an .hcl file with no app association may do nothing.
    - The board's "Open in editor" became "Edit doze.hcl".
    - The keys bar doesn't list ⌘E.
14. **Navigation errors are raw.** A failed `go` shows a raw red banner, "Error: …" (`String(e)`, store.ts:162 and app.tsx:154), with no action and no dismiss, until the next navigation.
15. **Native reload and quit.** ⌘R from the default View menu reloads the page and wipes back/forward history, which lives only in memory. ⌘Q quits the bat too, while the bat's own Quit is labelled "stacks keep running". Fold both into the B1 menu work.

## Polish

1. **The version shows as a branch name.** About, Help and the sidebar show "archive/dash" as the version in this build. Make sure release builds show semver, and print the commit only in parentheses.
2. **"Up 3d ago" reads like an uptime.** The Library LAST column says "up 3d ago" for a stopped stack. Use "last up 3d ago" (as the stopped Overview already does).
3. **The tab and Library row tooltips show the full /var/folders path** in the badconfig fixture (dir mismatch between projects/badconfig-1 and work/shop). Harmless, but it makes screenshots noisy.

---

## Works well
- The running ConfigBanner is good. It says plainly "the stack keeps running as it was", shows a source excerpt with the bad line highlighted and underlined, names the file and line, and notes "same message as doze lint" (SHOTS/bad-tab1.png).
- The esc ladder (service or logs → overview → Library) is the same on every page. ⌘[ ⌘] history works across stacks, services, logs and Settings, and a plain move clears forward history correctly.
- ⌘K owns the keyboard while open (⌘1, ⌘[ and ⌘W can't navigate underneath it). esc clears the query, then closes. Rows carry their stack and a key hint, and the empty state (Recent / Do) is useful.
- Opening a page for a service that no longer exists degrades gracefully: "Nothing called gone-service. shop does not declare it any more."
- Light and dark both render cleanly. Theme swatches preview each theme in the mode on screen, and Mode reports "following macOS: dark now".
- Failure toasts stay 12 s and can be dismissed. Settings hides Repair when no admin prompt is available and offers the terminal command instead.
- The window reopens on the last page, falling back to the Library if that stack is gone.
