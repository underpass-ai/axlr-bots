package main

import (
	"flag"
	"io"
	"log"
	"os"

	"github.com/underpass-ai/axlr-bots/adapters/logline"
)

// logFormatFlag adds --log-format to a long-running command: text (the
// default) or json, one object per line for Loki.
func logFormatFlag(flags *flag.FlagSet) *string {
	return flags.String("log-format", os.Getenv("AXLR_BOTS_LOG_FORMAT"), "the log lines: text, or json for one object per line with level, component and, for a line about a task or execution, bot, node, task, execution and correlationId (also AXLR_BOTS_LOG_FORMAT; default text)")
}

// newLoggers is the command's loggers in the format; with json the
// standard logger, which the file store writes to, goes the same way.
func newLoggers(stderr io.Writer, format string) (logline.Loggers, error) {
	loggers, err := logline.New(stderr, format)
	if err != nil {
		return logline.Loggers{}, err
	}
	if format == logline.FormatJSON {
		log.SetFlags(0)
		log.SetOutput(loggers.Info.Writer())
	}
	return loggers, nil
}
