package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/jev"
	"github.com/underpass-ai/axlr-bots/adapters/transcriptaudit"
	"github.com/underpass-ai/axlr-bots/internal/statelock"
)

// transcriptAuditDir is the adapter's own directory under the state
// directory: its verdicts and its lock.
const transcriptAuditDir = "transcript-audit"

// transcriptAudit runs the transcript audit adapter
// (docs/runbooks/transcript-audit.md): it reads and publishes through the
// daemon's socket, or its operator address with --control, and asks Jev
// with TYPESAFE_API_KEY.
func transcriptAudit(stateDir, socketPath string, remote remoteControl, args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	if len(args) == 0 || args[0] != "serve" {
		fmt.Fprintln(stderr, "axlr-bots: transcript-audit needs serve")
		return exitUsage
	}
	return transcriptAuditServe(ctx, stateDir, socketPath, remote, args[1:], jevJudgeFor(os.Getenv("TYPESAFE_API_KEY")), stdout, stderr)
}

// judgeFactory builds the judge for a pinned model and answers the model
// it asks.
type judgeFactory func(model string) (transcriptaudit.Judge, string, error)

// jevJudgeFor is the factory of Jev's client with the key.
func jevJudgeFor(key string) judgeFactory {
	return func(model string) (transcriptaudit.Judge, string, error) {
		client, err := jev.NewClient(jev.ClientConfig{APIKey: key, Model: model})
		if err != nil {
			return nil, "", err
		}
		return jevJudge{client: client}, client.Model(), nil
	}
}

// transcriptAuditServe audits every transcript after its cursor, then
// every --interval; --once stops after one round, --dry-run publishes
// nothing.
func transcriptAuditServe(ctx context.Context, stateDir, socketPath string, remote remoteControl, args []string, newJudge judgeFactory, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("transcript-audit serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	logFormat := logFormatFlag(flags)
	team := flags.String("team", "", "the team the events are published to (required)")
	event := flags.String("event", "", "the name of the event published for a transcript that hides a failure, such as transcripcion.fallo (required)")
	storeDir := flags.String("store", "", "the adapter's directory (default <state>/transcript-audit)")
	model := flags.String("model", jev.DefaultModel, "the pinned Jev version")
	interval := flags.Duration("interval", 30*time.Second, "how long to wait between rounds")
	once := flags.Bool("once", false, "stop after one round")
	dryRun := flags.Bool("dry-run", false, "record the verdicts, publish nothing")
	positional, ok := parseAnywhere(flags, args)
	if !ok {
		return exitUsage
	}
	if len(positional) != 0 || *team == "" || *event == "" {
		fmt.Fprintln(stderr, "axlr-bots: transcript-audit serve needs --team <team> and --event <name>, and takes no arguments")
		return exitUsage
	}
	if socketPath == "" && !remote.enabled() {
		return fail(stderr, errors.New("transcript-audit serve reads and publishes through the daemon's socket; --socket is empty"))
	}
	judge, pinned, err := newJudge(*model)
	if err != nil {
		return fail(stderr, err)
	}
	dir := *storeDir
	if dir == "" {
		dir = filepath.Join(stateDir, transcriptAuditDir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fail(stderr, err)
	}
	release, err := statelock.Acquire(dir)
	if err != nil {
		return fail(stderr, err)
	}
	defer release()
	verdicts, err := transcriptaudit.OpenVerdictLog(filepath.Join(dir, transcriptaudit.VerdictFileName))
	if err != nil {
		return fail(stderr, err)
	}
	loggers, err := newLoggers(stderr, *logFormat)
	if err != nil {
		return fail(stderr, err)
	}
	api, err := remote.clientOr(socketPath)
	if err != nil {
		return fail(stderr, err)
	}
	config := transcriptaudit.AuditorConfig{Source: api, Publisher: api, Judge: judge, Log: verdicts, Team: *team, Event: *event, Model: pinned, Logger: loggers.Info}
	if *dryRun {
		config.Publisher = nil
	}
	auditor, err := transcriptaudit.NewAuditor(config)
	if err != nil {
		return fail(stderr, err)
	}
	loggers.Info.Printf("transcript-audit: auditing the transcripts through %s with %s, publishing %s to team %s; verdicts in %s, cursor %d", remote.where(socketPath), pinned, *event, *team, dir, verdicts.Cursor())
	if err := auditor.Run(ctx, *interval, *once); err != nil {
		return fail(stderr, err)
	}
	if *once {
		fmt.Fprintf(stdout, "cursor %d\n", verdicts.Cursor())
	}
	return exitOK
}
