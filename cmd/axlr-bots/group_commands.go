package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
)

// groupList prints every team the bots name, with its members.
func groupList(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "axlr-bots: group list takes no arguments")
		return exitUsage
	}
	groups, err := service.ListGroups(ctx)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, groups)
	return exitOK
}

// groupGet prints one team, in the tenant given when several have it.
func groupGet(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("group get", flag.ContinueOnError)
	flags.SetOutput(stderr)
	tenant := flags.String("tenant", "", "the tenant, when several have a team of that name")
	positional, ok := parseAnywhere(flags, args)
	if !ok {
		return exitUsage
	}
	if len(positional) != 1 {
		fmt.Fprintln(stderr, "axlr-bots: group get needs exactly one <group>")
		return exitUsage
	}
	group, err := service.GetGroup(ctx, positional[0], *tenant)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, group)
	return exitOK
}

// groupEvents prints a team's timeline: its members' events, oldest
// first, after the one --after names.
func groupEvents(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("group events", flag.ContinueOnError)
	flags.SetOutput(stderr)
	tenant := flags.String("tenant", "", "the tenant, when several have a team of that name")
	after := flags.String("after", "", "only the events after this event id")
	limit := flags.Int("limit", 100, "at most this many events (up to 1000)")
	positional, ok := parseAnywhere(flags, args)
	if !ok {
		return exitUsage
	}
	if len(positional) != 1 {
		fmt.Fprintln(stderr, "axlr-bots: group events needs exactly one <group>")
		return exitUsage
	}
	events, err := service.ListGroupEvents(ctx, application.GroupEventsQuery{Name: positional[0], Tenant: *tenant, After: *after, Limit: *limit})
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, events)
	return exitOK
}
