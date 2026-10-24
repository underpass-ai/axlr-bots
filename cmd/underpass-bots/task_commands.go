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
	return exitCodeFor(result.Execution.Outcome())
}

func taskDecide(ctx context.Context, app application.Dependencies, args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, "underpass-bots: task decide needs <task-id> and approve|deny")
		return exitUsage
	}
	id, err := domain.NewTaskID(args[0])
	if err != nil {
		return fail(stderr, err)
	}
	if !application.IsDecision(args[1]) {
		fmt.Fprintf(stderr, "underpass-bots: task decide takes approve or deny, not %q\n", args[1])
		return exitUsage
	}
	result, err := application.NewApproveTask(app).Execute(ctx, id, args[1])
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, runOutput{Task: result.Task.Snapshot(), Execution: result.Execution.Snapshot()})
	return exitCodeFor(result.Execution.Outcome())
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

func taskReconcile(ctx context.Context, app application.Dependencies, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("task reconcile", flag.ContinueOnError)
	flags.SetOutput(stderr)
	grace := flags.Duration("grace", 0, "fail Running tasks older than this (default: the bot's task timeout plus 1m)")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "underpass-bots: task reconcile takes no arguments, only --grace")
		return exitUsage
	}
	result, err := application.NewReconcileTasks(app).Execute(ctx, *grace)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, reconcileOutput{Failed: taskIDStrings(result.Failed), Skipped: taskIDStrings(result.Skipped)})
	return exitOK
}

// taskIDStrings renders ids for JSON; an empty list prints as [] not null.
func taskIDStrings(ids []domain.TaskID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.String())
	}
	return out
}
