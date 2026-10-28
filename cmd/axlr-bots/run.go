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
		return serve(ctx, app, *stateDir, socketPath, rest[1:], stdout, stderr)
	}
	if rest[0] == "overview" {
		return overview(ctx, newService(ctx, app, socketPath, nil), rest[1:], stdout, stderr)
	}
	if len(rest) < 2 {
		printUsage(stderr)
		return exitUsage
	}
	group, verb, rest := rest[0], rest[1], rest[2:]
	switch group + " " + verb {
	case "task reconcile":
		return taskReconcile(ctx, app, rest, stdout, stderr)
	case "ca init":
		return caInit(*stateDir, rest, stdout, stderr)
	case "ca show":
		return caShow(*stateDir, rest, stdout, stderr)
	}
	service := newService(ctx, app, socketPath, nil)
	switch group + " " + verb {
	case "bot register":
		return botRegister(ctx, service, rest, stdout, stderr)
	case "bot enable":
		return botEnable(ctx, service, rest, stdout, stderr)
	case "bot pause":
		return botPause(ctx, service, rest, stdout, stderr)
	case "bot stop":
		return botStop(ctx, service, rest, stdout, stderr)
	case "bot list":
		return botList(ctx, service, rest, stdout, stderr)
	case "bot get":
		return botGet(ctx, service, rest, stdout, stderr)
	case "bot update":
		return botUpdate(ctx, service, rest, stdout, stderr)
	case "bot events":
		return botEvents(ctx, service, rest, stdout, stderr)
	case "bot subscribe":
		return botSubscribe(ctx, service, rest, stdout, stderr)
	case "bot unsubscribe":
		return botUnsubscribe(ctx, service, rest, stdout, stderr)
	case "bot subscriptions":
		return botSubscriptions(ctx, service, rest, stdout, stderr)
	case "group list":
		return groupList(ctx, service, rest, stdout, stderr)
	case "group get":
		return groupGet(ctx, service, rest, stdout, stderr)
	case "group events":
		return groupEvents(ctx, service, rest, stdout, stderr)
	case "task submit":
		return taskSubmit(ctx, service, rest, stdout, stderr)
	case "task list":
		return taskList(ctx, service, rest, stdout, stderr)
	case "task events":
		return taskEvents(ctx, service, rest, stdout, stderr)
	case "task cancel":
		return taskCancel(ctx, service, rest, stdout, stderr)
	case "task executions":
		return taskExecutions(ctx, service, rest, stdout, stderr)
	case "trigger fire":
		return triggerFire(ctx, service, rest, stdout, stderr)
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
	case "execution artifact":
		return executionArtifact(ctx, service, rest, stdout, stderr)
	case "node list":
		return nodeList(ctx, service, rest, stdout, stderr)
	case "node get":
		return nodeGet(ctx, service, rest, stdout, stderr)
	case "node enroll-token":
		return nodeEnrollToken(ctx, service, rest, stdout, stderr)
	case "node revoke":
		return nodeRevoke(ctx, service, rest, stdout, stderr)
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

  serve [--listen <addr>] [--server-names a,b] [--api-listen <addr>] [--api-token-file <path>]
        [--tick 2s] [--reconcile-every 1m]
                                      run the control plane: the API on the socket, the node
                                      protocol over TLS on --listen (nodes on other hosts), the
                                      operator API over TLS on --api-listen behind a bearer
                                      token (<state>/api.token), executions handed to the
                                      nodes, in-process runs when no node is online, and the
                                      timer triggers
  ca init                             create the fleet CA under <state>/ca (once per fleet)
  ca show                             print the CA certificate and its fingerprint
  node list                           list the nodes (hosts running axlr-node)
  node get <node-id>                  show one node
  node enroll-token [--node-id <id>] [--labels k=v,k=v] [--ttl 1h]
                                      print a one-time token a new host enrolls with
  node revoke <node-id>               refuse the node's certificate and fail what it ran
  bot register --file <bot.json>      register a bot from its definition
  bot enable <bot-id>                 let the bot accept tasks
  bot pause <bot-id>                  put an enabled bot on hold; it accepts no task
  bot stop <bot-id>                   pause it and cancel every task it has not finished
  bot list                            list bots
  bot get <bot-id>                    show one bot
  bot update <bot-id> --file <patch.json>
                                      replace the fields the patch names (mission, level,
                                      allowedTools, toolRules, triggers, placement, approvals,
                                      delegation, groups, events, subscriptions, model, budgets)
  bot events <bot-id> [--limit 100]   the bot's newest events, oldest first
  bot subscribe <bot-id> <topic> [--mode task|message|note] [--from a,b]
                                      make the bot listen to a topic of its teams
  bot unsubscribe <bot-id> <topic>    stop it listening, or drop its pending ask
  bot subscriptions <bot-id>          the topics it listens to; a pending ask is marked
  group list                          list the teams the bots name, with their members
  group get <group> [--tenant <id>]   show one team: each member's purpose, tools, where it runs
  group events <group> [--after <event-id>] [--limit 100] [--tenant <id>]
                                      the team's timeline: its members' events, oldest first
  task submit <bot-id> --objective "<text>" --key <idempotency-key> [--correlation <id>] [--urgent]
                                      accept a task; the same key names the same task; --urgent
                                      goes first and cancels the bot's work in progress
  trigger fire <bot-id> <trigger> --body '<json>' | --file <event.json> [--delivery <id>]
                                      fire a webhook trigger with an event (over the socket)
  task run <task-id>                  run a pending task now, in process
                                      (exit 0 completed, 3 decision required, 1 failed)
  task approve <task-id> [--call <id>] approve the call a WaitingApproval task stopped on
  task deny <task-id> [--call <id>]   deny it; both run the session to its end under a
                                      new execution (same exit codes as task run); --call
                                      must name the pending call; past its deadline the
                                      decision is refused (approval_expired)
  task decide <task-id> approve|deny  the same, with the decision as a word
  task get <task-id>                  show one task
  task list [--bot <id>] [--status <s>] [--since <time>] [--limit 100]
                                      the tasks across bots, newest first
  task events <task-id> [--limit 100] the task's timeline, oldest first
  task executions <task-id>           the task's executions, oldest first
  task cancel <task-id>               stop a task that has not finished
  task report <task-id> [--path]      print the task's report (or only where it is)
  task reconcile [--grace <duration>] fail the tasks a crashed run left Running
                                      longer than the grace (default: the bot's
                                      task timeout plus 1m); prints failed/skipped
  execution get <execution-id>        show one execution: outcome, answer, usage, policy,
                                      artifacts and receipt
  execution artifact <execution-id> <name> [--out <file>]
                                      an artifact the control plane keeps, checked
                                      against its SHA-256
  overview [--since <time>]           nodes, bots and tasks by status; tasks, executions
                                      and cost since the start of today
  install [--system] [--prefix <dir>] [--state <dir>] [--axlr-bin <path>]
          [--listen <addr>] [--server-names a,b] [--control <url>] [--node-only]
                                      install both binaries, the two systemd units and the
                                      environment file under ~/.local (or --system); --listen
                                      serves nodes over TLS, --node-only joins a control
                                      plane elsewhere at --control (see docs/runbooks/install.md)
  uninstall [--system] [--prefix <dir>] [--purge]
                                      remove them (keep the state unless --purge)

State lives under --state (default $AXLR_BOTS_STATE, $XDG_STATE_HOME/axlr-bots
or ~/.local/state/axlr-bots). When a daemon answers on the socket the
commands go through it; otherwise they work on the state directory. Output is
JSON on stdout.
`)
}
