# doze desktop: visual system and accessibility audit (cross-cutting)

Method: built binary served with the shop, long, crash, modules, badconfig, portclash and empty fixtures. I took WebKit screenshots of 12 pages in all 10 theme × mode combinations at 1280×800, plus 900×620, 1920×1080 and the long-names fixture. In Chromium I ran axe-core 4.13 (full rules) on 19 page/fixture pairs in violet-dark, and its colour-contrast rule on 10 pages in each of the other 9 themes. I recorded Tab order with computed outline on 8 pages, measured computed type and button sizes on 12 pages, and checked token pairs for contrast with a script.

Note: axe-core is **not** in test/desk-e2e/node_modules (package.json has only @playwright/test). I injected a copy found elsewhere on disk. To make this repeatable, add `axe-core` (or `@axe-core/playwright`) as a dev dependency.

Evidence root: `test/desk-e2e/.desk-look/ux-2026-11-06/visual/` (below as `V/`).
- `V/themes/<theme>-<mode>-<page>.png` (100 shots); contact sheets in `V/sheet-*.png`
- `V/narrow/`, `V/wide/`, `V/long/`, `V/longnarrow/`, `V/fx4815x/`
- `V/focus/*.png` (each focused element), `V/sheet-focus.png`
- `V/axe-full.json` (all rules, violet-dark), `V/axe-themes.json` (contrast, 9 other themes)

Counts: **0 blocker · 9 major · 13 minor · 7 polish**

---

## MAJOR

### M1. `--line` is two tokens at once, so body line-height is invalid everywhere
- **Where:** every page, every theme.
- **Seen:** tokens.css:6 sets `--line: 20px;` (the line-height size). tokens.css:32 and every theme block then redefine `--line` as the border colour. shell.css:26 `line-height: var(--line)` therefore resolves to a colour. That is invalid at computed-value time, so it falls back to `normal`. Measured `getComputedStyle(body).lineHeight` = `"normal"` (about 15.7px for Inter 13px) on every fixture. The boards (app.css:3) use `13px/20px`.
- **Why it matters:** every block of text that doesn't set its own line-height is about 4px tighter than designed. Examples: "Why it's awake" prose, help descriptions, banners, the empty-state copy and settings captions. This is the one rule that ties the app to the boards, and it is silently off.
- **Fix:** rename the size token in internal/brand/tokens.go, e.g. `--leading: 20px`, and use `line-height: var(--leading)` at shell.css:26. Add a generator check that size and colour token names never collide.

### M2. Tiles and list rows are `<button>`s with buttons inside them (nested interactive controls)
- **Where:** tiles, list, map, stopped stack, modules fixture (axe `nested-interactive`, 43 nodes on 8 pages).
- **Seen:** `<button class="tile">` contains the switch `<button class="sw">`, the address `<button class="a link">`, the copy button and the `.runbtn`. `<button class="qr">` contains its switch and run button. Nested buttons are invalid HTML. Screen readers announce the whole tile as one button, so the inner controls get merged or lost. Tab still reaches the inner ones in Chromium, but the behaviour differs by engine.
- **Why it matters:** VoiceOver users can't reliably reach the wake/sleep switch or the copy action, which are the app's main verbs.
- **Fix:** make the tile and row a `div role="group"` (or `role="row"` inside a grid). Give the name area its own `<button>` for open/select. Keep the switch, link and copy as siblings. If the whole surface must be clickable, use a stretched-link `::after` on the name button. CSS for `.tile`, `.qr` is unchanged except `text-align:left`.

### M3. Tab is hijacked on the service page: keyboard users can't reach any control there
- **Where:** inside:<service> (all themes and sizes). Evidence: Tab trace `V/logs/tab.out` shows 45 Tab presses and `document.activeElement` = BODY every time.
- **Seen:** pages/instance.tsx:34 `case "Tab": e.preventDefault(); …setTab(next)`. Tab cycles Overview/Logs instead of moving focus. Pin awake, Restart, Sleep, Copy, the sidebar service list and Overview back can never receive focus.
- **Why it matters:** this breaks keyboard operation (WCAG 2.1.1) on the most-used detail page. It also contradicts every other page, where Tab moves focus.
- **Fix:** move page-tab switching to `[` / `]` or ⌃Tab (the logs page already uses `[ ]` for runs), or to arrow keys while a `.ptab` has focus (the standard tablist pattern). Leave Tab to the browser. Update the keys bar text "tab next tab".

### M4. Modal dialogs don't take focus, don't trap it, and aren't marked modal
- **Where:** the start sheet ("Choose what starts…", `c` on a stopped stack); also the confirm dialog, help and palette. Screenshot: `V/startsheet-1.png`.
- **Seen:** when the sheet opens, focus stays on BODY. One Tab moves real focus to the **Tiles segmented button behind the scrim**: the accent outline is visible on "Tiles" through the overlay. dialog.tsx:35 and overview.tsx:414 track a fake `at` index instead of DOM focus. None of the `role="dialog"` sections have `aria-modal="true"` or `aria-labelledby`.
- **Why it matters:** screen readers don't know the page behind is inert, and keyboard focus wanders under the scrim. Enter can then act on a hidden control.
- **Fix:** on open, focus the default button (or the first pick). Set `aria-modal="true"` and add `inert` to `.win > :not(.scrim)` while open. Loop Tab inside the section, or use `<dialog>.showModal()`, which does all of this. Drive the highlighted button from real focus instead of `at`.

### M5. Muted text on the selected row (`--pick`) fails contrast in all 10 themes
- **Where:** Library selected stack row (path, "551 MB · 4 connections", "up 1m"); list selected row (`.why`); palette highlighted item (`.m`, `.k`); settings/inside sidebar `.si.on small`. Pairs and ratios:

  | text | background | ratio |
  |---|---|---|
  | `--dim` | `--pick` | 3.24 violet-dark · 3.89 teal-dark · 3.60 slate-dark · 3.84 teal-light · 3.90 violet-light · 4.12 burgundy-light · 4.30 amber-light |
  | `--dim` | `--sel` | 4.15 violet-dark / slate-dark |

  The state words also fail on `--pick` in the light themes: good 3.26, warn 3.72, cool 4.04 (violet-light). axe flags 27 nodes in violet-dark and 60 across the other themes. Screenshots: `V/themes/*-library.png`, `V/themes/*-list.png`.
- **Why it matters:** the row you're looking at is the one with the least legible detail.
- **Fix:** add `.lrow.sel .dim, .lrow.sel .when, .lrow.sel .name .dir, .qr.sel .why, .pal .it.on .m, .pal .it.on .k, .si.on small { color: var(--text); }`. Or lighten `--pick` in light mode and darken it less in dark (target `--dim`/`--pick` ≥ 4.5). Give state words on `.sel` rows a `font-weight:600`, or use the bright variants.

### M6. Muted text placed on an accent-filled primary button is invisible
- **Where:** "Set it up **(one password)**" banner button on every overview with an untrusted domain, all themes. Ratios: 1.45 (violet-dark), 1.03 (teal-light), 1.52 (teal-dark), about 1.2 (violet-light). Crop: `V/crop-banner-vl.png`. Source: overview.tsx:222 `<span className="dim">` inside `.btn.pri`.
- **Why it matters:** the one warning that it needs a password is unreadable. It reads as a smudge on the button.
- **Fix:** shell.css add `.btn.pri .dim, .btn.danger .dim { color: inherit; opacity: .7; }` (the same treatment as `.btn.pri kbd`, shell.css:90). Better still, move "(one password)" outside the button as `.banner .t span` copy.

### M7. Borders of off and unchecked controls, and the asleep dot, are near-invisible (non-text contrast)
- **Where:** all themes; worst in light. Token ratios against bg/panel:
  - `--line2` (switch-off border, `.box`, `.radio .o`, `.input`/`.filter` border, `.dot` default) = 1.37–1.79
  - `--track` (off switch fill) vs bg = 1.13–1.25

  WCAG 1.4.11 asks for 3:1. Crop: `V/crop-outside-teal-light.png`. The "asleep" dots on mailpit/search are barely there, the off switch is only identifiable by its knob, and the unchecked "worker" box in the start sheet (`V/startsheet-1.png`) is a faint square.
- **Why it matters:** asleep/off is a core state. In light themes the grey dot reads as "no dot", so the state column loses a symbol that every other state has.
- **Fix:**
  - `.dot { background: var(--dim); }` for asleep (keep `--line2` only for truly "none").
  - `.sw { border-color: var(--dim); }` when off.
  - `.box, .radio .o { border-color: var(--dim); }`.
  - For inputs, `.filter, .input { border-color: color-mix(in srgb, var(--dim) 55%, var(--line2)); }`.
  - Or add a `--control-edge` token per theme, checked ≥ 3:1 in tokens.go.

### M8. The narrow-window media query removes navigation and double-indents content
- **Where:** 900×620, all pages. Sheets: `V/sheet-narrow1.png`, `V/sheet-narrow2.png`.
- **Seen (a) lost navigation:** shell.css:404 `.side { display:none }` at ≤1000px, with no rail in its place (the comment says "the sidebar is a rail"; `.rail` exists at shell.css:71 but isn't used). As a result:
  - Settings loses General/Machine/About, so Machine and About are unreachable from Settings.
  - The logs page loses its "Show" service checkboxes, so you can't filter by service.
  - The service page loses its service list and "‹ Overview".
- **Seen (b) misalignment:** shell.css:407 gives `.strip, .gl, .tiles, .ql, .lib, .banner, .model, .machine` both 16px margin **and** 16px padding. Section labels, the strip and tiles start at x=32 while the h1, toolbar and Filter start at x=16. In the list, "APPS" sits at x=46 against Filter at x=16. The Library table gets an extra 16px inner gutter.
- **Fix:**
  - (a) At ≤1000px render `.side` as the 46px `.rail` (icons and dots with titles), or as an overlay toggled by a button. Never `display:none` for settings and logs.
  - (b) Replace line 407 with margin only: `.strip, .gl, .tiles, .ql, .lib, .banner, .model, .machine, .steps, .ways, .chk, .map, .dock, .frame, .tl { margin-left:16px; margin-right:16px; }` and `.head, .toolbar, .keys, .ptabs { padding-left:16px; padding-right:16px; }`. Better: make `--space-7` itself drop to 16px under the query, e.g. `:root { --gutter: 28px } @media (max-width:1000px){ :root{ --gutter:16px } }`, and use `var(--gutter)` everywhere `var(--space-7)` is used as a page gutter.

### M9. The overview strip is `role="status"`, so a live-updating figure strip is a chatty live region
- **Where:** overview.tsx:325 (`role="status"`) and :309 (`role="alert"`); every running stack.
- **Seen:** the strip holds memory, cores, awake and connections, which update constantly. Any change re-announces the strip politely. Meanwhile the events that matter, a service going asleep/waking/crashing, aren't in any live region (only toasts are, toasts.tsx:24).
- **Why it matters:** VoiceOver users get a stream of "552 MB resident 0.03 cores…" and miss "api crashed".
- **Fix:** drop the role from `.strip`; use a plain `<section aria-label="Stack summary">`. Add one visually hidden `aria-live="polite"` node that receives short sentences on state transitions ("api is asleep", "worker crashed, try 3 of 5"), and use `role="alert"` only for crashes.

---

## MINOR

### m1. The logs page uses native, unthemed checkboxes; everywhere else uses the custom `.box`
- **Where:** logs sidebar "Show" in all 10 themes, where they render system blue (`V/sheet-logs.png`). Compare the start sheet's accent `.box` checkboxes (`V/startsheet-1.png`). Source: components/sidebar.tsx:93, :99.
- **Fix:** add `input[type=checkbox] { accent-color: var(--accent); }` to shell.css, or render `.box` with a visually hidden input. Also remove the duplicate `.box` rules (shell.css:320–322 = 340–342; `.pop .tick .box` at :336–337 repeats them again).

### m2. Section-label ("eyebrow") style has 6 variants for one idea
Measured:

| selector | size / weight | letter-spacing | colour |
|---|---|---|---|
| `.gl` / `.qh` / `.map .col` | 11px 700 | 1.1px | dim |
| `.side h4` | 11px 600 | .88px | dim |
| `.insp h3`, `.pal .ph` | 11px 600 | .66px | dim |
| `.lrow.h`, `.ctr.h`, `.rr.h` | 11px 600 | .55px | dim |
| `.card h3`, `.hc h3`, timeline `h3` | **12px** 600 | .72px | dim |
| `.cr.sec`, `.grp` | 11px 700 | .88px | **accent** |

On one screen (inside:api) "APPS" in the sidebar is 11px and "REACH IT" on the card is 12px, side by side.
- **Fix:** add a single `.eyebrow` (11px / 600 / .08em / `--dim`) and point all of these at it, keeping `.cr.sec`/`.grp` accent only if it's meant as a different level. Board app.css should be the arbiter.

### m3. The type scale has drifted off its tokens
- **Seen:** tokens define 11/12/13/14/16/20/22. Measured in use: 10, 11, 11.5, 12, 12.5, 13, 14, 15, 20, 22. **16 (`--text-lg`) is used only by `.dialog h2`**, and weights are 400/500/600/650/700.
- **Off-scale literals in shell.css:**
  - 12.5px: :53, :166, :182, :212, :288, :327, :334, :379, :381
  - 11.5px: :69, :169, :232, :368, :502
  - 15px: :160, :190, :357–358, :498
  - 18px: :272
  - 10px: :318, :371
  - 9px: :44
- **Inline styles in TSX:** 110 `style={{…}}`, including `fontSize: 12/12.5/11.5/14` and `height: 26` on a `.btn.pri` (library.tsx:320).
- **Fix:** snap 12.5 to 13 (`--text-base`), 11.5 to 11 (`--text-xs`), and 15/18 to `--text-lg` (16). Replace inline fontSize/height with classes. Pick one heavy weight (650 or 700) for titles and labels.

### m4. Control heights and radii vary across similar buttons
- **Measured heights:**
  - `.btn` 30
  - `.btn` with inline height 26 (library machine bar)
  - `.seg`/`.ib`/`.tog`/`.filter` 28
  - `.runbtn` 24
  - `.code .copybtn` 24
  - `.input` 34
  - `.copy` text-buttons 14–15
- **Radii:** 7 (`--radius`) vs 6 (`.runbtn`, `.copybtn`) vs 8/9/11 literals (`.code`, `.ctable`, `.qr`, `.swatch`, `.node`).
- **Fix:** define `--control: 28px` and `--control-sm: 24px`, apply them to `.btn`/`.seg`/`.tog`/`.filter` and `.runbtn`/`.copybtn`, and use the radius tokens only.

### m5. Three switch sizes and four disabled opacities
- **Switch sizes:** `.sw` 34×18 (tiles, list, settings), `.sw.big` 44×24 (Library rows and the overview "Up"), `.tog .sw` 26×14.
- **Disabled:** `.btn:disabled` .45, `.sw.dis` .4, `.sw:disabled` .5, `.ptab.dis` .4, and `.ib.ghost` .3.
- **Fix:** keep two switch sizes at most (row and header), and set one `--disabled: .45`.

### m6. The keyboard-hint (`kbd`) style has at least 6 looks
| where | style |
|---|---|
| `.keys kbd` | bright on `--sel`, mono 11/600 |
| `.btn kbd` | dim, no background, 11/500 |
| `.filter kbd` | dim on `--sel` |
| `.pal .it .k` | dim, no background |
| `.pal .it .acts kbd` | **UI font**, bordered |
| `.pal .foot kbd` | 10px |
| `.kr kbd` / `.cact kbd` / `.hp .foot kbd` | — |

- **Fix:** have one `kbd` base rule (mono 11/600, `--sel` background, 4px radius). The only variant should be "inside a button" (no background, 70% opacity).

### m7. Focus rings are clipped or drawn on the wrong element
Evidence: `V/sheet-focus.png`.
- **Filter field:** the ring is drawn on the bare inner `<input>`, so a square box sits inside the rounded `.filter` pill.
  - Fix: `.filter:focus-within, .input:focus-within, .pal .in:focus-within { outline:2px solid var(--accent); outline-offset:1px } .filter input:focus-visible, .input input:focus-visible { outline:0 }`.
- **Segmented control:** `.seg { overflow:hidden }` clips the ring to a single vertical line ("Tiles" in the crop). Same for `.ctabs`.
  - Fix: `.seg button:focus-visible { outline-offset:-2px }`.
- **Scrollable logs `.lg`:** it gets the browser's default blue `auto` ring, not the accent, because it is focusable without `[tabindex]`.
  - Fix: add `tabindex="0"` and an aria-label (this also clears axe `scrollable-region-focusable`), or extend the selector to `.lg:focus-visible`.
- **Selected tile vs focused tile:** both are accent (shell.css:156 and :34), so selection and focus can't be told apart.
  - Fix: give focus a 2px offset ring in `--bright`, or keep the selection border at `--line2` plus an accent left bar.

### m8. Light themes: green state text fails AA on white
- **Where:** violet, teal and burgundy light themes.
- **Seen:** `--good` #1d8a4a on #fff = 4.39 and on bg #fbfaff = 4.22. Every "awake" word, the "4 awake" figure and log who-colour "api" (4.02 on `--well`). amber-light `--extra` #b85c00 on `--well` = 4.21.
- **Fix:** in tokens.go set light `Good` to #1a7d43 (≈5.0 on white) and `Extra` to #a85400.

### m9. Muted text on tinted banners and the `.gl` sub-labels fall under 4.5
- **Banners:** `.banner .t span` and `.btn.quiet` in a cool banner = 4.13 (violet-dark) and 4.37 (teal-light); warn banner span = 3.96.
- **Group sub-labels:** `.gl span` and `.map .col span` have `opacity:.8` (shell.css:150, :205), which gives 3.79 dark and 3.28–3.39 light ("your code", "from the doze registry").
- **Fix:** drop the `opacity:.8` (use `--dim` as is). For banners, use `color: var(--text)` on `.banner .t span`, or lower the tint from 10% to 6%.

### m10. Status colours do two jobs in logs and in the amber theme
- **Logs:** the `who` palette (shell.css:417–418) colours services with `--warn` and `--extra`, so "web" is the same yellow as a WRN line and as "dozing". "mongo" is orange (`V/themes/violet-dark-logs.png`).
- **Amber theme:** the accent is yellow (#F2E04A), the colour every other theme uses for "dozing", while amber's own `--warn` becomes orange. Switches and primary buttons read as "warning" (`V/sheet-tiles.png`).
- **Fix:** give log sources a dedicated non-status palette (tokens `--who-0..5`). For amber, document the shift, or keep `--warn` yellow and move the accent to a gold that is distinct from it.

### m11. Very wide windows (1920×1080) stretch rows and the strip into gaps
Evidence: `V/sheet-wide.png`.
- **List:** the name column and status column sit about 220px apart (`.qr` grid, shell.css:174).
- **Strip:** "552 MB" sits far left and cores/awake/connections far right.
- **Map:** columns are fixed and fill only two-thirds.
- **Service page:** the empty "Recent" card stretches to full height.
- **Fix:** cap content width (`.main > * { max-width: 1440px }`, or `.ql, .strip { max-width: 1280px }`). In `.qr` use `minmax(140px, 260px)` for the name column. `.con-row > .card { align-self:flex-start }`.

### m12. Truncation is inconsistent with long names
Evidence: `V/sheet-long.png`.
- **Timeline lane names:** clipped with no ellipsis ("customer-success-a", "primary-postgres-cl"). `.tl-name` is `display:flex`, so `text-overflow` on it does nothing (shell.css:470). Fix: wrap the text in `<span class="t ell">` and set `title`.
- **Logs `who` column:** end-ellipsis at 104px makes "customer-succ…" ambiguous for two services. Fix: use a middle ellipsis like the Library path, or a 140px column.
- **Stopped-stack explainer:** the "Modules wake on their ow…" sentence is ellipsised at 900px. Fix: prose should wrap; set `white-space:normal` on that line.
- **Palette footer:** "type a name, or a verb then a name: s" is cut mid-word at 1280px (shell.css:368–370). Fix: let it wrap, or shorten it.

### m13. Copy icons and text-links are tiny targets
- **Measured:** tile/list copy glyph buttons are **8×15px**; inspector "Copy" is 27×14 and "Pin" 16×14 (Tab trace). That is below the WCAG 2.5.8 minimum of 24×24.
- **Fix:** `.copy { min-width:24px; min-height:24px; display:inline-flex; align-items:center; justify-content:center; }` (negative margins keep the layout).

---

## POLISH

- **p1. axe structural nits.**
  - Library `.lrow.h` is `role=row` but its children aren't `columnheader`s. Data rows hold buttons directly, without `cell`/`gridcell`.
  - The Machine `.chk` is `role=table` with no rows.
  - Palette `role="option"` items have no `listbox` parent (palette.tsx:96).
  - Heading order jumps h1→h3 (timeline, help, inside cards).
  - Fix: add `role="columnheader"`/`"cell"` spans, wrap palette items in `role="listbox"`, and make card titles `h2`.
- **p2. Settings radio groups take one Tab stop per option.** Mode System/Light/Dark and the five theme swatches are `role=radio`, but each is a Tab stop. Use roving tabindex with arrow keys inside `role="radiogroup"`.
- **p3. Titlebar shows "Library" as the active tab while Settings is open** (`V/themes/violet-dark-settings.png`). Neither tab should be `.on`; add a transient "Settings" tab.
- **p4. Hover on tiles and nodes is a border change from `--line` to `--line2` (≈1.1:1 delta)**, which is effectively no hover (shell.css:155, :207). Use `background: color-mix(in srgb, var(--sel) 50%, var(--panel))`.
- **p5. Stale header comment.** shell.css:3–4 still mentions "legacy.css", which no longer exists. `.col` is defined twice with conflicting meanings (shell.css:204 `.map .col` absolute vs :249 flex column).
- **p6. Light-mode scrim and shadows use dark rgba.** `.scrim` rgba(9,8,14,.62) and shadows rgba(0,0,0,.45–.5) look heavy and grey on light themes. Add `--scrim`/`--shadow` tokens per mode (e.g. light scrim rgba(20,18,30,.28)).
- **p7. Switch labels contradict the visible state words.** Switch aria-names say "worker is awake" and "cache is awake" while the tile says "waking" and "dozing". Name the switch by its action/target ("Keep worker running", pressed or not) or use the same state word.

---

## Works well

- One focus colour (`--accent`, 2px) is applied consistently via `:focus-visible`; mouse clicks don't show rings.
- Text contrast for the main pairs (`--text`, `--bright` on bg/panel/side/well) passes AA in all 10 themes. `--dim` passes on bg and panel everywhere. Primary buttons (`--ink` on `--accent`) pass in all themes.
- All 10 themes render with no theme-only layout breaks; the token generator keeps them structurally identical.
- Tiles keep their 4/3/2-column rhythm, and long names ellipsise cleanly in tiles, list, tab titles and the sidebar. The Library path's left-side elision (rtl + bdi) is a good touch.
- Most controls have real accessible names (`aria-label` / `title` on switches, icon buttons and pills). Dots and glyphs are `aria-hidden`.
- The tablist in the titlebar, `role="log"` on log views, toasts in `aria-live="polite"` and the error banners as `role="alert"` are the right primitives; they just need the fixes above.
- The Tab order on Library, overview, logs, timeline and settings follows the visual order and wraps cleanly.
