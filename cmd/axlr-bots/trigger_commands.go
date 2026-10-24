package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
)

// triggerFire fires a bot's webhook trigger from the command line, with
// the event from --body or --file: what a sender does with a POST on the
// operator address, done here over the socket (no secret needed: the
// caller is trusted there).
func triggerFire(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 || args[0] == "" || args[0][0] == '-' || args[1] == "" || args[1][0] == '-' {
		fmt.Fprintln(stderr, "axlr-bots: trigger fire needs <bot-id> <trigger> first, then --body or --file")
		return exitUsage
	}
	botID, trigger, rest := args[0], args[1], args[2:]
	flags := flag.NewFlagSet("trigger fire", flag.ContinueOnError)
	flags.SetOutput(stderr)
	body := flags.String("body", "", "the event, a JSON object")
	file := flags.String("file", "", "a file with the event, a JSON object")
	delivery := flags.String("delivery", "", "the delivery id (the Idempotency-Key); the same id names the same task")
	if err := flags.Parse(rest); err != nil {
		return exitUsage
	}
	if flags.NArg() != 0 || (*body == "") == (*file == "") {
		fmt.Fprintln(stderr, "axlr-bots: trigger fire needs --body or --file, not both")
		return exitUsage
	}
	event := []byte(*body)
	if *file != "" {
		raw, err := os.ReadFile(*file)
		if err != nil {
			return fail(stderr, err)
		}
		event = raw
	}
	result, err := service.FireTrigger(ctx, unixapi.FireRequest{BotID: botID, Trigger: trigger, DeliveryID: *delivery, Body: json.RawMessage(event)})
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, result)
	return exitOK
}
