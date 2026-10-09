# Contract 2: daemon state, events and logs

Tier: core. Built in phase 2. Background in
[gap-analysis.md](gap-analysis.md#daemon-and-control-protocol).

Every screen reads from this contract. It is the most important one to get
right, and the cheapest to prove: the CLI is rebuilt on it before the dash is.

## Constraint: stay weightless

"At rest, your whole backend is one ~15 MB daemon" is the product. Nothing is
measured today. So:

- The daemon does no periodic work unless a client is subscribed.
- Logs live in files, not in memory.
- CPU and memory sampling runs only while someone is watching.
- Phase 2 adds the measurement before it adds anything else.

## States

| State | Today | Shown as | Meaning |
|---|---|---|---|
| `asleep` | `reaped` | asleep | Nothing running. |
| `waking` | `booting` | waking | Starting, or waiting on a dependency. |
| `awake` | `active` | awake | Running with at least one holder. |
| `dozing` | `idle` | dozing | Running, no holders, counting down to sleep. |
| `crashing` | (none) | crashing | Exited unexpectedly; a restart is scheduled. |
| `failed` | (none) | failed | Gave up restarting, or a task or setup step failed. |
| `done` | (none) | done | A task that exited 0. |
| `disabled` | a string in the handler | disabled | `enabled = false`. |

Fixes that come with the table:

- An app with `sleep = "never"` is `awake`, not `dozing` forever.
- Restart count and pin changes emit events. Today they don't.

## Why something is awake

A holder is anything keeping an instance out of the reaper:

| Holder | Known today? |
|---|---|
| A dependant that references it and is running | Yes. `Runtime.bootDeps` acquires each dependency. |
| A pin (`doze pin`) | Yes, in memory only. |
| An open connection through the proxy | Count only. |
| A named connection (which app opened it) | No. Dream tier; see [networking.md](networking.md#caller-identity). |

The status snapshot gains `Holders []Holder{Kind, Name}` and keeps `Conns`.
The canvas wording becomes "4 connections · held by api, migrate", which is
true with today's data.

## The journal

A structured record of what the daemon did. It replaces the free-text lines in
`run/doze.log` as the source for the dash.

```go
type Event struct {
    Seq      uint64
    Time     time.Time
    Kind     string        // wake, ready, sleep, crash, restart, give_up,
                           // step, task_start, task_end, config_changed
    Instance string
    Cause    Cause         // {Kind: connection|dependency|command|restart|watch|inputs, By: "worker"}
    Took     time.Duration // for ready, step, task_end
    Detail   string        // exit code, step name, error text
}
```

- Stored as JSON lines in `<ProjectDir>/run/journal.jsonl`, capped by size and
  rotated once.
- `ready` carries the time since its `wake`. That is the "woke in 38 ms"
  number, measured in the daemon.
- A `wake` caused by a dependant names the dependant. That is the wake trace,
  as far as declared references go.
- The `zzz` lines in the dash are journal events merged into the log view by
  time.

## Logs

One store per instance, on disk.

- Path: `<ProjectDir>/logs/<instance>/<run>.log`. A run starts at each spawn.
- Each line records a timestamp, the stream (stdout or stderr) and the bytes.
- Step output (`setup`, `on_ready`, task runs) is logged the same way with a
  step tag. Today hook output is dropped unless it fails.
- Kept: the last N runs and a size cap per instance, both set in `defaults`.
- Read by cursor: `(run, offset)`. A reader can ask for the tail, a range, or
  to follow.
- Logs outlive the process. Reading the logs of a sleeping or crashed instance
  works. Today it is an error.
- Follow is a push from the writer, not a 250 ms poll.

Level detection (`INF`, `WRN`, `ERR`) and ANSI handling are the client's job.
The daemon stores bytes.

## Protocol

Still newline-delimited JSON on the unix socket. Changes:

1. **Handshake.** The first frame is `{"hello": {"proto": 2, "client":
   "dash"}}`. A mismatch gets a clear error instead of a decode failure.
2. **One long-lived connection** may carry several requests, each with an id.
   Today it is one request per connection.
3. **`subscribe`** replaces polling. The client names what it wants:

| Topic | Frames | Notes |
|---|---|---|
| `instances` | a full snapshot, then changes | Extends today's `events`. Not lossy: a slow client gets a fresh snapshot instead of dropped frames. |
| `stats` | CPU, memory, connection count per instance, on an interval | Sampling runs only while someone is subscribed. |
| `journal` | events from a given `Seq` | |
| `logs` | lines from a cursor, for chosen instances | |
| `console` | panel data for one instance | See [consoles.md](consoles.md). |

4. **Verbs** keep their meaning and get consistent names: `up`, `down`,
   `wake`, `sleep`, `restart`, `reset`, `pin`, `sync`, `task`, `add`,
   `remove`.

## The client seam

The dash and the CLI depend on an interface, not on `*control.Client`:

```go
type Daemon interface {
    Snapshot(ctx) (Snapshot, error)
    Subscribe(ctx, topics ...Topic) (<-chan Frame, error)
    Do(ctx, verb Verb) (Result, error)
    Logs(ctx, q LogQuery) (LogReader, error)
}
```

Two implementations: the socket client, and `fakedaemon`, which replays a
scripted sequence of frames. The fake is what makes golden screens possible.

## CLI on the new API

Phase 2 proves the contract by rebuilding the non-interactive commands on it:

| Command | Change |
|---|---|
| `doze` | Opens the dash. Prints the status table when not on a terminal. |
| `doze up [name…]` | Progress from the `instances` and `journal` topics, one line per step. |
| `doze status` | The table from the canvas, plus `--json` and `-w`. |
| `doze logs <name> [-f] [--run N] [--since T]` | New as a command. |
| `doze wake`, `sleep`, `restart`, `reset`, `pin` | New as commands. They exist only in the dash today. |
| `doze env`, `run` | Unchanged in meaning. `run <app> -- cmd` uses that app's env. |

Every dash action must have a CLI form. `cmd/doze/parity_test.go` already
checks the action registry against the CLI; it extends to enforce this.

## Measures

| Measure | How | Budget |
|---|---|---|
| Idle daemon RSS, nothing subscribed | A test that starts the daemon on the fixture stack and reads RSS after settling | Set from the first measurement, then enforced in CI |
| Wake latency per engine | `ready.Took` from the journal, on the fixture stack | Recorded per run; a regression threshold, not an absolute |
| Cost of a subscribed dash | Daemon CPU with one `instances` and one `stats` subscriber | Recorded; compared against today's 500 ms `ps` polling |
| Log store memory | Daemon RSS while a service writes 100k lines | Flat |
