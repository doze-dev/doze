---
title: "The app"
description: One window for every stack on this machine, and a bat in the menu bar.
---

Run `doze` in a project folder, or `doze open ~/code/shop`, and the app opens
on that stack; anywhere else it opens on the Library. It is one window for
every stack on the machine and a bat in the menu bar that stays when the
window closes. Quitting it leaves every
stack running: the app is a face on the daemons, not their owner.

From a terminal the prompt comes back at once; the app runs on its own and
writes its log to `~/.doze/desk/app.log`. Over SSH, with no screen, `doze`
is the terminal dash instead, and piped it prints the status table.

## The Library

The first tab, and the one that never closes. Every stack doze knows, running
first, each with a switch: on brings it up as last time, off brings it down,
and the ▾ beside it offers everything or a chosen set. A stopped stack keeps
its page: the same view, every service down and every switch off. **Up as last
time** starts it again; **Choose what starts…** (`c`) opens a sheet to tick the
apps you will work on, showing what starts with them because they need it;
and turning one service on starts the stack with just that one and what it
needs. The Machine line at the bottom says whether the names, the network daemon and any declared domains are
in place; when one is not, **Repair** in Settings puts it there behind the
system's password prompt.

Drop a project folder on the window, or press ⌘O, to add one. A folder with a
`doze.hcl` appears stopped, with Up a press away; opening it never starts it.
A folder with no `doze.hcl` shows what doze found in it and the three ways to
start; each writes the file, and nothing runs until you bring the stack up.

## A stack

Three views of the same services, remembered per stack: **Tiles**, **List**
with a details pane for the selected row, and **Map**, which draws what needs
what. Every row has a switch: on wakes the service, off puts it to sleep,
asking first when something depends on it. Double-click or ⏎ opens it.

Inside a service: how to reach it, with the same URL in Node, Python, Go, a
shell, `.env` and `psql`; why it is awake; memory, CPU and connections over
the last four minutes; its logs by run; and a module's own console when it has
one. A task shows its last run and the runs kept.

A service with a web console of its own (Temporal, mailpit) shows it in its
Console tab, framed, with Open in browser a click away. **Timeline** in a
stack's head, or `T`, draws the last half hour: when each service woke, slept
and crashed, and what happened, newest first.

## When something is wrong

- **A crash** shows what the service said before it exited, the try it is on,
  and Try now or Stop trying.
- **A port already taken** names the process that has it, with its pid, and
  offers to stop it (a polite TERM, only after checking it still holds the
  port) or to change the port in `doze.hcl`.
- **A `doze.hcl` saved broken** is shown with the line and the loader's words;
  the stack keeps running as it was until the file loads again.
- **A `doze.hcl` changed** after the stack came up offers Restart the stack:
  a running stack keeps the file it started with.

## Links

`doze://shop` opens the shop stack, `doze://shop/db` its db service,
`doze://library` and `doze://settings` those pages. Put one in a README or a
runbook; from a terminal, `doze open doze://shop/db`.

## Keys

| Key | Does |
| --- | --- |
| ⌘K | search a service or a stack, or type a verb then a name: `sleep db`, `logs api` |
| esc | up a level: a service or the logs to its stack, a stack to the Library |
| ⌘[ ⌘] | back and forward, the way you came |
| ⌘0, ⌘1–9 | the Library, a stack's tab |
| ⌘O | open a folder |
| ⌘L | all logs of the stack |
| ⌘, | Settings |
| ? | every key, and what the words mean |

Every key is a command too: what the app does, `doze sleep db` or
`doze logs api` does from a script.

## Settings

**Mode** (System, Light or Dark; System follows macOS as it changes) and a
**Colour theme** (the terminal's five: violet, teal, burgundy, amber, slate,
each drawn light or dark as the mode says), **Open at login, in the
menu bar only**, the machine's setup part by part with the command that fixes
each and **Repair**, which runs that setup behind the system's own password
prompt (on Linux, polkit's), and the version. The Library's Machine line
offers the same as **Set up** the first time something is missing. Prefs live in
`~/.doze/desk/prefs.json`.
