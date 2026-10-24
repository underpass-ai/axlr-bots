package main

import (
	"fmt"
	"io"
)

func install(args []string, stdout, stderr io.Writer) int {
	fmt.Fprintln(stderr, "underpass-bots: install is not available yet")
	return exitError
}

func uninstall(args []string, stdout, stderr io.Writer) int {
	fmt.Fprintln(stderr, "underpass-bots: uninstall is not available yet")
	return exitError
}
