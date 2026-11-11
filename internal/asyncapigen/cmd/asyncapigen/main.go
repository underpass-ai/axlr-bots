// Command asyncapigen writes the Go DTOs of an AsyncAPI document into a
// package directory: go generate runs it (see adapters/bus/domainevent).
//
//	asyncapigen -spec <file> -package <name> -out <dir>
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/underpass-ai/axlr-bots/internal/asyncapigen"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string, out io.Writer) error {
	flags := flag.NewFlagSet("asyncapigen", flag.ContinueOnError)
	flags.SetOutput(out)
	spec := flags.String("spec", "", "the AsyncAPI document (JSON)")
	pkg := flags.String("package", "", "the Go package whose x-go-type schemas to write")
	dir := flags.String("out", ".", "the package directory")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *spec == "" || *pkg == "" {
		return fmt.Errorf("asyncapigen: -spec and -package are required")
	}
	raw, err := os.ReadFile(*spec)
	if err != nil {
		return err
	}
	files, err := asyncapigen.Generate(raw, filepath.ToSlash(filepath.Base(*spec)), *pkg)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(*dir, name), files[name], 0o644); err != nil {
			return err
		}
		fmt.Fprintln(out, "asyncapigen: wrote", name)
	}
	return nil
}
