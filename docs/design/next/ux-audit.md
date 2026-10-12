# The dash: a UX audit, with the worst cases

Written 2026-10-12, against `doze` main at the commit that adds this file. It
audits the terminal dash (`internal/dash`) as a person would meet it, and asks
of each part what happens on the worst day, not the demo.

Everything here is marked **measured** (I ran it and saw it), **read** (I read the
code and it follows), or **inferred** (a risk I could not reproduce from here).
A finding says what is wrong, what the worst case is, and what to do. Nothing in
this file has been fixed yet; it is the list to fix from.

## How it was done

- **A stress suite** (`internal/dash/stress_test.go`): every page of seven stacks
  nobody drew (none, one, eighty-character names, 60 and 200 instances, unicode
  names, no logs) at eight window sizes from 200×60 to 24×8, with hostile log
  lines. Plus 250 random keys, clicks, wheel and resizes per stack.
- **A real daemon**, isolated, killed under the dash.
- **Contrast computed** from the palette against WCAG 2.1.
- **Render cost timed** at 10 to 500 instances.
- **East Asian width** checked for every glyph the dash draws.
- **Guidance read**: [Command Line Interface Guidelines](https://clig.dev),
  [WCAG 2.1 SC 1.4.11 Non-text Contrast](https://www.w3.org/WAI/WCAG21/Understanding/non-text-contrast.html)
  (and 1.4.3), and Nielsen's usability heuristics as summarised in the search
  results, which I did not read at source.

## What holds

- No panic, no line wider than its window, no more lines than it has, across the
  suite and the fuzz. (measured)
- Log text from programs is made plain on arrival: escape sequences, window-title
  OSC, bells, carriage returns, tabs, control bytes, invalid UTF-8. (measured, and
  it was a real bug before)
- Colour is never the only signal: states have a word and a glyph, failures have
  `✗`. (read)
- Render cost is not a problem: Home 2–5 ms at 200 instances and 10 ms at 500; a
  focus page or the stack logs 3–5 ms. (measured)
- The destructive end of the range is guarded well: reset asks you to type the
  name. (read)

## Critical

### A1. A dead daemon looks alive. *(measured)*
Start a stack, open the dash, run `doze down`. Fifteen seconds later the dash
still says `worker  awake  2 MB`, with the log tail frozen and `following` in the
border. Nothing says the picture is old.

Why: `Run` subscribes in a goroutine and discards the error
(`_ = d.Subscribe(...)`, twice); a failed snapshot is ignored (`if msg.err == nil`);
there is no reconnect. A daemon that restarts, or a socket that drops, leaves the
dash showing the last frame forever.

Worst case: someone watches a green screen while their database is down, or
presses `w` on something they believe is awake.

Do: a connection state on the model (`live`, `reconnecting`, `gone`) set by the
subscription ending; reconnect with backoff; while not live, dim the page and put
`daemon not responding · retrying in 4s` where the resident total is, and refuse
verbs with a clear message. Add "last update 12s ago" once nothing has arrived for
more than three. Violates Nielsen's visibility of system status.

### A2. `r` restarts a database with 38 clients, no question. *(read)*
`s` asks when something is using the instance; `r`, one key to its right, does
not. A restart of a module drops every connection.

Do: apply the same rule to restart (ask when held, say how many), and name what
will be dropped. Error prevention.

### A3. "copied 12 lines" is said whether or not it happened. *(read)*
`y` uses OSC 52. Terminal.app, tmux without `set-clipboard`/passthrough, and some
ssh setups ignore it, and the dash cannot know. The notice is written before
anything is learned.

Do: say what was done, not what happened: `sent 12 lines to the clipboard`, and
when `$TMUX` is set or the terminal is not one known to support OSC 52, add `·
if nothing pasted, doze logs worker`. Better, offer `Y` to write the lines to a
file and say where.

## High

### B1. Before the first snapshot, Home says the stack is empty. *(read, introduced by the empty-state text)*
The model starts with no instances, so for the first frames, or for as long as a
slow daemon takes, the page says "Nothing is declared in this stack yet. doze add
postgres". On a big stack that is a lie, and it will be seen on every start.

Do: a `connecting` state until the first snapshot lands (spinner and the stack
name from the config path); only then the empty text.

### B2. Failures last until the next key. *(read)*
A failed wake or restart sets a notice that the next keypress clears. A long error
is cut at the window's edge. There is no way to see it again.

Do: keep the last message with its age until `esc` or ten seconds; show failures
in red; make `:` list "recent messages". Long errors open in the whole-line box.

### B3. A label that promises one thing and does another. *(read)*
The crash strip says `l  full logs`. On Home `l` opens the **stack's** logs, merged,
not the crashing instance's.

Do: make `l` on Home open the selected instance's logs tab, and give the stack's
logs another key (`L`), or relabel. The other `l`s in the dash mean "logs of this".

### B4. There is no help, and the key bar loses keys when narrow. *(read)*
The board has `?`. The key bar drops hints by priority on narrow windows, so
`t` tree, `o` sort, `v` select, `g`/`G`, `[`/`]` and the wheel are findable only by
accident. The palette lists commands, not keys.

Do: `?` opens an overlay generated from the same table the key bar is built from,
so the two cannot disagree. Recognition over recall.

### B5. Two palette colours fail contrast. *(measured)*

| text on | ratio | rule |
|---|---|---|
| dim on the Home selected row (`#8a85a8` on `#3a3170`) | **3.24** | 4.5 (1.4.3) |
| dim on a selected pill (`#2b2542`) | **4.15** | 4.5 |
| box borders on the page (`#5f5886` on `#15131d`) | **2.82** | 3.0 (1.4.11) |
| accent on the selected row | 4.70 | passes |
| everything else | 5 to 16 | passes |

The Home selected row is the row you are acting on, and its BECAUSE and RUNS text
are dim on it. The dim colour was tuned against the page, not against the
selection.

Do: on the selected row use the normal text colour for those cells; lift the
border to about `#6b6396` (3.1). The modal backdrop is meant to be faint (1.6) and
is not read.

### B6. Every box and status glyph is East Asian Ambiguous. *(measured)*
`╭ ─ │ ● ◐ · … ↑ ↓ █ ▇` are width 1 in a normal terminal and width 2 where the
terminal treats ambiguous characters as wide (an iTerm2 option, and a default in
some CJK locales). There, every border and column shifts by one per glyph and the
whole screen tears. `◌ ✓ ✗ ▸` are fine.

Do: detect (`LC_CTYPE`, or a query) and fall back to a set of narrow characters,
and offer `DOZE_ASCII=1` for a plain `+-|` and `*o.` set. It is also the answer to
B7.

### B7. Non-UTF-8 and `TERM=dumb`. *(inferred)*
`LANG=C` over ssh, `TERM=linux` on a console and `TERM=dumb` give box-drawing
garbage; clig.dev's rule is that colour is off for `dumb` and `NO_COLOR`. The dash
honours `NO_COLOR`; it does not look at `TERM` or the locale.

Do: the same ASCII set as B6, chosen when the locale is not UTF-8 or `TERM` is
`dumb`/`linux`.

## Medium

### C1. Too small to use is not said. *(measured)*
Below about 40×12 the frame is valid and there is nothing in it to use.

Do: under 60×16, one centred line: `make the window larger · 60×16 or more`.

### C2. The mouse takes selection away. *(read)*
Capturing the mouse means the terminal's own drag-select stops working (it is
shift-drag), the wheel scrolls the logs, and none of that is said.

Do: say it once in the key bar on the first mouse event: `shift-drag selects text`.

### C3. The meters are estimates and read as facts. *(read, one measured)*
`CPU%` is `ps`'s decayed average, so a one-second burst reads 0.0 (seen in a live
run). `resident` sums RSS, which counts shared pages more than once. `of ~X` is
the sum of each instance's highest reading seen, not a ceiling.

Do: label the header meters `≈`, and say what `of ~X` is in the `?` overlay.

### C4. The top of the logs ends without saying so. *(read)*
The dash holds 400 lines per instance and scrolling stops at the oldest, with no
mark. A person scrolling for a line from an hour ago concludes it was never
printed.

Do: at the top, a dim line: `── 400 kept · older: doze logs worker ──`.

### C5. Rows move under the cursor when sorted by memory. *(read)*
Sorting by `MEM` re-sorts every second as numbers change. The selection follows
the name, so the highlighted row travels, and a click or a keypress lands on a
different row from the one that was seen.

Do: hold the order for a few seconds after any key or click; re-sort on a timer,
not each frame. htop has the same problem and people use it anyway, but htop's
selection is a PID cursor, not a mouse target.

### C6. Quitting says nothing about what keeps running. *(read)*
`q` leaves the dash; the stack keeps running. A newcomer will assume it stopped.

Do: print one line on exit: `stack still running · doze down to stop it`.

### C7. An instance added while the dash is open has no logs. *(read)*
The log subscription names the instances known when it opened.

Do: resubscribe, for the new names only, when the instances frame brings one.

### C8. Memory and start-up under a very large stack. *(inferred, not measured)*
400 lines each for 500 instances is 200,000 lines held, and the dash asks the
daemon for all of them at once at start. Render cost was measured and is fine;
resident size and the start-up burst were not.

Do: measure, and if it matters, ask for 100 lines at the start and fetch the rest
for an instance when it is first looked at.

## Low

- **Time zone.** Log times are in the local zone with no zone shown; on a remote
  stack in another zone the clock misleads. (read)
- **Screen readers.** A full-screen TUI in the alternate screen is not usable with
  one. `doze status` is the accessible path; say so in the docs. (inferred)
- **Very fast logging.** Thousands of lines a second were not tested; each line is
  an update and a render is a few milliseconds, so it will be bounded by the
  renderer's frame rate, but that is a guess. (inferred)
- **`o` and `t` mean different things on different pages** (sort / open console;
  tree / time). Each is local to its page and shown in the key bar, which is
  tolerable, and is exactly what `?` is for. (read)

## Order to fix

1. A1, B1, A2, A3: the screen must not lie about the daemon, about being empty, or
   about having copied; a single key must not drop clients.
2. B3, B2, B4: what a label says and what happens; messages you can read; help.
3. B5, C1, C6, C4, C2: contrast, too-small, exit message, top-of-logs, mouse hint.
4. B6, B7: ASCII and narrow-glyph fallback.
5. C3, C5, C7, C8: honest meters, stable sort, new instances, scale.
