---
title: "The dashboard"
description: doze dash — a live, pokeable view of your whole stack, right in the terminal.
---

`doze status` is a snapshot. Sometimes you want the movie — to watch a service
wake up, see connections come and go, tail logs, or actually reach into a queue
and send a message without leaving the terminal. That's `doze dash`.

```sh
doze dash
```

It opens a live TUI (built with [Charm's Bubble Tea](https://github.com/charmbracelet/bubbletea)
— lovely toolkit, worth a look if you build terminal apps) that reflects the
daemon's real state and updates as things change. If the daemon isn't running,
`dash` starts it for you.

## What you're looking at

The main view is your stack, grouped by category — databases, caches, queues,
storage, your own `process` blocks — each service on a line with its state,
endpoint, connection count, and live resource use. The state is the honest
picture of doze's whole lazy-boot idea, in one column:

- **reaped** — asleep, costing nothing. Its endpoint is still there; the next
  connection wakes it.
- **booting** — someone just connected (or you asked); it's coming up.
- **active** — running, with connections.
- **idle** — running, but nothing's talking to it. In a few minutes it'll reap
  itself. This is the state you'll watch drift to *reaped* after you close your
  app, which is oddly satisfying.
- **tainted** — something went wrong on boot; the details are one keypress away
  so you're never guessing.

Arrow keys move around, `tab` switches focus, and there's a filter if your
stack is big. Select a service and you can wake or sleep it right there, or drop
into its logs.

## The AWS console, built in

Here's the part that's more than a status screen. Select one of the local AWS
engines — S3, SQS, SNS — and the dashboard becomes a little data console for it.
No `aws` CLI, no separate GUI, no hunting for the right endpoint flag.

- **S3** — browse your buckets and objects, and `put` an object straight from
  the dashboard to test an upload path.
- **SQS** — watch queues fill and drain, `send` a message (FIFO-aware, so it'll
  ask for the bits it needs), inspect what's sitting in a queue, and `purge`
  when you want a clean slate. The dead-letter and redrive setup is visible too,
  so you can actually see a message get dead-lettered instead of guessing why it
  vanished.
- **SNS** — see your topics and subscriptions, and `publish` a message with
  attributes so you can watch it fan out to the subscribing queues — filter
  policies and all. It's the fastest way to answer "is my subscription filter
  actually matching?" without writing a throwaway script.

This is the kind of thing that usually means alt-tabbing to a web console or
copy-pasting an `awslocal` incantation. Having it a keystroke away, pointed at
*your* running stack, is a small thing that saves a surprising number of little
context switches.

## Getting around

The footer always shows the keys for wherever you are, so you don't have to
memorize anything — but the shape of it:

- **↑ ↓ / arrows** — move within a list
- **tab** — switch between panels
- **enter** — open / drill into the selected thing
- **esc** — back out
- **/** — filter
- action keys (send, publish, put, purge, wake, sleep…) shown in context
- **?** — help · **q** — quit

## When to reach for it

Honestly? Whenever `status` isn't enough. When you're debugging a message that
should've been delivered and wasn't, when you want to watch a slow first boot
finish, when you're eyeballing memory across the stack, or when you just want to
poke a queue and see what happens. It's not required for anything — every action
in it has a command-line equivalent — but for the exploratory, "what's actually
going on" moments, a live view you can steer beats a snapshot you have to keep
re-running.
