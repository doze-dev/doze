---
title: "The dashboard"
description: doze dash — a live view of your whole stack, right in the terminal.
---

`doze status` is a snapshot. Sometimes you want the movie: to watch a service
wake up, see what is holding it, read its output as it prints. That's the
dash.

```sh
doze dash      # or just: doze
```

It opens a full-screen terminal view that follows the daemon as things change.
If the daemon isn't running, `dash` starts it for you. It needs a real
terminal; without one, use `doze status`.

## Home

Your stack, grouped by what each thing is: **apps** (your code), **modules**
(databases, caches, queues), **services** (commands from outside the registry)
and **tasks**. Each row has a glyph and a word for its state, the address a client
connects to (the name and port where there is a name), how many connections it
has, its memory as a small bar and a figure, its CPU, and a note that says why.
The header counts what is awake, dozing and asleep, the memory in total, how
long things sleep for, and whether names are on:

| | Meaning |
|---|---|
| `● awake` | Running. The note says what holds it: `held by api, worker`. |
| `○ dozing` | Running with nothing using it, counting down: `sleeps in 3m12s`. |
| `◌ waking` | Starting. |
| `· asleep` | Costing nothing. The next connection wakes it. |
| `↻ crashing 2/5` | Exited by itself and is being restarted; the note says when. |
| `✕ failed` | Out of restarts, or a start that did not complete. |
| `✓ done` | A task that ran, with when, how long and why. |

Under the table, **Recent** lists what just happened: what woke, what went to
sleep, what crashed, with the time and why.

On a wide terminal the right-hand pane is a card for the selected row: how to
reach it (name, address, connection string, the environment variable it fills),
what it costs now (memory, CPU, connections, how long it has been up, when it
sleeps), how a task last ran, why it is awake, and its last lines. On a narrow
one (80 columns) the list stands alone, losing columns from the right as it
shrinks, and `enter` opens the row.

| Key | Does |
|---|---|
| `↑↓` or `j k` | Select |
| `enter` | Open the row |
| `w` `s` `r` `p` | Wake, sleep, restart, pin awake |
| `R` | Reset: delete its data (asks you to type its name) |
| `c` | Copy menu: its name, address, connection string, env line |
| `L` | All logs, merged |
| `:` | Palette |
| `T` | Theme |
| `q` | Quit |

Sleeping something that is held asks first, and says who is affected.

## A row

`enter` opens an instance. An app opens on its **logs**; a module on its
**overview** (where to reach it, why it is awake, vitals, what happened to it
lately); a task on its **last run** (result, output, and the runs before it
with the reason each ran). `tab` switches tabs, `esc` goes back.

On the logs tab, lines carry timestamps and a level read from what the program
wrote; errors and warnings are counted in the status line. Scrolling pauses the
view and `f` follows again; `[` and `]` read earlier runs. `v` starts a
selection: `↑↓` extend it, `y` copies it (`Y` without timestamps), `w` saves it
to a file. Copying uses the terminal's own clipboard sequence, so it works over
SSH and inside tmux.

## All logs

`L` merges every instance's output in time order, each source in its own
colour. `/` searches with the matches highlighted, `n` and `N` move between
them, `1`–`9` mute a source, `e` cycles the level filter (all, warnings,
errors).

## Palette and themes

`:` lists what the dash can do to the selected instance, with the command-line
form of each beside it — so what you did with a key, you can put in a script.
`T` previews the five themes (dark and light) on the whole dash as you move;
`enter` keeps one, `esc` puts the old one back, and `p` turns colour off.

The AWS and Kafka consoles that used to be part of the dash are being rebuilt
as module consoles.
