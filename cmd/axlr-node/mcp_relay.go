package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"time"
)

// mcpRelay is `axlr-node mcp-relay --socket <path>`: the stdio MCP server
// AXLR starts for an execution of a bot that may delegate. It is a pipe:
// its stdin goes to the agent's socket for the execution and what the
// agent answers comes out on its stdout. It ends when either side
// closes.
func mcpRelay(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("axlr-node mcp-relay", flag.ContinueOnError)
	flags.SetOutput(stderr)
	socket := flags.String("socket", "", "the agent's socket for the execution")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if *socket == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "axlr-node mcp-relay: --socket names the agent's socket, and nothing else is taken")
		return exitUsage
	}
	conn, err := net.DialTimeout("unix", *socket, 5*time.Second)
	if err != nil {
		fmt.Fprintln(stderr, "axlr-node mcp-relay:", err)
		return exitError
	}
	defer conn.Close()
	go func() {
		_, _ = io.Copy(conn, stdin)
		// The client closed its side: let the agent see the end and
		// close its own once it has answered.
		if unix, ok := conn.(*net.UnixConn); ok {
			_ = unix.CloseWrite()
		}
	}()
	_, _ = io.Copy(stdout, conn)
	return exitOK
}

// stdinOf is the process's stdin, a variable so the tests can replace it.
var stdinOf io.Reader = os.Stdin
