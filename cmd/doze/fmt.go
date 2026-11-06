package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	"github.com/spf13/cobra"

	"github.com/doze-dev/doze/internal/config"
	"github.com/doze-dev/doze/internal/fault"
)

func fmtCmd() *cobra.Command {
	var check bool
	cmd := &cobra.Command{
		Use:   "fmt",
		Short: "Format doze.hcl and its sibling *.doze.hcl in place",
		Long: "fmt rewrites doze.hcl, and every *.doze.hcl beside it, in HCL's canonical\n" +
			"layout: indentation, aligned `=`, spacing. It changes layout only, never\n" +
			"meaning, and leaves a file that does not parse untouched.\n\n" +
			"With --check it writes nothing: it lists the files that would change and\n" +
			"exits 1 if there are any, for CI and pre-commit hooks.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			files, err := config.Files(configPath)
			if err != nil {
				if os.IsNotExist(err) {
					return noConfigError()
				}
				return err
			}
			return formatFiles(cmd.OutOrStdout(), files, check)
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "write nothing; list the files that would change and exit 1 if any would")
	return cmd
}

// formatFiles formats each file in place, or with check only reports the ones
// that would change. Every file is parsed first: one that does not parse is an
// error, and nothing is written.
func formatFiles(w io.Writer, files []string, check bool) error {
	type change struct {
		path string
		out  []byte
		mode os.FileMode
	}
	var changes []change
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			return err
		}
		if _, diags := hclsyntax.ParseConfig(src, f, hcl.InitialPos); diags.HasErrors() {
			return fault.With(fault.Invalid, fmt.Errorf("%s does not parse, so it was not formatted: %w", f, diags))
		}
		out := hclwrite.Format(src)
		if bytes.Equal(src, out) {
			continue
		}
		mode := os.FileMode(0o644)
		if fi, err := os.Stat(f); err == nil {
			mode = fi.Mode().Perm()
		}
		changes = append(changes, change{path: f, out: out, mode: mode})
	}
	for _, c := range changes {
		_, _ = fmt.Fprintln(w, displayPath(c.path))
		if check {
			continue
		}
		if err := os.WriteFile(c.path, c.out, c.mode); err != nil {
			return err
		}
	}
	if check && len(changes) > 0 {
		return shown(errors.New("not formatted"))
	}
	return nil
}

// displayPath is path relative to the working directory when it is under it.
func displayPath(path string) string {
	wd, err := os.Getwd()
	if err != nil {
		return path
	}
	if rel, err := filepath.Rel(wd, path); err == nil && filepath.IsLocal(rel) {
		return rel
	}
	return path
}
