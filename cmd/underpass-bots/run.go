package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// run parses the global flags, wires the application and dispatches the
// subcommand. It returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("underpass-bots", flag.ContinueOnError)
	flags.SetOutput(stderr)
	stateDir := flags.String("state", defaultStateDir(), "state directory (bots, tasks, executions, events, audit, traces)")
	axlrBin := flags.String("axlr-bin", defaultAXLRBinary(), "the axlr-tui to run tasks with (also UNDERPASS_AXLR_BIN)")
	actor := flags.String("actor", defaultActor(), "who is acting, for the audit log")
	flags.Usage = func() { printUsage(stderr) }
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	rest := flags.Args()
	if len(rest) < 2 {
		printUsage(stderr)
		return exitUsage
	}
	app, err := newApp(*stateDir, *axlrBin, *actor)
	if err != nil {
		fmt.Fprintln(stderr, "underpass-bots:", err)
		return exitError
	}
	ctx := context.Background()
	group, verb, rest := rest[0], rest[1], rest[2:]
	var code int
	switch group + " " + verb {
	case "bot register":
		code = botRegister(ctx, app, rest, stdout, stderr)
	case "bot enable":
		code = botEnable(ctx, app, rest, stdout, stderr)
	case "bot list":
		code = botList(ctx, app, rest, stdout, stderr)
	case "bot get":
		code = botGet(ctx, app, rest, stdout, stderr)
	case "task submit":
		code = taskSubmit(ctx, app, rest, stdout, stderr)
	case "task run":
		code = taskRun(ctx, app, rest, stdout, stderr)
	case "task get":
		code = taskGet(ctx, app, rest, stdout, stderr)
	case "task reconcile":
		code = taskReconcile(ctx, app, rest, stdout, stderr)
	case "execution get":
		code = executionGet(ctx, app, rest, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "underpass-bots: unknown command %q %q\n", group, verb)
		printUsage(stderr)
		code = exitUsage
	}
	return code
}

func defaultStateDir() string {
	if dir := os.Getenv("UNDERPASS_STATE"); dir != "" {
		return dir
	}
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "underpass-bots")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "underpass-bots-state")
	}
	return filepath.Join(home, ".local", "state", "underpass-bots")
}

func defaultAXLRBinary() string {
	if bin := os.Getenv("UNDERPASS_AXLR_BIN"); bin != "" {
		return bin
	}
	return "axlr-tui"
}

func defaultActor() string {
	if user := os.Getenv("USER"); user != "" {
		return "cli:" + user
	}
	return "cli:unknown"
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `usage: underpass-bots [--state <dir>] [--axlr-bin <path>] [--actor <name>] <command>

  bot register --file <bot.json>      register a bot from its definition
  bot enable <bot-id>                 let the bot accept tasks
  bot list                            list bots
  bot get <bot-id>                    show one bot
  task submit <bot-id> --objective "<text>" --key <idempotency-key> [--correlation <id>]
                                      accept a task; the same key names the same task
  task run <task-id>                  run a pending task once through AXLR
                                      (exit 0 completed, 3 decision required, 1 failed)
  task get <task-id>                  show one task
  task reconcile [--grace <duration>] fail the tasks a crashed run left Running
                                      longer than the grace (default: the bot's
                                      task timeout plus 1m); prints failed/skipped
  execution get <execution-id>        show one execution: outcome, answer, usage

State lives under --state (default $UNDERPASS_STATE, $XDG_STATE_HOME/underpass-bots
or ~/.local/state/underpass-bots). Output is JSON on stdout.
`)
}
