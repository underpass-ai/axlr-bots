package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/underpass-ai/axlr-bots/application"
	"io"
	"time"

	"github.com/underpass-ai/axlr-bots/application/taskactivity"
)

// taskActivity is task activity <id>: everything that happened under a
// task, its delegated tasks included, as text or --json; --watch follows
// it live until the whole tree has finished.
func taskActivity(ctx context.Context, service application.OperatorService, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || args[0] == "" || args[0][0] == '-' {
		fmt.Fprintln(stderr, "axlr-bots: task activity needs <task-id> first")
		return exitUsage
	}
	taskID, rest := args[0], args[1:]
	flags := flag.NewFlagSet("task activity", flag.ContinueOnError)
	flags.SetOutput(stderr)
	asJSON := flags.Bool("json", false, "print the tree as JSON (with --watch, one line per change)")
	depth := flags.Int("depth", 0, "how many levels of delegated tasks to follow (default: the delegation limit)")
	watch := flags.Bool("watch", false, "follow the tree live until every task in it has finished")
	if err := flags.Parse(rest); err != nil {
		return exitUsage
	}
	if flags.NArg() != 0 || *depth < 0 {
		fmt.Fprintln(stderr, "axlr-bots: task activity takes --json, --depth N (not negative) and --watch only")
		return exitUsage
	}
	reader, ok := service.(taskactivity.Reader)
	if !ok {
		fmt.Fprintln(stderr, "axlr-bots: task activity reads the control plane's activity over gRPC: run it against the daemon (serve, with its gRPC socket) or with --control")
		return exitError
	}
	options := taskactivity.Options{MaxDepth: *depth}
	if !*watch {
		activity, err := reader.Get(ctx, taskID, options)
		if err != nil {
			return fail(stderr, err)
		}
		if *asJSON {
			printJSON(stdout, activity)
		} else {
			printActivity(stdout, activity, false)
		}
		return exitOK
	}
	var tree taskactivity.Activity
	err := reader.Watch(ctx, taskID, options, "", func(_ string, update taskactivity.Update) error {
		if update.Heartbeat() {
			return nil
		}
		tree.Apply(update)
		if !*asJSON && update.Snapshot == nil {
			fmt.Fprintf(stdout, "\n--- %s ---\n", stamp(time.Now()))
		}
		printActivity(stdout, tree, *asJSON)
		return nil
	})
	if err != nil && !errors.Is(err, context.Canceled) {
		return fail(stderr, err)
	}
	return exitOK
}

// printActivity writes the tree as text, or as JSON on one line, the
// form of a followed tree.
func printActivity(w io.Writer, activity taskactivity.Activity, asJSON bool) {
	if !asJSON {
		activityRenderer{out: w}.render(activity)
		return
	}
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(activity); err != nil {
		fmt.Fprintln(w, "{}")
		return
	}
	_, _ = w.Write(buf.Bytes())
}
