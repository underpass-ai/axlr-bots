package logline

import (
	"context"
	"fmt"
	"io"
	"log"
	"log/slog"
	"strings"

	"github.com/underpass-ai/axlr-bots/application"
)

// Logger writes the lines of one level. In the text format it is the
// plain standard logger and a subject changes nothing; in JSON each
// line is an object and the subject's ids are fields of it.
type Logger struct {
	plain   *log.Logger
	json    *slog.Logger
	level   slog.Level
	subject application.LogSubject
}

func plainLogger(plain *log.Logger) Logger { return Logger{plain: plain} }

func jsonLogger(handler slog.Handler, level slog.Level) Logger {
	return Logger{json: slog.New(handler), level: level}
}

// Printf is a line, with the fields of the logger's subject in JSON.
func (l Logger) Printf(format string, args ...any) {
	if l.plain != nil {
		l.plain.Printf(format, args...)
		return
	}
	if l.json == nil {
		return
	}
	message := strings.TrimSuffix(fmt.Sprintf(format, args...), "\n")
	l.json.LogAttrs(context.Background(), l.level, message, subjectAttrs(l.subject)...)
}

// With is a logger whose lines are about the subject, laid over this
// one's.
func (l Logger) With(subject application.LogSubject) application.LogPrinter {
	l.subject = l.subject.Over(subject)
	return l
}

// Writer is the writer the standard logger can write to so its lines go
// the same way: through Printf's level and format.
func (l Logger) Writer() io.Writer {
	if l.plain != nil {
		return l.plain.Writer()
	}
	return slog.NewLogLogger(l.json.Handler(), l.level).Writer()
}
