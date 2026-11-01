package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
)

// incidentOpen reports an alert group by hand: a new incident and its
// triage task, a repeat of the one already open, or a reopening.
func incidentOpen(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("incident open", flag.ContinueOnError)
	flags.SetOutput(stderr)
	tenant := flags.String("tenant", "", "the tenant of the bot")
	bot := flags.String("bot", "", "the bot that triages it; it declares the incident trigger")
	groupKey := flags.String("group-key", "", "what the source groups the alerts by; the same key again is a repeat or a reopening")
	source := flags.String("source", "", "where the alerts come from (default manual)")
	title := flags.String("title", "", "the incident in one line")
	severity := flags.String("severity", "", "sev1, sev2, sev3 (the default) or sev4")
	fingerprints := flags.String("fingerprints", "", "the alerts, comma-separated")
	objective := flags.String("objective", "", "what the triage task asks for (default: the trigger's template, or the built-in one)")
	positional, ok := parseAnywhere(flags, args)
	if !ok {
		return exitUsage
	}
	if len(positional) != 0 || *tenant == "" || *bot == "" || *groupKey == "" || *title == "" {
		fmt.Fprintln(stderr, `axlr-bots: incident open needs --tenant, --bot, --group-key and --title`)
		return exitUsage
	}
	command := application.OpenIncidentCommand{Tenant: *tenant, BotID: *bot, GroupKey: *groupKey, Source: *source, Title: *title, Severity: *severity, Objective: *objective}
	for _, fingerprint := range strings.Split(*fingerprints, ",") {
		if fingerprint = strings.TrimSpace(fingerprint); fingerprint != "" {
			command.Fingerprints = append(command.Fingerprints, fingerprint)
		}
	}
	response, err := service.OpenIncident(ctx, command)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, response)
	return exitOK
}

// incidentList prints the incidents, the last seen first.
func incidentList(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("incident list", flag.ContinueOnError)
	flags.SetOutput(stderr)
	tenant := flags.String("tenant", "", "only this tenant")
	status := flags.String("status", "", "only this status: Open, Diagnosed, Resolved or Escalated")
	limit := flags.Int("limit", 0, "at most this many (100 when absent, up to 1000)")
	positional, ok := parseAnywhere(flags, args)
	if !ok {
		return exitUsage
	}
	if len(positional) != 0 {
		fmt.Fprintln(stderr, "axlr-bots: incident list takes no arguments")
		return exitUsage
	}
	incidents, err := service.ListIncidents(ctx, application.IncidentQuery{Tenant: *tenant, Status: *status, Limit: *limit})
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, incidents)
	return exitOK
}

// incidentGet prints one incident.
func incidentGet(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "axlr-bots: incident get needs exactly one <incident-id>")
		return exitUsage
	}
	incident, err := service.GetIncident(ctx, args[0])
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, incident)
	return exitOK
}

// incidentResolve closes an incident.
func incidentResolve(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("incident resolve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	reason := flags.String("reason", "", "why it is over")
	positional, ok := parseAnywhere(flags, args)
	if !ok {
		return exitUsage
	}
	if len(positional) != 1 {
		fmt.Fprintln(stderr, "axlr-bots: incident resolve needs exactly one <incident-id>")
		return exitUsage
	}
	incident, err := service.ResolveIncident(ctx, positional[0], unixapi.ResolveRequest{Reason: *reason})
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, incident)
	return exitOK
}
