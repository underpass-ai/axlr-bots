package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// printJSON writes v as indented JSON on one stream, with < and > as
// they are (the output is for people and files, not HTML).
func printJSON(w io.Writer, v any) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(v); err != nil {
		fmt.Fprintln(w, "{}")
		return
	}
	_, _ = w.Write(buf.Bytes())
}

// fail reports an error on stderr and returns the error exit code.
func fail(stderr io.Writer, err error) int {
	fmt.Fprintln(stderr, "axlr-bots:", err)
	return exitError
}
