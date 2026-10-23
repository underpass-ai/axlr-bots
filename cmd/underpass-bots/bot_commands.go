package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/underpass-ai/underpass-bots/application"
	"github.com/underpass-ai/underpass-bots/domain"
)

func botRegister(ctx context.Context, app application.Dependencies, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("bot register", flag.ContinueOnError)
	flags.SetOutput(stderr)
	file := flags.String("file", "", "bot definition (JSON)")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}
	if *file == "" {
		fmt.Fprintln(stderr, "underpass-bots: bot register needs --file <bot.json>")
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
	bot, err := application.NewRegisterBot(app).Execute(ctx, definition)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, bot.Snapshot())
	return exitOK
}

func botEnable(ctx context.Context, app application.Dependencies, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "underpass-bots: bot enable needs exactly one <bot-id>")
		return exitUsage
	}
	id, err := domain.NewBotID(args[0])
	if err != nil {
		return fail(stderr, err)
	}
	bot, err := application.NewEnableBot(app).Execute(ctx, id)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, bot.Snapshot())
	return exitOK
}

func botList(ctx context.Context, app application.Dependencies, args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "underpass-bots: bot list takes no arguments")
		return exitUsage
	}
	bots, err := application.NewListBots(app).Execute(ctx)
	if err != nil {
		return fail(stderr, err)
	}
	snapshots := make([]domain.BotSnapshot, 0, len(bots))
	for _, b := range bots {
		snapshots = append(snapshots, b.Snapshot())
	}
	printJSON(stdout, snapshots)
	return exitOK
}

func botGet(ctx context.Context, app application.Dependencies, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "underpass-bots: bot get needs exactly one <bot-id>")
		return exitUsage
	}
	id, err := domain.NewBotID(args[0])
	if err != nil {
		return fail(stderr, err)
	}
	bot, err := application.NewGetBot(app).Execute(ctx, id)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, bot.Snapshot())
	return exitOK
}
