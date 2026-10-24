package main

import (
	"context"
	"fmt"
	"io"

	"github.com/underpass-ai/underpass-bots/adapters/unixapi"
)

func executionGet(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "underpass-bots: execution get needs exactly one <execution-id>")
		return exitUsage
	}
	execution, err := service.GetExecution(ctx, args[0])
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, execution)
	return exitOK
}
