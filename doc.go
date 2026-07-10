/*
Package doze is the embeddable Go API for doze — real local backing services
(Postgres, MariaDB, Valkey, Kafka, S3/SQS/SNS, …) without Docker, booted lazily
and reaped when idle.

It is a library you call, not a CLI you shell out to. In Serve mode it drives the
daemon in-process with native Go types and errors; in Attach mode it speaks to a
background daemon over its control socket. Same surface either way. You can build
your own tooling — a custom config format, an internal platform, your own UI — on
top of it.

# Entry points

Attach connects to the background daemon for a config, spawning it if needed.
Your program is a client; the stack outlives it until you Shutdown.

	sess, err := doze.Attach(ctx, doze.Options{ConfigPath: "doze.hcl"})

Serve runs the daemon inside your process (no separate process; Close/Shutdown
stops it) and talks to it directly — no socket round-trip.

	sess, err := doze.Serve(ctx, doze.Options{Stack: myStack})

# Config-less: bring your own format

You don't need an HCL file. Build the topology in Go — map it from your own YAML,
a database, an API — and hand it to Serve/Attach:

	sb := doze.NewStack("shop").Domains(true)
	sb.AddProcess("worker", doze.Process{Command: "python worker.py"})
	sb.AddModule("postgres", "db").Version("16").Body(`databases = ["app"]`)
	sess, _ := doze.Serve(ctx, doze.Options{Stack: sb})

AddProcess is typed (process is a first-class engine); AddModule takes engine
config as HCL, because module engines decode out-of-process. Stack.HCL() renders
the equivalent file. LoadStack(path) parses an existing config back into an
editable Stack (read → mutate → re-render), and EngineSchema(opts, engine)
discovers what an AddModule accepts.

# Reasoning without running (static plane)

Load(opts) parses + validates a config (or Stack) with no daemon — a returned
error is the lint result; the Inspection exposes Topology() and Plan() (the
diff against last-applied state). Session.Sync reconciles (converge + prune).
Modules(opts) lists module pins and upgrades them.

# Driving and reading a live stack

Lifecycle: Up, Boot, Down, Restart, Apply, Destroy, Reset, plus live
AddProcess/AddModule/Remove to mutate a running stack with no restart.

Introspection for building a UI:
  - Topology() — the declared graph as []Node, without the daemon running.
  - Status()/Instance() — live state incl. PID, RAM, CPU, endpoint.
  - Resources()/Admin() — a service's sub-resources (buckets/queues/…) and the
    data actions its engine offers.
  - Endpoints()/Env() — connection strings and env vars.
  - Logs()/FollowLogs() — buffered and streaming logs.
  - Events() — the live state-transition feed; this is also the progress signal,
    e.g. watch it while Up runs to see each service go booting → active → healthy.

Failures come back as typed sentinels — ErrNotFound, ErrAlreadyExists,
ErrPortConflict, ErrBootFailed, ErrUnsupported — so you branch with errors.Is,
not string matching.

# Stability

Experimental (v0): the surface may change. See examples/basic for a runnable
walkthrough and examples/dash for a minimal live dashboard built on this package.
*/
package doze
