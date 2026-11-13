package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
)

func botRegister(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("bot register", flag.ContinueOnError)
	flags.SetOutput(stderr)
	file := flags.String("file", "", "bot definition (JSON)")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}
	if *file == "" {
		fmt.Fprintln(stderr, "axlr-bots: bot register needs --file <bot.json>")
		return exitUsage
	}
	raw, err := os.ReadFile(*file)
	if err != nil {
		return fail(stderr, err)
	}
	var definition application.BotDefinition
	decoder := json.NewDecoder(bytesReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&definition); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) && typeErr.Field == "model" {
			return fail(stderr, fmt.Errorf("%s: model must be an object, for example {\"model\": \"provider/model\"}", *file))
		}
		return fail(stderr, fmt.Errorf("%s: %w", *file, err))
	}
	bot, err := service.RegisterBot(ctx, definition)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, bot)
	return exitOK
}

func botEnable(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "axlr-bots: bot enable needs exactly one <bot-id>")
		return exitUsage
	}
	bot, err := service.EnableBot(ctx, args[0])
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, bot)
	return exitOK
}

func botPause(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "axlr-bots: bot pause needs exactly one <bot-id>")
		return exitUsage
	}
	bot, err := service.PauseBot(ctx, args[0])
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, bot)
	return exitOK
}

// botStop pauses the bot and cancels every task it has not finished.
func botStop(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "axlr-bots: bot stop needs exactly one <bot-id>")
		return exitUsage
	}
	stopped, err := service.StopBot(ctx, args[0])
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, stopped)
	return exitOK
}

// botDelete deletes the bot: its unfinished tasks and the children it
// asked for are cancelled, its incidents resolved, its timers removed;
// the answer names the bots that still name it.
func botDelete(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "axlr-bots: bot delete needs exactly one <bot-id>")
		return exitUsage
	}
	deleted, err := service.DeleteBot(ctx, args[0])
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, deleted)
	return exitOK
}

func botList(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "axlr-bots: bot list takes no arguments")
		return exitUsage
	}
	bots, err := service.ListBots(ctx)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, bots)
	return exitOK
}

func botGet(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "axlr-bots: bot get needs exactly one <bot-id>")
		return exitUsage
	}
	bot, err := service.GetBot(ctx, args[0])
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, bot)
	return exitOK
}

// botUpdate applies a patch file to a bot's definition (PATCH /v1/bots/{id}).
func botUpdate(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || args[0] == "" || args[0][0] == '-' {
		fmt.Fprintln(stderr, "axlr-bots: bot update needs <bot-id> first, then --file <patch.json>")
		return exitUsage
	}
	botID, rest := args[0], args[1:]
	flags := flag.NewFlagSet("bot update", flag.ContinueOnError)
	flags.SetOutput(stderr)
	file := flags.String("file", "", "the patch (JSON): the fields to replace")
	if err := flags.Parse(rest); err != nil {
		return exitUsage
	}
	if *file == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "axlr-bots: bot update needs --file <patch.json>")
		return exitUsage
	}
	raw, err := os.ReadFile(*file)
	if err != nil {
		return fail(stderr, err)
	}
	var patch application.BotPatch
	decoder := json.NewDecoder(bytesReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&patch); err != nil {
		return fail(stderr, fmt.Errorf("%s: %w", *file, err))
	}
	bot, err := service.UpdateBot(ctx, botID, patch)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, bot)
	return exitOK
}

// botEvents prints the newest events of a bot, oldest first.
func botEvents(ctx context.Context, service unixapi.Service, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || args[0] == "" || args[0][0] == '-' {
		fmt.Fprintln(stderr, "axlr-bots: bot events needs <bot-id> first")
		return exitUsage
	}
	botID, rest := args[0], args[1:]
	flags := flag.NewFlagSet("bot events", flag.ContinueOnError)
	flags.SetOutput(stderr)
	limit := flags.Int("limit", 0, "how many of the newest events (default 100, at most 1000)")
	if err := flags.Parse(rest); err != nil {
		return exitUsage
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "axlr-bots: bot events takes --limit only")
		return exitUsage
	}
	events, err := service.ListEvents(ctx, botID, *limit)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, events)
	return exitOK
}
