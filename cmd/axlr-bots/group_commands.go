package main

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
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
