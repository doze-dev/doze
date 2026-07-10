// Command basic is a minimal example of embedding doze: load a config, bring
// the stack up, print each service's endpoint, stream a few events, and shut
// down. Run it from a directory with a doze.hcl (or pass -config).
//
//	go run ./examples/basic -config ./doze.hcl
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	doze "github.com/doze-dev/doze"
)

func main() {
	cfgPath := flag.String("config", "", "path to doze.hcl (empty: search upward)")
	flag.Parse()

	ctx := context.Background()

	// Attach to (or spawn) the background daemon for this config.
	sess, err := doze.Attach(ctx, doze.Options{
		ConfigPath: *cfgPath,
		Logf:       func(f string, a ...any) { fmt.Fprintf(os.Stderr, "doze: "+f+"\n", a...) },
	})
	if err != nil {
		log.Fatal(err)
	}
	defer sess.Close()

	fmt.Printf("stack %q: %v\n", sess.StackName(), sess.Services())

	// Bring every enabled service up (converge + wake, in dependency order).
	upCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := sess.Up(upCtx); err != nil {
		log.Fatal(err)
	}

	// Print the connection env vars — the same pairs `doze env` emits.
	env, err := sess.Env(ctx)
	if err != nil {
		log.Fatal(err)
	}
	for k, v := range env {
		fmt.Printf("export %s=%s\n", k, v)
	}

	// Watch state transitions for 5 seconds.
	watchCtx, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	_ = sess.Events(watchCtx, func(in doze.Instance) {
		fmt.Printf("event: %s -> %s\n", in.Name, in.State)
	})

	// Stop the stack. (Drop this to leave it supervised in the background.)
	if err := sess.Down(ctx, ""); err != nil {
		log.Fatal(err)
	}
	fmt.Println("stack down")
}
