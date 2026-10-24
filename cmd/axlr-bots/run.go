package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/underpass-ai/axlr-bots/application"
)

// socketFileName is the daemon's socket under the state directory.
const socketFileName = "axlr-bots.sock"

// run parses the global flags, wires the application and dispatches the
// subcommand. It returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("axlr-bots", flag.ContinueOnError)
	flags.SetOutput(stderr)
	stateDir := flags.String("state", defaultStateDir(), "state directory (bots, tasks, executions, events, audit, traces, artifacts)")
	axlrBin := flags.String("axlr-bin", defaultAXLRBinary(), "the axlr-tui to run tasks with (also AXLR_TUI)")
	actor := flags.String("actor", defaultActor(), "who is acting, for the audit log")
	socket := flags.String("socket", defaultSocket(), "the daemon's socket (also AXLR_BOTS_SOCKET; default <state>/axlr-bots.sock; empty: never use a daemon)")
	flags.Usage = func() { printUsage(stderr) }
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	rest := flags.Args()
	if len(rest) < 1 {
		printUsage(stderr)
		return exitUsage
	}
	socketPath := *socket
	if socketPath == "" && !flagSet(flags, "socket") && os.Getenv("AXLR_BOTS_SOCKET") == "" {
		socketPath = filepath.Join(*stateDir, socketFileName)
	}
	ctx := context.Background()
	switch rest[0] {
	case "install":
		return install(rest[1:], stdout, stderr)
	case "uninstall":
		return uninstall(rest[1:], stdout, stderr)
	}
	app, err := newApp(*stateDir, *axlrBin, *actor)
	if err != nil {
		fmt.Fprintln(stderr, "axlr-bots:", err)
		return exitError
	}
	if rest[0] == "serve" {
		return serve(ctx, app, socketPath, rest[1:], stdout, stderr)
	}
	if len(rest) < 2 {
		printUsage(stderr)
		return exitUsage
	}
	group, verb, rest := rest[0], rest[1], rest[2:]
	if group+" "+verb == "task reconcile" {
		return taskReconcile(ctx, app, rest, stdout, stderr)
	}
	service := newService(ctx, app, socketPath, nil)
	switch group + " " + verb {
	case "bot register":
		return botRegister(ctx, service, rest, stdout, stderr)
	case "bot enable":
		return botEnable(ctx, service, rest, stdout, stderr)
	case "bot list":
		return botList(ctx, service, rest, stdout, stderr)
	case "bot get":
		return botGet(ctx, service, rest, stdout, stderr)
	case "task submit":
		return taskSubmit(ctx, service, rest, stdout, stderr)
	case "task run":
		return taskRun(ctx, service, rest, stdout, stderr)
	case "task decide":
		return taskDecide(ctx, service, rest, stdout, stderr)
	case "task approve":
		return taskDecideAs(ctx, service, application.DecisionApprove, rest, stdout, stderr)
	case "task deny":
		return taskDecideAs(ctx, service, application.DecisionDeny, rest, stdout, stderr)
	case "task get":
		return taskGet(ctx, service, rest, stdout, stderr)
	case "task report":
		return taskReport(ctx, service, rest, stdout, stderr)
	case "execution get":
		return executionGet(ctx, service, rest, stdout, stderr)
	case "node list":
		return nodeList(ctx, service, rest, stdout, stderr)
	case "node get":
		return nodeGet(ctx, service, rest, stdout, stderr)
	}
	fmt.Fprintf(stderr, "axlr-bots: unknown command %q %q\n", group, verb)
	printUsage(stderr)
	return exitUsage
}

// flagSet reports whether the flag was given on the command line.
func flagSet(flags *flag.FlagSet, name string) bool {
	given := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == name {
			given = true
		}
	})
	return given
}

func defaultStateDir() string {
	if dir := os.Getenv("AXLR_BOTS_STATE"); dir != "" {
		return dir
	}
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "axlr-bots")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", "axlr-bots-state")
	}
	return filepath.Join(home, ".local", "state", "axlr-bots")
}

func defaultAXLRBinary() string {
	if bin := os.Getenv("AXLR_TUI"); bin != "" {
		return bin
	}
	return "axlr-tui"
}

// defaultSocket is AXLR_BOTS_SOCKET; empty means <state>/axlr-bots.sock
// once the state directory is known.
func defaultSocket() string {
	return os.Getenv("AXLR_BOTS_SOCKET")
}

func defaultActor() string {
	if user := os.Getenv("USER"); user != "" {
		return "cli:" + user
	}
	return "cli:unknown"
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `usage: axlr-bots [--state <dir>] [--axlr-bin <path>] [--socket <path>] [--actor <name>] <command>

  serve [--listen <addr>] [--tick 2s] [--reconcile-every 1m]
                                      run the control plane: the API on the socket (and
                                      a TCP address for nodes elsewhere), executions handed
                                      to the nodes, in-process runs when no node is online
  node list                           list the nodes (hosts running axlr-node)
  node get <node-id>                  show one node
  bot register --file <bot.json>      register a bot from its definition
  bot enable <bot-id>                 let the bot accept tasks
  bot list                            list bots
  bot get <bot-id>                    show one bot
  task submit <bot-id> --objective "<text>" --key <idempotency-key> [--correlation <id>]
                                      accept a task; the same key names the same task
  task run <task-id>                  run a pending task now, in process
                                      (exit 0 completed, 3 decision required, 1 failed)
  task approve <task-id>              approve the call a WaitingApproval task stopped on
  task deny <task-id>                 deny it; both run the session to its end under a
                                      new execution (same exit codes as task run)
  task decide <task-id> approve|deny  the same, with the decision as a word
  task get <task-id>                  show one task
  task report <task-id> [--path]      print the task's report (or only where it is)
  task reconcile [--grace <duration>] fail the tasks a crashed run left Running
                                      longer than the grace (default: the bot's
                                      task timeout plus 1m); prints failed/skipped
  execution get <execution-id>        show one execution: outcome, answer, usage, policy
  install [--system] [--prefix <dir>] [--state <dir>] [--axlr-bin <path>]
                                      install both binaries, the two systemd units and the
                                      environment file under ~/.local (or --system) (see README)
  uninstall [--system] [--prefix <dir>] [--purge]
                                      remove them (keep the state unless --purge)

State lives under --state (default $AXLR_BOTS_STATE, $XDG_STATE_HOME/axlr-bots
or ~/.local/state/axlr-bots). When a daemon answers on the socket the
commands go through it; otherwise they work on the state directory. Output is
JSON on stdout.
`)
}
