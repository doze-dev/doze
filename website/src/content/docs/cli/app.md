---
title: "The app"
description: One window for every stack on this machine, and a bat in the menu bar.
---

Run `doze` in a project folder, or `doze open ~/code/shop`, and the app opens
on that stack. It is one window for every stack on the machine and a bat in
the menu bar that stays when the window closes. Quitting it leaves every
stack running: the app is a face on the daemons, not their owner.

From a terminal the prompt comes back at once; the app runs on its own and
writes its log to `~/.doze/desk/app.log`. Over SSH, with no screen, `doze`
is the terminal dash instead, and piped it prints the status table.

## The Library

The first tab, and the one that never closes. Every stack doze knows, running
first, each with a switch: on brings it up as last time, off brings it down,
and the ▾ beside it offers everything or a chosen set. A stopped stack opens
on what its `doze.hcl` declares, ticked as last time. The Machine line at the
bottom says whether the names, the network daemon and any declared domains are
in place; when one is not, **Repair** in Settings puts it there behind the
system's password prompt.

Drop a project folder on the window, or press ⌘O, to add one. A folder with no
`doze.hcl` shows what doze found in it and the three ways to start, and
nothing runs until you choose.

## A stack

Three views of the same services, remembered per stack: **Tiles**, **List**
with a details pane for the selected row, and **Map**, which draws what needs
what. Every row has a switch: on wakes the service, off puts it to sleep,
asking first when something depends on it. Double-click or ⏎ opens it.

Inside a service: how to reach it, with the same URL in Node, Python, Go, a
shell, `.env` and `psql`; why it is awake; memory, CPU and connections over
the last four minutes; its logs by run; and a module's own console when it has
one. A task shows its last run and the runs kept.

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

Six themes (the terminal's five and a light one), the machine's setup part by
part with the command that fixes each, and the version. Prefs live in
`~/.doze/desk/prefs.json`.
