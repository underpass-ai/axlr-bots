package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
)

// autonomyList prints the runbooks on ladders, with their step, counters
// and history.
func autonomyList(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("autonomy list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	tenant := flags.String("tenant", "", "only this tenant")
	bot := flags.String("bot", "", "only this bot")
	runbook := flags.String("runbook", "", "only this runbook (the alert name)")
	namespace := flags.String("namespace", "", "only this namespace")
	positional, ok := parseAnywhere(flags, args)
	if !ok {
		return exitUsage
	}
	if len(positional) != 0 {
		fmt.Fprintln(stderr, "axlr-bots: autonomy list takes no arguments")
		return exitUsage
	}
	list, err := service.ListAutonomy(ctx, application.AutonomyQuery{Tenant: *tenant, Bot: *bot, Runbook: *runbook, Namespace: *namespace})
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, list)
	return exitOK
}

// autonomyGet prints one runbook's step, counters and history.
func autonomyGet(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "axlr-bots: autonomy get needs exactly one <autonomy-id>")
		return exitUsage
	}
	response, err := service.GetAutonomy(ctx, args[0])
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, response)
	return exitOK
}
