package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/underpass-ai/axlr-bots/application"
)

// botStage stages a patch file for later (PUT /v1/bots/{id}/staged): the
// patch bot update takes, naming the version it brings.
func botStage(ctx context.Context, service application.OperatorService, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || args[0] == "" || args[0][0] == '-' {
		fmt.Fprintln(stderr, "axlr-bots: bot stage needs <bot-id> first, then --file <patch.json>")
		return exitUsage
	}
	botID, rest := args[0], args[1:]
	flags := flag.NewFlagSet("bot stage", flag.ContinueOnError)
	flags.SetOutput(stderr)
	file := flags.String("file", "", "the patch (JSON): the fields to replace and the version it brings")
	if err := flags.Parse(rest); err != nil {
		return exitUsage
	}
	if *file == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "axlr-bots: bot stage needs --file <patch.json>")
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
	bot, err := service.StageBot(ctx, botID, patch)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, bot)
	return exitOK
}

// botStaged prints the change a bot holds staged, its secrets redacted.
func botStaged(ctx context.Context, service application.OperatorService, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "axlr-bots: bot staged needs exactly one <bot-id>")
		return exitUsage
	}
	view, err := service.GetStagedBot(ctx, args[0])
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, view)
	return exitOK
}

// botApplyStaged applies the change a bot holds staged, through the
// update, and drops it.
func botApplyStaged(ctx context.Context, service application.OperatorService, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "axlr-bots: bot apply-staged needs exactly one <bot-id>")
		return exitUsage
	}
	bot, err := service.ApplyStagedBot(ctx, args[0])
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, bot)
	return exitOK
}

// botUnstage drops the change a bot holds staged, unapplied.
func botUnstage(ctx context.Context, service application.OperatorService, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "axlr-bots: bot unstage needs exactly one <bot-id>")
		return exitUsage
	}
	bot, err := service.UnstageBot(ctx, args[0])
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, bot)
	return exitOK
}
