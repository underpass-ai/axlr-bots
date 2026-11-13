package logline

import (
	"log/slog"

	"github.com/underpass-ai/axlr-bots/application"
)

// The names of the fields a subject sets.
const (
	KeyBot           = "bot"
	KeyNode          = "node"
	KeyTask          = "task"
	KeyExecution     = "execution"
	KeyCorrelationID = "correlationId"
)

// subjectAttrs are the fields of the subject that are set.
func subjectAttrs(s application.LogSubject) []slog.Attr {
	var attrs []slog.Attr
	add := func(key, value string) {
		if value != "" {
			attrs = append(attrs, slog.String(key, value))
		}
	}
	add(KeyBot, s.Bot.String())
	add(KeyNode, s.Node.String())
	add(KeyTask, s.Task.String())
	add(KeyExecution, s.Execution.String())
	add(KeyCorrelationID, s.CorrelationID.String())
	return attrs
}
