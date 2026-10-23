package main

import (
	"context"
	"fmt"
	"io"

	"github.com/underpass-ai/underpass-bots/application"
	"github.com/underpass-ai/underpass-bots/domain"
)

func executionGet(ctx context.Context, app application.Dependencies, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "underpass-bots: execution get needs exactly one <execution-id>")
		return exitUsage
	}
	id, err := domain.NewExecutionID(args[0])
	if err != nil {
		return fail(stderr, err)
	}
	execution, err := application.NewGetExecution(app).Execute(ctx, id)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, execution.Snapshot())
	return exitOK
}
