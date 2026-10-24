package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/underpass-ai/underpass-bots/adapters/unixapi"
	"github.com/underpass-ai/underpass-bots/application"
)

// defaultBot is the bot an alert goes to when none is named.
const defaultBot = "sre-host-bot"

// alert submits the diagnosis task of a failed systemd unit. The OnFailure
// unit runs it; an operator or a script can too. When no daemon answers
// the task is spooled into the state directory, where the daemon picks
// it up on its next start, and the fallback is said on stderr.
func alert(ctx context.Context, app application.Dependencies, socketPath string, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("alert", flag.ContinueOnError)
	flags.SetOutput(stderr)
	unit := flags.String("unit", "", "the systemd unit that failed (a name without a type is a .service)")
	bot := flags.String("bot", envOr("UNDERPASS_BOT", defaultBot), "the bot that diagnoses (also UNDERPASS_BOT)")
	host := flags.String("host", hostname(), "the host, for the objective")
	at := flags.String("at", "", "when the unit failed, RFC 3339 (default now)")
	window := flags.Duration("window", application.DefaultAlertWindow, "failures of the unit within one window name one task")
	user := flags.Bool("user", os.Getenv("UNDERPASS_SYSTEMD_USER") == "1", "the unit belongs to systemd --user (also UNDERPASS_SYSTEMD_USER=1)")
	correlation := flags.String("correlation", "", "correlation id from upstream, if any")
	if err := flags.Parse(args); err != nil {
		return exitUsage
	}
	if *unit == "" || flags.NArg() != 0 {
		fmt.Fprintln(stderr, "underpass-bots: alert needs --unit <name>")
		return exitUsage
	}
	var when time.Time
	if *at != "" {
		parsed, err := time.Parse(time.RFC3339, *at)
		if err != nil {
			fmt.Fprintln(stderr, "underpass-bots: --at must be RFC 3339:", err)
			return exitUsage
		}
		when = parsed
	}
	service := newService(ctx, app, socketPath, stderr)
	result, err := service.Alert(ctx, unixapi.AlertRequest{
		BotID: *bot, Unit: *unit, Host: *host, At: when, WindowSeconds: int(window.Seconds()), UserManager: *user, CorrelationID: *correlation,
	})
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, result)
	return exitOK
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func hostname() string {
	name, err := os.Hostname()
	if err != nil {
		return ""
	}
	return name
}
