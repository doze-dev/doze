# Adaptive layout: one design for every window

Written 2026-10-16. Applies to Home first (the page that is on screen most), then
the stack's logs and an instance's page. It answers one question: what does the
dash do with a window that is not the one it was drawn for, small or large?

Status of each claim: **measured** (rendered and looked at), **read** (read in
the code), **recalled** (from memory of other tools; the search for a source came
back generic, so treat as unverified).

## What is wrong today

Rendered at 100×30, 190×54 and 260×70 (`go run . -size 260x70 au-home` in
`tools/look`). **measured**

- **Big windows stretch, they do not inform.** At 260×70 the meters sit in a small
  cluster on the left and the state block is pushed to the far right; the RUNS
  column floats far from NAME, so the eye cannot follow a row; the logs box is
  about fifty rows tall with twelve lines in it. Extra width and height are spent
  on blank space.
- **Cliffs.** What is drawn is decided by thresholds scattered through the code
  (**read**): `w<100` and `w<120` for the header, name caps at 110/130/160, the
  meter key at `h>=34`, a minimum of 60×16, a separate layout at 80×24, and a
  right column on an instance's page at `w>=100`. Between two of them, one more
  row makes a thing appear or vanish. At 100×30 the memory bar's colours have no
  key, because 30 is under 34.
- **The wide design exists and is mostly not built.** `boards/W-Home.dc.html`
  (page 7 of the canvas) draws a wide window as a master and a detail: the stack
  as a list on the left, and on the right the selected instance's vitals and logs.
  Only the charts were built (D30–D32).

## Principles

1. **Size is a budget, spent in priority order.** There are no cliffs: every part
   has steps, each with a cost in rows and columns, and what is left after the
   necessary is spent on upgrades in a fixed order.
2. **More space buys information, never stretch.** Extra width shows more (a
   detail beside the list, a trend, more names); it never pads a column.
3. **Small windows lose the least important first, and say what they lost.** A
   line that says `↓ 44 more`, a key bar that keeps `? help`.
4. **The same data, one more or one less step.** A window is never a different
   program: the keys do the same things at every size.
5. **A size is not a setting.** Nothing to configure; the dash reads the window,
   and re-reads it on every resize.

## The profile

One value, computed once per frame from the window, that every part asks instead
of comparing `w` and `h` itself.

| width class | columns | what it is (estimates, **not** measured on a machine) |
|---|---|---|
| compact | under 100 | a small or split window; the old 80×24 |
| standard | 100–175 | half a laptop screen; a maximised small laptop |
| wide | 176–219 | a maximised 14″ laptop or larger |
| grand | 220 and up | a large laptop or an external display |

| height class | rows |
|---|---|
| short | under 28 |
| normal | 28–47 |
| tall | 48 and up |

Under 60×16 the dash asks for a larger window (D45) and that does not change.

## The budget, for Home

Rows, top to bottom. **Necessary** is always drawn; the rest is bought in the
order listed, while rows remain.

| order | part | cost | steps |
|---|---|---|---|
| necessary | key bar | 1 | hints dropped by rank, never `? help` |
| necessary | title line | 1 | the folder dropped before the stack name |
| necessary | table header + at least 6 rows (or all, if fewer) | 7 | |
| necessary | logs, 8 lines and their box | 10 | a troubled instance gets half the room |
| 1 | the meters as bars (mem, cpu, conns) | 3 | one line each, or a one-line summary under 100 columns |
| 2 | the line under each bar naming its colours | +2 | bought only while the table still shows up to 16 rows (a small stack gets it at 24 rows, the board's 15 rows at 34) |
| 3 | every table row | up to all | scrolls, saying `↓ N more` |
| 4 | more log lines | the rest | |

So at 100×30 the order is bars, then the key, then rows; at 24 rows the key is
the first thing to go, and the bars become a summary line before the table loses a
row. **New:** today the key appears at a bare `h>=34`.

## The geometry, by width

**Compact (under 100).** One column. The header is a one-line summary
(`≈ 552 MB · 0.03 cores · 6 conns`), the table loses columns in the existing order
(kind, conns, cpu, uptime, engine) and `?`, `l` and `esc` stay in the key bar.
Home at 80×24 keeps its own screen (`Narrow.dc.html`).

**Standard (100–175).** Stacked, as today: header, table, the selected instance's
logs under it. Two changes: the table is only as wide as its content (a column
stops growing at its widest cell, so RUNS is beside BECAUSE, not across the room),
and the header row and the selected row still run the full width, as in htop.

```
 doze  shop ~/code/shop                                      ≈ 552 MB resident
 mem   552 MB   [|||||||||||||||||||||||||||||]     state ● 4 awake ◐ 1 dozing
                web 213 · mongo 148 · db 96 · api 84 · +1 more
 cpu   0.03 cores [||||||||||||||||||||||||||||]
                api 0.02 · db 0.01 · 3 idle
 conns 6 open across the stack
 NAME      STATE    MEM   CPU%  CONN   UP  BECAUSE              RUNS
 ● api     awake  84 MB    2.1     4  14m  runs until stopped   go run ./cmd/api
 ...
╭─ api · logs · following ───────────────────────────────────────────────────╮
```

**Wide (176–219).** Master and detail, after `W-Home`: the table on the left at
its natural width (about 110–125 columns, content-capped), and on the right, the
whole height of the page, the selected instance: its caption, its vitals as
charts as tall as the rows allow, and its logs. The header across the top stays.
Selecting a row changes the right side at once; `⏎` still opens the instance's
page, which is now the same thing with the list gone.

```
 doze  shop ~/code/shop                                                   ≈ 552 MB resident
 mem ... bars and key ...                       state ...
 NAME       STATE   MEM  CPU% CONN UP  BECAUSE        │╭─ api · awake · up 14m ────────────────╮
 ● api      awake 84 MB  2.1    4 14m  runs until st… ││ cpu 2.1%  ▁▂▃▅▂▁▂▃▅▇▅▃  last 4m       │
 ● web      awake 213 MB 0.3    1 14m  runs until st… ││ mem 84 MB ▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃▃ │
 ...                                                  │╰──────────────────────────────────────╯
 ↓ 12 more                                            │╭─ logs · following ───────────────────╮
                                                      ││ 14:04:07 WRN slow query 212ms ...    │
```

**Grand (220 and up).** The wide layout, plus a memory trend per instance in the
table (twelve cells, from the history the dash already holds); the detail's
charts go side by side on their own once the detail is wide enough (`W-Charts`). Nothing else grows: the windows are bigger, the
type is not.

## The meters, at every size

- The key under each bar names as many instances as the width holds, up to eight,
  and ends `+N more · M MB` so the rest is not just a count (D46 follow-up).
- Wide and grand give the meters the whole left half, not a 64-column cap.
- Under 100 columns the meters are one summary line.

## Height

- **Short (under 28):** bars only if they fit after the necessary; the key bar and
  table keep their rows.
- **Normal:** the budget above, which is today's behaviour with the key added when
  there is room.
- **Tall:** every row of the table, then log lines. Because the logs are the
  elastic part, a tall window never has a blank band while there are lines to show.

## The instance's page and the stack's logs

- The instance's page already has a right column from 100 columns; it moves to the
  profile (standard and up) so it and Home agree. Its vitals are the same blocks
  the wide Home's detail uses, so the two cannot disagree.
- The stack's logs page is one list; it keeps its single column at every size, and
  only its source column's width follows the profile.

## What this does not do

- No setting, no key to cycle layouts. If the wide split is wanted off, a later
  key can do it; it is not here because nobody has asked.
- No change below 60×16, and none to the keys.
- No scaling of text: a terminal does that, not the program.

## How it is tested

- **A size matrix**, extending `stress_test.go`: every shape of stack at 80×24,
  100×30, 140×40, 190×54 and 260×70, asserting the existing invariants (no panic,
  width, height, no control bytes) and new ones: no blank band taller than a
  fifth of the window while there is content to show; the selected row is always
  on screen; the key bar always holds `? help` and `esc`.
- **Budget tests**: at a stated height the right steps are bought (bars at 28, key
  at 34 rows with a ten-row table, the full table at 54).
- **Screens**: `au-*` scenes at the five sizes, drawn beside the boards, in
  `ux-audit/`.
- **Resize fuzz**: the existing fuzz already resizes; it now crosses the width
  classes.

## Why 176 and not 160

Built and rendered at 160 first: the detail beside the list was 59 cells wide, too
narrow for a chart, and the list had to drop three columns to make room. At 176
the detail is about 75 cells and the list keeps its columns. The boundary is a
measured one, and the profile is the one place to move it.

## Built (2026-10-16)

The profile and the budget (`layout.go`), columns that stop at their content, the
full-width header and selected row, the master and detail with the list's activity
under it (`detail.go`), the memory trend at grand, and the size matrix
(`layout_test.go`). The screens are `ux-audit/after-size-*.png`. Not built: the
single-line meter summary under 100 columns (the meters keep their bar there, one
line each) and `+N more · M MB` on the key is built but the key's width is the only
thing that grows with the window.

## Files

New `internal/dash/layout.go` (profile, budget); changed `table.go` (the one big
`homeTable` is split: header, table, detail), `meters.go` (key width, `+N more · M
MB`), `focus.go` (the page's right column uses the profile), `home.go`; new
`layout_test.go`; `tools/look` scenes at the five sizes.

## Sizing, honestly

About two to three days. The first step (the profile, the budget, natural-width
columns) is the day that changes how it feels at every size. The master/detail is
the largest piece and the riskiest, because `homeTable` builds the table and the
logs in one function and the mouse hit-test assumes the table is the full width.
The grand extras are small once the detail exists.

## Decisions

| | |
|---|---|
| D47 | The dash reads its window into one profile (width class, height class), and every part asks it; there are no scattered thresholds. |
| D48 | Size is a budget spent in priority order, never a cliff: bars, then the key under them, then every table row, then more log lines. |
| D49 | At 160 columns and up, Home is master and detail (`W-Home`): the table at its natural width on the left, the selected instance's vitals and logs on the right. |
| D50 | Columns stop at their content; extra width is spent on information, never on blank space. At 220 and up the table gains a memory trend. |
