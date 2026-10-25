package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func taskSubmit(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || len(args[0]) == 0 || args[0][0] == '-' {
		fmt.Fprintln(stderr, "axlr-bots: task submit needs <bot-id> first, then --objective and --key")
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
		fmt.Fprintln(stderr, "axlr-bots: task submit needs --objective and --key")
		return exitUsage
	}
	result, err := service.SubmitTask(ctx, unixapi.SubmitRequest{BotID: botID, Objective: *objective, IdempotencyKey: *key, CorrelationID: *correlation})
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, result)
	return exitOK
}

func taskRun(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "axlr-bots: task run needs exactly one <task-id>")
		return exitUsage
	}
	result, err := service.RunTask(ctx, args[0])
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, result)
	return exitCodeFor(result.Execution.Outcome)
}

func taskDecide(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	positional, call, ok := decideArgs("task decide", args, stderr)
	if !ok {
		return exitUsage
	}
	if len(positional) != 2 {
		fmt.Fprintln(stderr, "axlr-bots: task decide needs <task-id> and approve|deny")
		return exitUsage
	}
	if !application.IsDecision(positional[1]) {
		fmt.Fprintf(stderr, "axlr-bots: task decide takes approve or deny, not %q\n", positional[1])
		return exitUsage
	}
	return decide(ctx, service, positional[0], unixapi.DecideRequest{Decision: positional[1], CallID: call}, stdout, stderr)
}

// taskDecideAs is task approve and task deny: the decision is the verb.
func taskDecideAs(ctx context.Context, service unixapi.Service, decision string, args []string, stdout, stderr io.Writer) int {
	positional, call, ok := decideArgs("task "+decision, args, stderr)
	if !ok {
		return exitUsage
	}
	if len(positional) != 1 {
		fmt.Fprintf(stderr, "axlr-bots: task %s needs exactly one <task-id>\n", decision)
		return exitUsage
	}
	return decide(ctx, service, positional[0], unixapi.DecideRequest{Decision: decision, CallID: call}, stdout, stderr)
}

// decideArgs reads --call, before or after the positional arguments.
func decideArgs(name string, args []string, stderr io.Writer) ([]string, string, bool) {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	call := flags.String("call", "", "the pending call the decision is for; refused when the task waits on another")
	positional, ok := parseAnywhere(flags, args)
	return positional, *call, ok
}

func decide(ctx context.Context, service unixapi.Service, id string, request unixapi.DecideRequest, stdout, stderr io.Writer) int {
	result, err := service.DecideTask(ctx, id, request)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, result)
	return exitCodeFor(result.Execution.Outcome)
}

func taskGet(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "axlr-bots: task get needs exactly one <task-id>")
		return exitUsage
	}
	task, err := service.GetTask(ctx, args[0])
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, task)
	return exitOK
}

// taskReport prints the task's report as text, or only its path.
func taskReport(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("task report", flag.ContinueOnError)
	flags.SetOutput(stderr)
	pathOnly := flags.Bool("path", false, "print only where the report is kept")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}
	if flags.NArg() != 1 {
		fmt.Fprintln(stderr, "axlr-bots: task report needs exactly one <task-id>")
		return exitUsage
	}
	report, err := service.GetReport(ctx, flags.Arg(0))
	if err != nil {
		return fail(stderr, err)
	}
	if *pathOnly {
		fmt.Fprintln(stdout, report.Reference)
		return exitOK
	}
	fmt.Fprint(stdout, report.Content)
	if len(report.Content) > 0 && report.Content[len(report.Content)-1] != '\n' {
		fmt.Fprintln(stdout)
	}
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
		fmt.Fprintln(stderr, "axlr-bots: task reconcile takes no arguments, only --grace")
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

// taskCancel stops a task that has not finished.
func taskCancel(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "axlr-bots: task cancel needs exactly one <task-id>")
		return exitUsage
	}
	result, err := service.CancelTask(ctx, args[0])
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, result)
	return exitOK
}

// taskExecutions lists a task's executions, oldest first.
func taskExecutions(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "axlr-bots: task executions needs exactly one <task-id>")
		return exitUsage
	}
	executions, err := service.ListExecutions(ctx, args[0])
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, executions)
	return exitOK
}

// taskList lists the tasks across bots, newest first.
func taskList(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("task list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	bot := flags.String("bot", "", "only this bot's tasks")
	status := flags.String("status", "", "only tasks in this status (Pending, Running, WaitingApproval, Completed, Failed, Cancelled)")
	since := flags.String("since", "", "only tasks created at or after this RFC 3339 time")
	limit := flags.Int("limit", 0, "how many of the newest tasks (default 100, at most 1000)")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "axlr-bots: task list takes flags only")
		return exitUsage
	}
	query := application.TaskQuery{BotID: *bot, Status: *status, Limit: *limit}
	if *since != "" {
		parsed, err := time.Parse(time.RFC3339Nano, *since)
		if err != nil {
			fmt.Fprintln(stderr, "axlr-bots: --since must be an RFC 3339 time")
			return exitUsage
		}
		query.Since = parsed
	}
	tasks, err := service.ListTasks(ctx, query)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, tasks)
	return exitOK
}

// taskEvents prints the task's timeline, oldest first.
func taskEvents(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || args[0] == "" || args[0][0] == '-' {
		fmt.Fprintln(stderr, "axlr-bots: task events needs <task-id> first")
		return exitUsage
	}
	taskID, rest := args[0], args[1:]
	flags := flag.NewFlagSet("task events", flag.ContinueOnError)
	flags.SetOutput(stderr)
	limit := flags.Int("limit", 0, "how many of the newest events (default 100, at most 1000)")
	if err := flags.Parse(rest); err != nil {
		return exitUsage
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "axlr-bots: task events takes --limit only")
		return exitUsage
	}
	events, err := service.ListTaskEvents(ctx, taskID, *limit)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, events)
	return exitOK
}

// overview prints the dashboard's counts: nodes, bots and tasks by
// status, and the tasks, executions and cost since a moment.
func overview(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("overview", flag.ContinueOnError)
	flags.SetOutput(stderr)
	since := flags.String("since", "", "count from this RFC 3339 time (default: the start of today)")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "axlr-bots: overview takes --since only")
		return exitUsage
	}
	var from time.Time
	if *since != "" {
		parsed, err := time.Parse(time.RFC3339Nano, *since)
		if err != nil {
			fmt.Fprintln(stderr, "axlr-bots: --since must be an RFC 3339 time")
			return exitUsage
		}
		from = parsed
	}
	value, err := service.GetOverview(ctx, from)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, value)
	return exitOK
}
