---
title: "A tour of the command line"
description: The handful of commands you'll actually use, grouped by what you're trying to do.
---

There's a full [CLI reference](/reference/cli/) with every flag — but you won't
need most of it most days. So instead of a wall of options, here's the walk
through the commands the way you'll actually reach for them: by what you're
trying to get done. If you've used `docker compose`, a lot of this will feel
like coming home, just with less waiting around.

The whole thing is one binary. No daemon to install and babysit — doze starts
its own in the background the first time you need it, and stops it when you're
done.

## Getting a project going

You've got an empty directory and an idea of what your app needs.

```sh
doze init
```

`init` writes you a starter `doze.hcl`. It'll ask a couple of questions (or take
`--yes` and give you a sensible default), and it pulls the list of available
engines from the live registry, so what it offers is real, not hardcoded. Open
the file, shape it to your app, and you're ready.

Not sure the file is right? Check it before anything touches your machine:

```sh
doze lint
```

`lint` reads your config the same way doze will — same strict schema, same
typo-catching — but boots nothing and writes nothing. It's the fast feedback
loop while you're editing: if `shared_bufers` should've been `shared_buffers`,
you hear about it here, with the line number and a suggestion, not three
commands later.

## The everyday loop

This is the part you'll live in.

```sh
doze up
```

`up` converges your declared structure — creates the databases, roles,
buckets, queues you asked for — and boots everything, in dependency order, in
the background. Then it gets out of your way. Your services are listening on
the ports you declared; point your app at them and go.

Most of the time, though, you won't even type `doze up`. You'll type this:

```sh
doze run -- npm test
doze run -- go test ./...
doze run -- ./your-dev-server
```

`run` makes sure the daemon is up, then runs your command — so your backends
are awake before your code tries to connect, and they drift back to sleep when
your command's done and nothing's talking to them. It's the honest answer to
"I just want my tests to have a real database without thinking about it." (One
deliberate choice: `run` doesn't inject env vars. Your ports are stable because
you declared them, so put the connection strings in your app config where they
belong — or use a [`process` block](/guides/workflows/) and let doze wire the
URLs for you.)

When you want to actually *look* at a service:

```sh
doze status          # everything, at a glance
doze shell app       # a real psql into "app" — booting it if it was asleep
doze logs -f app     # follow a service's logs
```

`shell` opens the right client for the engine — `psql` for Postgres,
`redis-cli` for Valkey, `mongosh` for the Mongo-compatible one — and if the
service was cold, it wakes it for you first. `status` is the one you'll glance
at most; there's a live, richer version of it in the
[dashboard](/cli/dashboard/).

Done for the day, or switching projects?

```sh
doze down
```

Everything sleeps, the daemon stops, your fans were never spinning in the first
place.

## When you change the config

You edited `doze.hcl` — added a role, a new bucket, bumped a version. You don't
need to tear anything down:

```sh
doze sync
```

`sync` reconciles what's running with what you've now declared: creates what's
new, updates what changed, prunes what you removed. It's convergence on demand.
(`doze up` does this too as part of booting; `sync` is for when the stack's
already up and you just changed the shape.)

Two more for the moments you need them, one gentle and one not:

```sh
doze wake cache      # boot one service now (and anything it depends on)
doze sleep cache     # reap one service now
```

```sh
doze reset app       # wipe a service's data and start clean
```

`reset` is the "let me start over" button — it drops the data directory and
re-provisions fresh. doze converges *structure*, never your data, so this is
how you deliberately throw the data away when a migration went sideways and you
want a clean slate. It asks first.

## When something's off

Rare, but it happens — a port's taken, a cache got into a weird state:

```sh
doze doctor
```

`doctor` looks over your environment and config and tells you what it finds —
in plain language, with the fix, not a stack trace. It's the first thing to run
when a thing that should work doesn't.

## Managing engines and modules

Two quieter corners you'll visit occasionally. Engines come from real upstream
binaries; the plugins that provide them ("modules") come from a signed
registry. Both are covered in depth elsewhere — [modules for
users](/guides/modules/), [managing binaries](/reference/binaries/) — but the
commands live here:

```sh
# discovery lives on the registry — doze.nerdmenot.in/registry — engine
# versions, platforms, and the config reference, generated from each module
doze modules upgrade --check # anything newer worth pulling? (CI-friendly)
doze binaries available postgres   # which engine versions exist
```

## That's really it

Nine or ten commands cover essentially everything: `init`, `lint`, `up`/`run`,
`status`/`shell`/`logs`, `sync`, `down`, and `reset` when you mean it. The
[reference](/reference/cli/) has the rest for when you go looking — but if you
learned only the ones on this page, you'd be fine indefinitely.

Next: the [live dashboard](/cli/dashboard/), which turns `status` into
something you can actually poke at.
