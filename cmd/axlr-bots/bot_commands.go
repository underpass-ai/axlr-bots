package main

import (
	"context"
	"encoding/json"
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
