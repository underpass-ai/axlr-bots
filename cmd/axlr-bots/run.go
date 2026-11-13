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
	actor := flags.String("actor", defaultActor(), "who is acting, for the audit log; task approve, deny and decide send it as who decides (a bot's approvers name it exactly)")
	socket := flags.String("socket", defaultSocket(), "the daemon's socket (also AXLR_BOTS_SOCKET; default <state>/axlr-bots.sock; empty: never use a daemon)")
	var remote remoteControl
	flags.StringVar(&remote.URL, "control", os.Getenv("AXLR_BOTS_CONTROL"), "a control plane on another host: its operator address, https://host:7879, used in place of the socket (also AXLR_BOTS_CONTROL)")
	flags.StringVar(&remote.TokenFile, "token-file", os.Getenv("AXLR_BOTS_TOKEN_FILE"), "with --control, the file holding the operator token (also AXLR_BOTS_TOKEN_FILE)")
	flags.StringVar(&remote.CAFile, "ca-file", os.Getenv("AXLR_BOTS_CA_FILE"), "with --control, the CA the control plane's certificate is checked against, the fleet CA from ca show (also AXLR_BOTS_CA_FILE; default the system's)")
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
	if len(rest) == 2 && rest[0] == "task" && (rest[1] == "--help" || rest[1] == "-h") {
		fmt.Fprint(stdout, `usage: axlr-bots task <command>

  task submit <bot-id> --objective "<text>" --key <idempotency-key> [--correlation <id>] [--urgent]
  task run <task-id>
  task approve <task-id> [--call <id>]
  task deny <task-id> [--call <id>]
  task decide <task-id> approve|deny
  task get <task-id>
  task list [--bot <id>] [--status <s>] [--since <time>] [--limit 100]
  task events <task-id> [--limit 100]
  task executions <task-id>
  task cancel <task-id>
  task report <task-id> [--path]
  task reconcile [--grace <duration>]
`)
		return exitOK
	}
	socketPath := *socket
	if socketPath == "" && !flagSet(flags, "socket") && os.Getenv("AXLR_BOTS_SOCKET") == "" {
		socketPath = filepath.Join(*stateDir, socketFileName)
	}
	if err := remote.validate(); err != nil {
		fmt.Fprintln(stderr, "axlr-bots:", err)
		return exitUsage
	}
	ctx := context.Background()
	switch rest[0] {
	case "install":
		return install(rest[1:], stdout, stderr)
	case "uninstall":
		return uninstall(rest[1:], stdout, stderr)
	case "github-hooks":
		return githubHooks(socketPath, remote, rest[1:], stdout, stderr)
	case "activity-audit":
		return activityAudit(*stateDir, socketPath, remote, rest[1:], stdout, stderr)
	case "transcript-audit":
		return transcriptAudit(*stateDir, socketPath, remote, rest[1:], stdout, stderr)
	case "health":
		return health(ctx, socketPath, remote, rest[1:], stdout, stderr)
	case "version":
		return version(rest[1:], stdout, stderr)
	}
	// With --control a command goes to the operator address and never
	// opens the state directory; serve, ca and task reconcile work on the
	// local state whatever --control says.
	var app application.Dependencies
	if !remote.enabled() || localOnly(rest) {
		var err error
		app, err = newApp(*stateDir, *axlrBin, *actor)
		if err != nil {
			fmt.Fprintln(stderr, "axlr-bots:", err)
			return exitError
		}
	}
	if rest[0] == "serve" {
		return serve(ctx, app, *stateDir, socketPath, rest[1:], stdout, stderr)
	}
	if rest[0] == "overview" {
		service, release, err := newService(ctx, app, *stateDir, socketPath, remote, nil)
		if err != nil {
			return fail(stderr, err)
		}
		defer release()
		return overview(ctx, service, rest[1:], stdout, stderr)
	}
	if len(rest) < 2 {
		printUsage(stderr)
		return exitUsage
	}
	group, verb, rest := rest[0], rest[1], rest[2:]
	switch group + " " + verb {
	case "task reconcile":
		return taskReconcile(ctx, app, *stateDir, rest, stdout, stderr)
	case "ca init":
		return caInit(*stateDir, rest, stdout, stderr)
	case "ca show":
		return caShow(*stateDir, rest, stdout, stderr)
	case "ca issue-client":
		return caIssueClient(*stateDir, rest, stdout, stderr)
	}
	service, release, err := newService(ctx, app, *stateDir, socketPath, remote, nil)
	if err != nil {
		return fail(stderr, err)
	}
	defer release()
	switch group + " " + verb {
	case "bot register":
		return botRegister(ctx, service, rest, stdout, stderr)
	case "bot enable":
		return botEnable(ctx, service, rest, stdout, stderr)
	case "bot pause":
		return botPause(ctx, service, rest, stdout, stderr)
	case "bot stop":
		return botStop(ctx, service, rest, stdout, stderr)
	case "bot delete":
		return botDelete(ctx, service, rest, stdout, stderr)
	case "bot list":
		return botList(ctx, service, rest, stdout, stderr)
	case "bot get":
		return botGet(ctx, service, rest, stdout, stderr)
	case "bot update":
		return botUpdate(ctx, service, rest, stdout, stderr)
	case "bot stage":
		return botStage(ctx, service, rest, stdout, stderr)
	case "bot staged":
		return botStaged(ctx, service, rest, stdout, stderr)
	case "bot apply-staged":
		return botApplyStaged(ctx, service, rest, stdout, stderr)
	case "bot unstage":
		return botUnstage(ctx, service, rest, stdout, stderr)
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
	case "group topics":
		return groupTopics(ctx, service, rest, stdout, stderr)
	case "group links":
		return groupLinks(ctx, service, rest, stdout, stderr)
	case "group publish":
		return groupPublish(ctx, service, rest, stdout, stderr)
	case "incident open":
		return incidentOpen(ctx, service, rest, stdout, stderr)
	case "incident list":
		return incidentList(ctx, service, rest, stdout, stderr)
	case "incident get":
		return incidentGet(ctx, service, rest, stdout, stderr)
	case "incident diagnosis":
		return incidentDiagnosis(ctx, service, *actor, rest, stdout, stderr)
	case "autonomy list":
		return autonomyList(ctx, service, rest, stdout, stderr)
	case "autonomy get":
		return autonomyGet(ctx, service, rest, stdout, stderr)
	case "autonomy lower":
		return autonomyLower(ctx, service, *actor, rest, stdout, stderr)
	case "autonomy clear":
		return autonomyClear(ctx, service, *actor, rest, stdout, stderr)
	case "incident resolve":
		return incidentResolve(ctx, service, rest, stdout, stderr)
	case "incident postmortem":
		return incidentPostmortem(ctx, service, *actor, rest, stdout, stderr)
	case "output add":
		return outputAdd(ctx, service, rest, stdout, stderr)
	case "output list":
		return outputList(ctx, service, rest, stdout, stderr)
	case "output remove":
		return outputRemove(ctx, service, rest, stdout, stderr)
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
		return taskDecide(ctx, service, *actor, rest, stdout, stderr)
	case "task approve":
		return taskDecideAs(ctx, service, *actor, application.DecisionApprove, rest, stdout, stderr)
	case "task deny":
		return taskDecideAs(ctx, service, *actor, application.DecisionDeny, rest, stdout, stderr)
	case "task get":
		return taskGet(ctx, service, rest, stdout, stderr)
	case "task report":
		return taskReport(ctx, service, rest, stdout, stderr)
	case "execution get":
		return executionGet(ctx, service, rest, stdout, stderr)
	case "execution artifact":
		return executionArtifact(ctx, service, rest, stdout, stderr)
	case "execution transcripts":
		return executionTranscripts(ctx, service, rest, stdout, stderr)
	case "execution transcript":
		return executionTranscript(ctx, service, rest, stdout, stderr)
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
       axlr-bots --control https://host:7879 --token-file <path> [--ca-file <pem>] <command>
                                      the same commands against a control plane on another
                                      host, through its operator address

  serve [--listen <addr>] [--server-names a,b] [--api-listen <addr>] [--api-token-file <path>]
        [--bus direct|nats] [--nats-url <url>] [--nats-creds <file>]
        [--nats-cert <file> --nats-key <file>] [--nats-ca <file>]
        [--tick 2s] [--reconcile-every 1m] [--min-axlr-version 0.20.0]
                                      run the control plane: the API on the socket, the node
                                      protocol over TLS on --listen (nodes on other hosts), the
                                      operator API over TLS on --api-listen behind a bearer
                                      token (<state>/api.token), executions handed to the
                                      nodes, in-process runs when no node is online, and the
                                      timer triggers
  ca init                             create the fleet CA under <state>/ca (once per fleet)
  ca show                             print the CA certificate and its fingerprint
  ca issue-client --name <cn> --out <dir>
                                      a client certificate of the fleet CA for one of its own services
                                      (the control plane's broker connection): client.key, client.crt, ca.crt
  node list                           list the nodes (hosts running axlr-node)
  node get <node-id>                  show one node
  node enroll-token [--node-id <id>] [--labels k=v,k=v] [--ttl 1h]
                                      print a one-time token a new host enrolls with
  node revoke <node-id>               refuse the node's certificate and fail what it ran
  bot register --file <bot.json>      register a bot from its definition
  bot enable <bot-id>                 let the bot accept tasks
  bot pause <bot-id>                  put an enabled bot on hold; it accepts no task
  bot stop <bot-id>                   pause it and cancel every task it has not finished
  bot delete <bot-id>                 delete it: cancel its unfinished tasks and the children it
                                      asked for, resolve its incidents, remove its timers; its
                                      history stays and the answer names the bots that name it
  bot list                            list bots
  bot get <bot-id>                    show one bot
  bot update <bot-id> --file <patch.json>
                                      replace the fields the patch names (mission, level,
                                      allowedTools, toolRules, triggers, placement, approvals,
                                      autoApprove, delegation, ask, groups, events, subscriptions,
                                      model, budgets, runtime, skills, version)
  bot stage <bot-id> --file <patch.json>
                                      hold a patch naming its version for later, checked as an
                                      update would be now; replaces the one staged before
  bot staged <bot-id>                 the change it holds staged, secrets redacted
  bot apply-staged <bot-id>           apply the staged change as an update and drop it
  bot unstage <bot-id>                drop the staged change unapplied
  bot events <bot-id> [--limit 100]   the bot's newest events, oldest first
  bot subscribe <bot-id> <topic> [--mode task|message|note] [--from a,b]
                                      make the bot listen to a topic of its teams
  bot unsubscribe <bot-id> <topic>    stop it listening, or drop its pending ask
  bot subscriptions <bot-id>          the topics it listens to; a pending ask is marked
  group list                          list the teams the bots name, with their members
  group get <group> [--tenant <id>]   show one team: each member's purpose, tools, where it runs
  group events <group> [--after <event-id>] [--limit 100] [--tenant <id>]
                                      the team's timeline: its members' events, oldest first
  group topics <group> [--tenant <id>]
                                      the team's topics: who publishes, who listens, nobody listens
  group links <group> [--tenant <id>]
                                      the team channel's links from and to the team: who asks, who answers
  group publish <group> <name> --message "<text>" [--data '{…}'] [--to a,b] [--key <k>] [--tenant <id>]
                                      publish the operator's event on the team; stop.all stops everyone
                                      and drops their queued tasks
  incident open --tenant <id> --bot <bot-id> --group-key "<key>" --title "<text>" [--severity sev1..sev4]
                [--namespace <ns>] [--fingerprints a,b] [--source <name>] [--objective "<text>"]
                                      report an alert group: a new incident and its triage task,
                                      a repeat of the one open for that key, or a reopening
  incident postmortem show <incident-id>
                                      print a resolved incident's postmortem as Markdown
  incident postmortem approve <incident-id> --author-task <task-id>
                                      approve the text you read as --actor; the outputs announce it
  incident postmortem send-back <incident-id> --note "<text>" [--author-task <task-id>]
                                      send the text you read back, as --actor; the second send-back of
                                      a text leaves it to a person. With no postmortem, asks for one
  incident list [--tenant <id>] [--status Open|Diagnosed|Proposed|Resolved|Escalated] [--limit 100]
                                      the incidents, the last seen first
  incident get <incident-id>          show one incident: its alerts, tasks, diagnosis, cost
  incident diagnosis <incident-id> correct|wrong
                                      mark the diagnosis of the current cycle, as --actor
  autonomy list [--tenant <id>] [--bot <id>] [--runbook <name>] [--namespace <ns>]
  autonomy get <autonomy-id>          a runbook's step, counters and history
  autonomy lower <autonomy-id> [--reason "<text>"]
                                      take a runbook one step down, as --actor
  autonomy clear <autonomy-id> [--reason "<text>"]
                                      end its outstanding applies, as --actor
  incident resolve <incident-id> [--reason "<text>"]
                                      close it; a new alert within 30 minutes reopens it
  output add <name> --type webhook|slack|command [--url <url> | --url-file <file>|-] [--secret-file <file>|-]
             [--events opened,diagnosed,…] [--tenants a,b] [--severities sev1,sev2] [--panel-url <url>]
             [--file <output.json>] [-- <program> <args>…]
                                      announce incidents to a signed webhook, a Slack incoming
                                      webhook or a local command (see docs/runbooks/outputs.md)
  output list                         the output targets: name, type, filters, a fingerprint;
                                      never the URL, the secret or the command
  output remove <name>                drop a target; its pending deliveries are skipped
  task submit <bot-id> --objective "<text>" --key <idempotency-key> [--correlation <id>] [--urgent]
                                      accept a task; the same key names the same task; --urgent
                                      goes first, cancels the bot's work in progress and drops
                                      its queued tasks
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
                                      task timeout plus 1m); prints failed/skipped.
                                      Works on the files and takes the state
                                      directory's lock: while serve runs (and
                                      reconciles by itself) it is refused
  execution get <execution-id>        show one execution: outcome, answer, usage, policy,
                                      artifacts and receipt
  execution artifact <execution-id> <name> [--out <file>]
  execution transcripts [--after <seq>] [--limit <n>]
  execution transcript <execution-id> <session> [--jsonl] [--out <file>]
                                      an artifact the control plane keeps, checked
                                      against its SHA-256
  overview [--since <time>]           nodes, bots and tasks by status; tasks, executions
                                      and cost since the start of today
  health                              exit 0 when the daemon answers /healthz on the socket
                                      (a container's healthcheck), 1 when it does not
  version                             the release, commit and time this binary was built from
  github-hooks serve --secret-file <path> --team <team> --ci-event <name> --merged-event <name>
                     --repos a/b,c/d [--branch-prefix <prefix>] [--listen 127.0.0.1:7890] [--public]
                                      the GitHub webhook adapter: POST /github, signed with the hook's
                                      secret, publishes --ci-event and --merged-event to --team through the
                                      daemon's socket (see docs/runbooks/github-hooks.md)
  github-hooks redeliver --repos a/b,c/d [--since 24h] [--hook-url <url>] [--gh <path>]
                                      ask GitHub, through gh api, to redeliver each failed delivery of
                                      the /github hooks that never succeeded since; prints them
  activity-audit serve --team <team> --event <name> [--ignore-programs a,b] [--ignore-team-partial=true] [--since <RFC3339>]
                       [--store <dir>] [--rearm id,b|all] [--per-minute 6] [--dry-run] [--once] [--wait 30s]
                                      the activity audit adapter: when each task ends, reads its
                                      executions and timeline through the daemon's socket and
                                      publishes each new failure signature once, as the event --event,
                                      to the team --team (see docs/runbooks/activity-audit.md)
  activity-audit report [--store <dir>] [--json]
                                      the signatures seen: published, count, bots, what failed
  transcript-audit serve --team <team> --event <name> [--store <dir>] [--model jev-1.13.0] [--interval 30s] [--once] [--dry-run]
                                      ask Jev of every block of every execution's whole transcript
                                      and tell --team of those that hide a failure; TYPESAFE_API_KEY
                                      (see docs/runbooks/transcript-audit.md)
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
