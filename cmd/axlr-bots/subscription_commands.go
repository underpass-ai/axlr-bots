package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// botSubscribe makes the bot listen to a topic of its teams: it reads
// the bot's subscriptions, adds the topic (or replaces the one of the
// same pattern) and writes the list back (PATCH /v1/bots/{id}).
func botSubscribe(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("bot subscribe", flag.ContinueOnError)
	flags.SetOutput(stderr)
	mode := flags.String("mode", "", "task, message or note; absent: by the event's severity")
	from := flags.String("from", "", "only events of these teammates, operator or control-plane, comma-separated; absent: anyone in the team")
	positional, ok := parseAnywhere(flags, args)
	if !ok {
		return exitUsage
	}
	if len(positional) != 2 {
		fmt.Fprintln(stderr, "axlr-bots: bot subscribe needs <bot-id> <topic>")
		return exitUsage
	}
	bot, err := service.GetBot(ctx, positional[0])
	if err != nil {
		return fail(stderr, err)
	}
	entry := domain.SubscriptionSnapshot{Topic: positional[1], Mode: *mode}
	for _, sender := range strings.Split(*from, ",") {
		if sender = strings.TrimSpace(sender); sender != "" {
			entry.From = append(entry.From, sender)
		}
	}
	list := []domain.SubscriptionSnapshot{}
	replaced := false
	for _, sub := range bot.Subscriptions {
		if sub.Topic == entry.Topic {
			sub, replaced = entry, true
		}
		list = append(list, sub)
	}
	if !replaced {
		list = append(list, entry)
	}
	return writeSubscriptions(ctx, service, positional[0], list, stdout, stderr)
}

// botUnsubscribe stops the bot listening to a topic, or drops a pending
// ask for it.
func botUnsubscribe(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, "axlr-bots: bot unsubscribe needs <bot-id> <topic>")
		return exitUsage
	}
	bot, err := service.GetBot(ctx, args[0])
	if err != nil {
		return fail(stderr, err)
	}
	list := []domain.SubscriptionSnapshot{}
	found := false
	for _, sub := range bot.Subscriptions {
		if sub.Topic == args[1] {
			found = true
			continue
		}
		list = append(list, sub)
	}
	if !found {
		return fail(stderr, domain.NotFoundError{Kind: "subscription", ID: args[1]})
	}
	return writeSubscriptions(ctx, service, args[0], list, stdout, stderr)
}

// botSubscriptions prints the topics the bot listens to; a pending ask
// carries status pending.
func botSubscriptions(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "axlr-bots: bot subscriptions needs exactly one <bot-id>")
		return exitUsage
	}
	bot, err := service.GetBot(ctx, args[0])
	if err != nil {
		return fail(stderr, err)
	}
	list := bot.Subscriptions
	if list == nil {
		list = []domain.SubscriptionSnapshot{}
	}
	printJSON(stdout, list)
	return exitOK
}

func writeSubscriptions(ctx context.Context, service unixapi.Service, id string, list []domain.SubscriptionSnapshot, stdout, stderr io.Writer) int {
	bot, err := service.UpdateBot(ctx, id, application.BotPatch{Subscriptions: &list})
	if err != nil {
		return fail(stderr, err)
	}
	subs := bot.Subscriptions
	if subs == nil {
		subs = []domain.SubscriptionSnapshot{}
	}
	printJSON(stdout, subs)
	return exitOK
}
