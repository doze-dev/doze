package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/doze-dev/doze-sdk/binaries"
)

// fetchCmd is the hidden helper the daemon re-execs to download a service's
// archive. The daemon is meant to sit at rest in about thirty megabytes, and
// the first HTTPS fetch in a process costs it six to ten more that never come
// back (the system certificate store, the TLS and HTTP stacks). A short-lived
// child pays that and exits, which is also how the modules do it.
func fetchCmd() *cobra.Command {
	var home, url, sha string
	cmd := &cobra.Command{
		Use:    "__fetch",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			m := binaries.NewManager(home)
			m.Logf = func(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...) }
			dir, digest, err := m.EnsureDownload(context.Background(), url, sha)
			if err != nil {
				return err
			}
			return json.NewEncoder(os.Stdout).Encode(map[string]string{"dir": dir, "digest": digest})
		},
	}
	cmd.Flags().StringVar(&home, "home", "", "doze home (the cache)")
	cmd.Flags().StringVar(&url, "url", "", "what to fetch")
	cmd.Flags().StringVar(&sha, "sha", "", "the digest the lock pins, if any")
	return cmd
}
