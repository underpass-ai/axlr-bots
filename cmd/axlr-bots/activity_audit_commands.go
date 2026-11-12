package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/activityaudit"
	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/internal/statelock"
)

// activityAuditDir is the adapter's own directory under the state
// directory: its seen-store and its lock, nothing of the control plane's.
const activityAuditDir = "activity-audit"

// activityAudit runs the activity audit adapter
// (docs/runbooks/activity-audit.md). It reads and publishes through the
// daemon's socket only; of the state directory it touches its own
// subdirectory.
func activityAudit(stateDir, socketPath string, args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	verb := ""
	if len(args) > 0 {
		verb, args = args[0], args[1:]
	}
	switch verb {
	case "serve":
		return activityAuditServe(ctx, stateDir, socketPath, args, stdout, stderr)
	case "report":
		return activityAuditReport(stateDir, args, stdout, stderr)
	}
	fmt.Fprintln(stderr, "axlr-bots: activity-audit needs serve or report")
	return exitUsage
}

// activityAuditServe audits each task when it ends until ctx ends; with
// --once, the replay of --since only; with --dry-run, publishing nothing
// and keeping nothing, and printing what it would have published.
func activityAuditServe(ctx context.Context, stateDir, socketPath string, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("activity-audit serve", flag.ContinueOnError)
	flags.SetOutput(stderr)
	team := flags.String("team", "", "the team the events are published to (required)")
	event := flags.String("event", "", "the name of the event published for each new failure signature, such as activity.failure (required)")
	ignore := flags.String("ignore-programs", "", "local programs whose failures are not audited, basenames comma-separated (default none)")
	teamPartial := flags.Bool("ignore-team-partial", true, "also ignore a local_exec whose line hid its program when its bot is in --team")
	sinceText := flags.String("since", "", "on a first run (a seen-store without a cursor), replay the tasks created from this RFC 3339 time")
	storeDir := flags.String("store", "", "the adapter's directory (default <state>/activity-audit)")
	rearm := flags.String("rearm", "", "signature ids to publish again on their next occurrence, comma-separated, or all")
	dryRun := flags.Bool("dry-run", false, "publish nothing and keep nothing: start from an empty seen-store in memory and print what would be published")
	once := flags.Bool("once", false, "stop after the start's reconciliation (the replay of --since) instead of following the teams")
	perMinute := flags.Int("per-minute", activityaudit.DefaultPerMinute, "publish at most this many events a minute, oldest first (0: no bound)")
	wait := flags.Duration("wait", activityaudit.DefaultWait, "how long one long poll of a team's timeline waits")
	positional, ok := parseAnywhere(flags, args)
	if !ok {
		return exitUsage
	}
	if len(positional) != 0 || *team == "" || *event == "" {
		fmt.Fprintln(stderr, "axlr-bots: activity-audit serve needs --team <team> and --event <name>, and takes no arguments")
		return exitUsage
	}
	var since time.Time
	if *sinceText != "" {
		parsed, err := time.Parse(time.RFC3339Nano, *sinceText)
		if err != nil {
			return fail(stderr, fmt.Errorf("--since %s: %w", *sinceText, err))
		}
		since = parsed
	}
	if socketPath == "" {
		return fail(stderr, errors.New("activity-audit serve reads and publishes through the daemon's socket; --socket is empty"))
	}
	dir := activityAuditStore(stateDir, *storeDir)
	path := filepath.Join(dir, activityaudit.SeenFileName)
	if *dryRun {
		path = ""
	} else {
		release, err := statelock.Acquire(dir)
		if err != nil {
			return fail(stderr, err)
		}
		defer release()
	}
	store, err := activityaudit.OpenSeenStore(path)
	if err != nil {
		return fail(stderr, err)
	}
	if *rearm != "" {
		rearmed, err := store.Rearm(splitList(*rearm))
		if err != nil {
			return fail(stderr, err)
		}
		if err := store.Save(); err != nil {
			return fail(stderr, err)
		}
		fmt.Fprintf(stderr, "activity-audit: rearmed %s\n", strings.Join(rearmed, ","))
	}
	logger := log.New(stderr, "", log.LstdFlags)
	client := unixapi.NewClient(socketPath)
	config := activityaudit.AuditorConfig{Source: client, Publisher: client, Store: store, Team: *team, Event: *event, IgnorePrograms: splitList(*ignore), IgnoreTeamPartial: *teamPartial, PerMinute: *perMinute, Log: logger}
	if *dryRun {
		config.Publisher = nil
	}
	auditor, err := activityaudit.NewAuditor(config)
	if err != nil {
		return fail(stderr, err)
	}
	watcher, err := activityaudit.NewWatcher(activityaudit.WatcherConfig{Source: client, Auditor: auditor, Wait: *wait, Once: *once, Log: logger})
	if err != nil {
		return fail(stderr, err)
	}
	where := path
	if *dryRun {
		where = "memory (dry run)"
	}
	logger.Printf("activity-audit: auditing the fleet through %s, publishing %s to team %s; seen-store %s", socketPath, *event, *team, where)
	err = watcher.Run(ctx, since)
	if *dryRun {
		var due []activityaudit.SeenSignature
		for _, signature := range store.Signatures() {
			if signature.PublishedAt != "" {
				due = append(due, signature)
			}
		}
		fmt.Fprintf(stdout, "would publish %d of %d signatures\n", len(due), len(store.Signatures()))
		if reportErr := activityaudit.Report(stdout, store.Signatures()); reportErr != nil && err == nil {
			err = reportErr
		}
	}
	if err != nil {
		return fail(stderr, err)
	}
	logger.Printf("activity-audit: stopped")
	return exitOK
}

// activityAuditReport prints the seen-store as a table, or as JSON.
func activityAuditReport(stateDir string, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("activity-audit report", flag.ContinueOnError)
	flags.SetOutput(stderr)
	storeDir := flags.String("store", "", "the adapter's directory (default <state>/activity-audit)")
	asJSON := flags.Bool("json", false, "print the signatures as JSON")
	positional, ok := parseAnywhere(flags, args)
	if !ok {
		return exitUsage
	}
	if len(positional) != 0 {
		fmt.Fprintln(stderr, "axlr-bots: activity-audit report takes no arguments")
		return exitUsage
	}
	store, err := activityaudit.OpenSeenStore(filepath.Join(activityAuditStore(stateDir, *storeDir), activityaudit.SeenFileName))
	if err != nil {
		return fail(stderr, err)
	}
	if *asJSON {
		printJSON(stdout, store.Signatures())
		return exitOK
	}
	if err := activityaudit.Report(stdout, store.Signatures()); err != nil {
		return fail(stderr, err)
	}
	return exitOK
}

// activityAuditStore is the adapter's directory: --store, else its own
// under the state directory.
func activityAuditStore(stateDir, store string) string {
	if store != "" {
		return store
	}
	return filepath.Join(stateDir, activityAuditDir)
}

// splitList is a comma-separated list, its blanks dropped.
func splitList(text string) []string {
	var items []string
	for _, item := range strings.Split(text, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}
