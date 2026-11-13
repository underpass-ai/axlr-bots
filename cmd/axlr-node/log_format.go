package main

import (
	"flag"
	"io"

	"github.com/underpass-ai/axlr-bots/adapters/logline"
)

// logFormatFlag adds --log-format to the run command: text (the
// default) or json, one object per line for Loki.
func logFormatFlag(flags *flag.FlagSet) *string {
	return flags.String("log-format", envOr("AXLR_NODE_LOG_FORMAT", ""), "the log lines: text, or json for one object per line with level, component and, for a line about an execution, node, bot, task, execution and correlationId as fields (also AXLR_NODE_LOG_FORMAT; default text)")
}

// newLogger is the node's logger in the format.
func newLogger(stderr io.Writer, format string) (logline.Logger, error) {
	loggers, err := logline.New(stderr, format)
	return loggers.Info, err
}
