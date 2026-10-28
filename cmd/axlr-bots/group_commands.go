package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

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

// groupTopics prints a team's topics: who publishes, who listens and
// how, the last event, and the ones nobody listens to.
func groupTopics(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("group topics", flag.ContinueOnError)
	flags.SetOutput(stderr)
	tenant := flags.String("tenant", "", "the tenant, when several have a team of that name")
	positional, ok := parseAnywhere(flags, args)
	if !ok {
		return exitUsage
	}
	if len(positional) != 1 {
		fmt.Fprintln(stderr, "axlr-bots: group topics needs exactly one <group>")
		return exitUsage
	}
	topics, err := service.ListGroupTopics(ctx, positional[0], *tenant)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, topics)
	return exitOK
}

// groupPublish publishes the operator's event on a team.
func groupPublish(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("group publish", flag.ContinueOnError)
	flags.SetOutput(stderr)
	tenant := flags.String("tenant", "", "the tenant, when several have a team of that name")
	message := flags.String("message", "", "one line: what happened and why")
	data := flags.String("data", "", "facts for the receivers, a JSON object")
	to := flags.String("to", "", "only these members, comma-separated; absent: the whole team")
	key := flags.String("key", "", "the idempotency key; the same key again is the same event (default: a new one)")
	positional, ok := parseAnywhere(flags, args)
	if !ok {
		return exitUsage
	}
	if len(positional) != 2 || *message == "" {
		fmt.Fprintln(stderr, `axlr-bots: group publish needs <group> <name> --message "<text>"`)
		return exitUsage
	}
	command := application.PublishGroupEventCommand{Group: positional[0], Tenant: *tenant, Name: positional[1], Message: *message, IdempotencyKey: *key}
	if *data != "" {
		if err := json.Unmarshal([]byte(*data), &command.Data); err != nil || command.Data == nil {
			fmt.Fprintln(stderr, "axlr-bots: --data must be a JSON object")
			return exitUsage
		}
	}
	for _, member := range strings.Split(*to, ",") {
		if member = strings.TrimSpace(member); member != "" {
			command.To = append(command.To, member)
		}
	}
	if command.IdempotencyKey == "" {
		command.IdempotencyKey = "publish-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	answer, err := service.PublishGroupEvent(ctx, command)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, answer)
	return exitOK
}
