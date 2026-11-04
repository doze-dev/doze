// Command gen writes the app's tokens.css from internal/brand.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/doze-dev/doze/internal/brand"
)

func main() {
	out := flag.String("o", "", "the file to write (stdout when empty)")
	flag.Parse()
	css := brand.CSS()
	if *out == "" {
		fmt.Print(css)
		return
	}
	if err := os.WriteFile(*out, []byte(css), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
