package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/underpass-ai/axlr-bots/application"
	"io"
	"os"
)

// executionTranscripts lists the transcripts the control plane keeps,
// oldest first, after --after (docs/architecture/transcripts.md).
func executionTranscripts(ctx context.Context, service application.OperatorService, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("execution transcripts", flag.ContinueOnError)
	flags.SetOutput(stderr)
	after := flags.Int64("after", 0, "only the transcripts after this seq")
	limit := flags.Int("limit", 0, "at most this many (default and bound 500)")
	positional, ok := parseAnywhere(flags, args)
	if !ok {
		return exitUsage
	}
	if len(positional) != 0 {
		fmt.Fprintln(stderr, "axlr-bots: execution transcripts takes no arguments")
		return exitUsage
	}
	records, err := service.ListTranscripts(ctx, *after, *limit)
	if err != nil {
		return fail(stderr, err)
	}
	printJSON(stdout, records)
	return exitOK
}

// executionTranscript writes one transcript whole, as the node sent it,
// to stdout or --out; --jsonl writes one line per message of the session
// instead.
func executionTranscript(ctx context.Context, service application.OperatorService, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("execution transcript", flag.ContinueOnError)
	flags.SetOutput(stderr)
	out := flags.String("out", "", "write the transcript to this file instead of stdout")
	lines := flags.Bool("jsonl", false, "one line per message of the session (JSON Lines)")
	positional, ok := parseAnywhere(flags, args)
	if !ok {
		return exitUsage
	}
	if len(positional) != 2 {
		fmt.Fprintln(stderr, "axlr-bots: execution transcript needs <execution-id> and <session>")
		return exitUsage
	}
	content, err := service.GetTranscript(ctx, positional[0], positional[1])
	if err != nil {
		return fail(stderr, err)
	}
	if *lines {
		if content, err = jsonLines(content); err != nil {
			return fail(stderr, err)
		}
	}
	sink := stdout
	if *out != "" {
		file, err := os.Create(*out)
		if err != nil {
			return fail(stderr, err)
		}
		defer file.Close()
		sink = file
	}
	if _, err := sink.Write(content); err != nil {
		return fail(stderr, err)
	}
	return exitOK
}

// jsonLines turns a session ({"messages": [...]}) into one compact line
// per message.
func jsonLines(content []byte) ([]byte, error) {
	var session struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(content, &session); err != nil {
		return nil, fmt.Errorf("the transcript is not a session with messages: %w", err)
	}
	var b bytes.Buffer
	for _, message := range session.Messages {
		if err := json.Compact(&b, message); err != nil {
			return nil, err
		}
		b.WriteByte('\n')
	}
	return b.Bytes(), nil
}
