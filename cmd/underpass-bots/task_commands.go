package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/underpass-ai/underpass-bots/application"
	"github.com/underpass-ai/underpass-bots/domain"
)

func taskSubmit(ctx context.Context, app application.Dependencies, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || len(args[0]) == 0 || args[0][0] == '-' {
		fmt.Fprintln(stderr, "underpass-bots: task submit needs <bot-id> first, then --objective and --key")
		return exitUsage
	}
	botID, rest := args[0], args[1:]
	flags := flag.NewFlagSet("task submit", flag.ContinueOnError)
	flags.SetOutput(stderr)
	objective := flags.String("objective", "", "what the task asks for")
	key := flags.String("key", "", "idempotency key: the same key names the same task")
	correlation := flags.String("correlation", "", "correlation id from upstream, if any")
	if err := flags.Parse(rest); err != nil {
		return exitUsage
	}
	if *objective == "" || *key == "" {
		fmt.Fprintln(stderr, "underpass-bots: task submit needs --objective and --key")
		return exitUsage
	}
	result, err := application.NewSubmitTask(app).Execute(ctx, application.SubmitTaskCommand{
		BotID: botID, Objective: *objective, IdempotencyKey: *key, CorrelationID: *correlation,
	})
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, submitOutput{Created: result.Created, Task: result.Task.Snapshot()})
	return exitOK
}

func taskRun(ctx context.Context, app application.Dependencies, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "underpass-bots: task run needs exactly one <task-id>")
		return exitUsage
	}
	id, err := domain.NewTaskID(args[0])
	if err != nil {
		return fail(stderr, err)
	}
	result, err := application.NewRunTask(app).Execute(ctx, id)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, runOutput{Task: result.Task.Snapshot(), Execution: result.Execution.Snapshot()})
	switch result.Execution.Outcome() {
	case domain.ExecutionCompleted:
		return exitOK
	case domain.ExecutionDecisionRequired:
		return exitDecisionRequired
	}
	return exitError
}

func taskGet(ctx context.Context, app application.Dependencies, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "underpass-bots: task get needs exactly one <task-id>")
		return exitUsage
	}
	id, err := domain.NewTaskID(args[0])
	if err != nil {
		return fail(stderr, err)
	}
	task, err := application.NewGetTask(app).Execute(ctx, id)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, task.Snapshot())
	return exitOK
}
