/*
Package doze is the embeddable Go API for doze — real local backing services
(Postgres, MariaDB, Valkey, Kafka, S3/SQS/SNS, …) without Docker, booted lazily
and reaped when idle.

It is a thin, stable facade over the same daemon and control socket the `doze`
CLI drives, so embedding a stack and running it from the terminal are the same
machinery. The public types here (Options, Session, Instance) are deliberately
independent of doze's internal wire types.

# Two entry points

Attach connects to the background daemon for a config, spawning it if needed —
the CLI's model. Your program is a client; the stack outlives it until you
Shutdown.

	sess, err := doze.Attach(ctx, doze.Options{ConfigPath: "doze.hcl"})
	if err != nil { ... }
	defer sess.Close()
	if err := sess.Up(ctx); err != nil { ... }        // converge + wake, in dep order
	env, _ := sess.Env(ctx)                            // DATABASE_URL=…, KAFKA_BROKERS=…

Serve runs the daemon inside your process instead — no separate background
process; Close or Shutdown stops it. Useful for tests and self-contained tools.

	sess, err := doze.Serve(ctx, doze.Options{ConfigPath: "doze.hcl"})

# Stability

Experimental (v0): the surface may change. See examples/basic for a runnable
walkthrough.
*/
package doze
