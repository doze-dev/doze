# doze desktop: journeys and mental-model audit

Auditor: journeys and mental model. Date: 2026-11-06. Build: dist/Doze.app (sidebar reports "archive/dash").
Method: walked the five journeys against fixtures shop/shop-copy (48160/48167), crash (48161), modules, badconfig (48163), portclash (48164), an empty folder (48166). WebKit 1280×800. Steps the fixtures cannot fake were read in code.
Screenshots and text dumps: test/desk-e2e/.desk-look/ux-2026-11-06/journeys/ (file names below are relative to that folder).

Counts: **3 blocker · 12 major · 15 minor · 3 polish**

---

## Blockers

### B1. The row switch has three meanings, and it moves by itself
- **Journey:** 2 (daily loop: "put a heavy module to sleep", "work on one app only").
- **What happens:**
  - On a running stack, the switch shows *awake right now*: `row.On = !Disabled && word != "asleep"` (internal/desk/view/home.go:227). Flipping it calls wake or sleep (overview.tsx:516-527; instance.tsx:113-114).
  - doze sleeps idle services on its own. So a switch you turned on goes off by itself five minutes later. A module you switch off comes back on as soon as anything connects. The sleep dialog says so itself: "The next connection wakes it again, so it is back within seconds" (daily-01-sleep-db-dialog.png).
  - The other places that define the switch disagree:
    - CLI `doze off`: "It is the row's switch in the app" (cmd/doze/verbs.go:183-185).
    - D18: "The switch is 'part of this run'" (docs/design/desktop/d18-library-and-switch.md).
    - The toggle's own code comment: "part of this run, or not" (overview.tsx:516).
    - The app's help: "off — not part of this run; nothing wakes it until you switch it on" (help.tsx:95).
    - The docs: "on wakes the service, off puts it to sleep" (website/.../cli/app.md:37).
    - Three of these say "part of this run" and one says wake/sleep. The code does wake/sleep.
- **Why it breaks trust:** A switch is a promise that a state stays put until you change it. Docker Desktop and OrbStack switches mean "this container runs". Here the switch is a live readout dressed as a control. The developer who switches kafka off to save memory sees it come back on by itself, and the app has no switch that keeps it off. This is the same class of contradiction as the "switch on while the stack is asleep" bug.
- **Recommendation:**
  - Make the switch "part of this run" (`doze on` / `doze off`, which already exist). The switch only moves when a person moves it.
  - Show awake / dozing / asleep with the dot and the word, never the switch.
  - Keep Wake and Sleep as buttons and the `w` / `s` keys.
  - Help text: "switch: in this run or not · the dot: awake right now".
  - Fix app.md:37 and the help line to match.

### B2. An off service reads "disabled · wakes on a connection"
- **Journey:** 2 / 3. Seen after `off cache` from ⌘K (off2-01-after-off.png).
- **What happens:**
  - The row word is **disabled**. The help legend has no such word; it calls this state "off". The strip says "1 off" and the CLI says "off".
  - The reason beside it is "wakes on a connection", which is exactly what off means it will not do. `display.Because` has no case for Disabled, so a module falls through to "wakes on a connection" and an app to "starts when needed" (internal/display/words.go:42-43 and 158-226).
  - One flag stands for two different things: `enabled = false` in doze.hcl, and `doze off` for this run (internal/daemon/handler.go:107: `Disabled = !decl.Enabled || rt.IsOff`).
  - On a running stack, an `enabled = false` row still gets the tooltip "doze off took it out of this run; the switch puts it back" (overview.tsx:636). Flipping that switch sends `on` to something that doze.hcl pauses.
- **Why it breaks trust:** One row says one thing in the word and the opposite in the sentence beside it.
- **Recommendation:**
  - Split the two states. **off** = `doze off`, for this run. Reason line: "off · nothing wakes it · switch on to bring it back".
  - **paused** = `enabled = false`, from the file. Reason line: "paused in doze.hcl". No switch; an "Edit doze.hcl" link instead.
  - Add both words to the help legend. Never show "disabled".

### B3. A crashing service is labelled "awake", with "Awake because… it sleeps after 5:00"
- **Journey:** 3 (an app crash-loops). Seen in crash2-01-api-crash-page.png and clash2-01-api-page.png.
- **What happens:**
  - The page head shows a red dot and "crashing", but the label beside the switch reads **awake** and the switch is on. The code is `on = !off && word !== "asleep"` (instance.tsx:24,113).
  - The card is titled "AWAKE BECAUSE" and says "Nothing holds it. It sleeps after 5:00 with nothing connected" (instance.tsx:252).
  - The charts show 84 MB and "2 connections" for a process that exits 1 s after it starts.
  - The List view's pane on a crashing or asleep row also says "sleep: Nothing holds it. It sleeps…" and offers Pin (crash-03-list.png, daily-03-after-sleep-cache.png).
- **Why it breaks trust:** Three statements on one screen disagree. The reader cannot tell which one is true.
- **Recommendation:**
  - The head label always shows `look.word` (crashing, waking, dozing…), never a word derived from the switch.
  - While the service is in trouble, the "why" card becomes "Why it stops" and shows the last error.
  - Hide the Pin, Sleep and memory figures for a service that isn't up, or label them "last run".

## Major

### M1. "awake · nothing holds it" contradicts the app's own definition of dozing
- In the shop fixture, api, web and mongo are "awake · nothing holds it" (shop-02-tiles.png).
- The app's help says *dozing* = "awake with nothing holding it" (help.tsx).
- The CLI reference says *awake* = "Running, and something is holding it" (website/.../reference/cli.md:94-95).
- **Recommendation:** pick one rule.
  - Either: awake with no holder is shown as dozing, with its countdown.
  - Or: redefine the words as "awake = up and has been used recently; dozing = counting down". Then change the help, the CLI reference and `Because` to match. Today three sources disagree.

### M2. The connection count has two sources, and they disagree on screen
- The Library row says "4 connections"; the strip on the same stack says "6 connections" (crash-01-library.png vs crash-03-list.png, both at 2m).
- api's row says "nothing holds it" while api's Connections chart says "2 connections".
- The Library and `Because` count connection *holders* (`display.Connections`, library.go:236; words.go:181-186). The strip and the charts count `v.Conns` (home.go vitalsOf).
- **Recommendation:** one source, `v.Conns`. Derive "N connections" in `Because` from the same field. Add a test that sums the rows and compares the total with both the strip and the Library.

### M3. The menu bar's "awake" counts dozing and waking services too
- `tray.Build` uses `display.Awake(state)`, which counts active, idle and booting (tray.go:66, display.go:16-17).
- For shop, the menu bar line and tooltip say **7 awake**. The Library and the strip say **4 awake** (with 1 dozing and 2 waking listed apart).
- The memory figure in the menu bar includes the waking ones as well.
- **Recommendation:** build the menu bar's figure from the Library's `LibStack.Fig` and `Sub`, so the two cannot drift.

### M4. The menu bar never shows the red dot, and ignores failed tasks
- `Model.NeedsYou` is computed but never used. app/tray.go sets the template icon once (line 37) and never changes it.
- The only signal is a tooltip ("needs you") on a native menu item, which macOS barely shows.
- `tray.Build` skips tasks *before* it checks for trouble (tray.go:62-64), so a failed task never counts as trouble. The `NeedsYou` comment says it should.
- D1 in the design README promises "the red dot".
- **Recommendation:** swap to a bat-with-dot template icon when `NeedsYou` is set, and include failed tasks.

### M5. Opening a stack starts it, so "stopped" never stays stopped
- **Journeys:** 1, 2, 5.
- `doze open` and bare `doze` in a project folder call `startDaemon` when no daemon is running (cmd/doze/open.go:66-75).
- The stack is then *Up* with every row asleep:
  - "Up as last time" is skipped.
  - The stopped page, with its "Choose what starts…", is never seen from a terminal.
  - A stack the developer brought **Down** in the app at 6pm comes back **Up** the moment they type `doze` to look at it.
- `doctor` says "daemon stopped (starts automatically on first use)" (doctor.go:92).
- The CLI reference says that after `down`, "the next `up` (or the first connection) brings everything back" (reference/cli.md:63). After `down` there is no listener, so the first connection cannot bring anything back.
- **Recommendation:**
  - Looking never starts anything. `doze open` shows the stopped page. Only Up, `doze up` or `doze run` start a stack.
  - Correct reference/cli.md:63 and the doctor line.

### M6. Stack Down asks nothing; sleeping one service does
- The big Up switch, space on a Library row and the ▾ menu's Down all run `doze down` at once. There is no confirmation and the switch has no pending state; the only feedback is a toast (down-01-after-down.png; overview.tsx:187; library.tsx).
- Sleeping db alone opens a dialog listing its dependants and its 4 open connections.
- The larger, more disruptive action has the weaker guard.
- **Recommendation:**
  - When anything has open connections or something is running, ask: "Stop shop? 6 connections drop; api, web stop. Data is kept." Offer Stop and Cancel.
  - Show "stopping…" on the switch until the daemon has gone.

### M7. The Library offers a green, clickable link that the stack page calls unreachable
- The Library pill **https://dev.shop.example** has a green dot and is a link.
- The stack page's banner says "dev.shop.example is not reachable here yet" (shop-01-library.png vs shop-02-tiles.png). The web tile links to the same URL.
- The pill's dot follows `AwakeNow(v)` (library.go addressesOf), not whether the domain is set up.
- **Recommendation:** while trust is missing, draw the pill amber and not as a link, with the title "not set up here yet · Set it up". Do the same on the tile.

### M8. Taking a service out of the run asks nothing, even when a running app needs it
- `off cache` from ⌘K (palette.go:341-345) and "Stop trying" (instance.tsx:118) act at once, while api is running and needs cache.
- Afterwards api is still "awake · nothing holds it", with a ⊘ cache chip. Nothing warns that its connections to cache will now fail.
- Sleep, the milder action, asks first.
- **Recommendation:**
  - Give off the same dialog as sleep: "api needs cache; its connections will fail until you switch cache on".
  - On the app, warn: "needs cache, which is off".

### M9. "Stop trying `s`": the `s` key does nothing on a crashing service
- The button shows `s` (instance.tsx:118-120). The key handler runs sleep only when the service is awake (instance.tsx:44-46). A crashing service is not awake, so the key does nothing; I confirmed no change in the page dump.
- The footer on the same page offers "w wake" instead.
- **Recommendation:** while crashing, map `s` to `off` (Stop trying). Fix the footer.

### M10. Getting started's first connect step is a command that doesn't exist
- Step 3 tells the reader to run `doze shell app` (website/.../start/getting-started.md:69-75). The binary has no `shell` command; `doze --help` lists none.
- The status samples use the daemon's words (`idle`, `active`, `reaped`). The app and the CLI reference say dozing, awake and asleep.
- The first hour fails on its first connection.
- **Recommendation:** use `psql "$(doze url app)"` or `doze run -- psql`, and refresh the samples.

### M11. "As last time" silently becomes something you never chose
- Turning one row on in a stopped stack runs `doze up X`. `rememberUp` *replaces* the remembered set with X (remember.go:186-193; library.go Stack "up").
  - Example: check one service on a stopped stack once, and tomorrow "Up as last time" starts only that one.
- At `down`, everything that happened to be awake is *added* (remember.go rememberDown), and the set never shrinks.
  - Example: a GUI client pokes mailpit once, and mailpit starts every morning from then on.
- **Recommendation:**
  - Only "Choose what starts…" writes the set.
  - A one-row start and incidental wakes don't change it.
  - Show the set on the Up switch's tooltip and in the ▾ menu: "as last time: api, web".

### M12. Tasks have two contradictory rules for when they run
- The stopped page says migrate "runs once the stack is up" (stopped.go:147-149).
- A task's own page says "A task runs once when you say so, and then gets out of the way" (instance.tsx, task empty state).
- In practice, "Up · Everything" includes every task (`enabledNames` → `bootClosure`), so `seed` runs on each full Up. With a smaller set, only the tasks that are needed run.
- Verify whether re-running seed every morning is intended.
- **Recommendation:** pick one rule and state it in both places, for example "runs when something that needs it starts" or "runs only when you press Run". Give up-everything the same rule.

## Minor

1. **Stack state has four names.**
   - "stopped" in the Library figure and the page path; "Down" as the stopped page's heading; "down" as the word on every row, tasks included.
   - CLI `down` = "sleep every service and stop the daemon" (down.go:11), so "sleep" is also used for "stop".
   - Rec: the stack is **up** or **stopped**; the verb is **Stop** (CLI `down` kept as an alias). A row of a stopped stack reads "stopped".
2. **State figures disagree in order and naming.**
   - The Library lists waking before dozing; the strip lists dozing before waking.
   - The Library counts *failed* as "crashing" (figOf), and its pill reads "api is failed".
   - Rec: one order, built once in Go; "failed" stays "failed".
3. **The map's bright edge contradicts the target's word.**
   - api→cache is bright while cache reads "dozing · idle: sleeps soon" (shop-04-map.png). The edge is "on" when *both* ends are up (overview.go:105-110), not when one keeps the other awake, which is what D18 asked for.
   - In `Because`, "api needs it" really means "held by api". Rec: edge "on" only when the target lists the source as a holder.
4. **Doctor opens Settings › General, not Machine** (library.tsx:324; set-01-doctor.png). One machine-setup concept also has many names: Machine, Doctor, Repair, "Set it up (one password)", "Set up" (docs), `doze doctor --fix`, `dns-setup`, `trust`. Rec: "Machine" everywhere in the app; the button "Set up…" opens Machine.
5. **The broken-doze.hcl banner wrongly suggests a later save takes effect.** "Nothing is applied until the file loads" reads as auto-apply. A running stack never applies a change: the next state is the "changed — Restart the stack" banner. Rec: "The stack keeps running the file it started with; fix the file, then restart the stack to use it."
6. **`doze sync` contradicts itself.** Its short help says "create new, update changed, prune removed"; its own long text says the daemon reads doze.hcl once and only down && up applies changes (reference/cli.md sync section).
7. **The app points to the CLI.** After writing a starter file the toast says "fill it in, then: doze up" (serve/service.go:863). Rec: "…then switch it on in the Library".
8. **One address, three spellings.** Tile: `http://api.shop.doze:8080`. Pane: `api.shop.doze:8080`. Library pill: `api.shop.doze`, which opens port-less through the front door. D18 says port-less. Rec: show the port-less URL everywhere it is http; the port only in the pane.
9. **The ▾ menu on a running stack offers "Up · As last time / Everything".** That reads as a no-op. "Down — this set is remembered" doesn't say which set (library.tsx:258-292). Rec: on a running stack, "Wake last time's set" / "Wake everything", and name the set.
10. **The "Last" column mixes durations and dates.** "up 29s" (how long it has been up) sits beside "up 3d ago" (when it was last up). The stack page says "last up 3d ago"; the menu bar says "up 3 days ago". Rec: "up 29s" vs "last up 3d ago", everywhere.
11. **The CLI help is stale.**
    - `down`'s long help says "use the dash: `doze`, then :sleep" (down.go:12-15).
    - `doze --help` has an empty "The dash:" group and a "Before-the-dash tools" heading.
    - `doze off` / `doze on` are missing from reference/cli.md, and its state table has no off/paused.
12. **⌘K hides things.** The verb hint omits `off` and `on`. Services of a stopped stack can't be found by name (palette.go byName adds only "Up acme").
13. **Same-named stacks are ambiguous.** Two worktrees both named `shop`: `doze://shop` silently opens the first match (app.go stackDir), and both tabs read "shop". Rec: show the folder in tab and Library when names collide; the link asks which one.
14. **Error toasts are cut to one line**, and the cut part is the reason ("down shop failed: chdir ~/code/shop: no such file …", "setting up /private/tmp/… failed: only the …"; down-01-after-down.png, empty2-01-after-1.png). Rec: wrap error toasts, or open the full text on click.
15. **"off" also means "failed"** in the crash banner: "doze will try again, then stop and leave it off until you say". After the last try the state is *failed*, not `doze off`. Rec: "…then stop trying and mark it failed".

## Polish
1. ⌘O (open folder) and ⌘0 (Library) look the same in the kbd font. Both appear in the Library header, the footer and the help.
2. The Library's Machine line reads "checking…" on first paint and stays that way for seconds (shop-01-library.png).
3. The sidebar and help show a branch name ("archive/dash") as the version.

---

## Vocabulary

| Word | Every meaning it has today | Recommended single meaning |
|---|---|---|
| **switch (row)** | awake now (running stack); "part of this run" (CLI `off` help, D18, help legend, code comment); "start the stack with this one" (stopped stack); "Run" button (task) | In this run or not (`on`/`off`). Moves only when a person moves it. |
| **switch (stack, "Up")** | daemon running (`doze up --last` / `doze down`) | Keep: stack up / stopped. |
| **on** | switch position; `doze on` (back in the run, starts nothing); the app's toggle (on, then wake) | `doze on`: in the run. The app's switch does the same; Wake is separate. |
| **off** | `doze off` (out of the run); `enabled=false` in doze.hcl; "leave it off" = failed after retries; `pin --off`; `DOZE_ZONE=off` | `doze off` only. `enabled=false` → **paused**; retries exhausted → **failed**; `pin --off` → `unpin`. |
| **disabled** | the row word for both off and enabled=false; "Disabled services are skipped" (up help) | Retire it. Use off / paused. |
| **awake** | active (strip, Library); active+idle (rows' memory, `h.Awake`, stacks API = 5); active+idle+booting (menu bar = 7); "awake · nothing holds it" (= dozing by the help's definition); the crashing head label | Up and held by something. Count it the same way everywhere. |
| **dozing** | idle (daemon); "awake with nothing holding it" (help) while rows say "awake · nothing holds it" | Up, nothing holds it, counting down. Every unheld awake service shows as this. |
| **idle** | daemon state for dozing; "idle: sleeps soon"; CPU key "the rest idle"; idle timeout | Only the timeout ("idle after 5:00"). The state is dozing. |
| **asleep** | not running, wakes on connect; help: "stopped, data kept" | Not running; the next connection wakes it. Don't define it with "stopped". |
| **down** | stack verb (`doze down`); stopped-page heading "Down"; the word on every row of a stopped stack, tasks included; Library footer "up / down" | The CLI verb only (alias of Stop). A stopped row reads "stopped". |
| **stopped** | stack not running (Library, path); help defines asleep as "stopped, data kept" | The stack is not running. Nothing else. |
| **sleep** | one service down until the next connection; `doze sleep` with no name; `down` "sleeps every service" | One service, wakes on the next connection. Down "stops". |
| **up** | stack running; `doze up` (start + wake the set); "up 29s" (duration); "up 3d ago" (point in time); ▾ menu "Up" on a running stack | Stack is running / start it. Use "last up" for the past. |
| **needs** | declared dependency (map, chips, start sheet); "api needs it" = held by api right now (`Because`) | Declared dependency only. Holding reads "held by api". |
| **holds / held by** | a holder (connection, dependant, pin); "nothing holds it" shown beside 2 connections | Every live reason to stay awake, connections included, from one source. |
| **ready** | task not run yet; (developer's sense: healthy, accepting connections) | "not run yet". Ready reads as healthy. |
| **run** | logs "by run" (a process lifetime); "part of this run" (stack session); a task run; `doze run`; app `run =` command | Process lifetime = **start**; the session = **this session**; keep task run and `doze run`. |
| **crashing** | retrying after exits; also used for failed in the Library count; also a port clash | Retrying. A port clash says "port 7799 taken". |
| **Machine / Doctor / Repair / Set it up / Set up / trust / dns-setup** | the same machine-setup concept | **Machine** in the app; the button is "Set up…". |
| **pinned / Pin awake / pin --off / Unpin** | never sleeps | "Pinned awake"; the CLI also takes `unpin`. |

## Developer expectations (Docker Desktop, OrbStack, compose, Tilt)
- **Docker Desktop and OrbStack:** a switch is the desired state and stays put. doze's switch is a live readout (B1). That is the biggest gap.
- **`docker compose up` / `down`:** "down" is final until "up". With doze, `doze` / `doze open` / `doze run` / `doze env` quietly start a downed stack again (M5).
- **Tilt:** it shows *why* a resource is in its state, and so does doze. The why line is doze's best feature, but it contradicts the state word in four places (B2, B3, M1, M2).
- **compose:** `enabled = false` maps to compose profiles, which are file-level. doze mixes file-level and session-level off into one word (B2).

## Works well
- The crash page: what it said before it exited, "try 3 of 5", and Try now / Stop trying in one place (aside from the key and label bugs above).
- The sleep dialog names what is affected (dependants, open connections) and says the data is kept. It only asks when something else is affected.
- The empty-folder page: what doze found, what each item becomes, "nothing runs until you bring the stack up", and "doze never edits doze.hcl behind your back".
- The stopped stack keeps its page and layout, with one sentence saying exactly what Up will start. This fix holds.
- A broken doze.hcl is shown at the line with the loader's message, and "the same message as doze lint". The stack keeps running.
- The port clash tells doze's own processes apart from others' ("bring its stack down from the Library instead").
- ⌘K rows carry their CLI equivalent (`doze sleep db`, `doze up --last`), which teaches the CLI for free.
- The Library is a real front door: running stacks first, "last time: api, web, db" on a stopped row, and Forget that leaves files alone.
