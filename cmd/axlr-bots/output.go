package main

import (
	"encoding/json"
	"fmt"
	"io"
)

// printJSON writes v as indented JSON on one stream.
func printJSON(w io.Writer, v any) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Fprintln(w, "{}")
		return
	}
	fmt.Fprintln(w, string(data))
}

// fail reports an error on stderr and returns the error exit code.
func fail(stderr io.Writer, err error) int {
	fmt.Fprintln(stderr, "axlr-bots:", err)
	return exitError
}
