package logline

import (
	"context"
	"log/slog"
	"regexp"
)

var (
	componentPattern = regexp.MustCompile(`^([a-z][a-z0-9-]*): `)
	idPatterns       = []struct {
		key     string
		pattern *regexp.Regexp
	}{
		{"task", regexp.MustCompile(`\btask-[0-9a-f]{8,}\b`)},
		{"execution", regexp.MustCompile(`\bexec-[0-9a-f]{8,}\b`)},
	}
)

// FieldsHandler adds to each record the fields its message names: the
// component ("daemon: …" → component=daemon) and the first task and
// execution id in it, so Loki can filter on them.
type FieldsHandler struct {
	next slog.Handler
}

// NewFieldsHandler hands its records, with the fields added, to next.
func NewFieldsHandler(next slog.Handler) FieldsHandler { return FieldsHandler{next: next} }

// Enabled is next's.
func (h FieldsHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle adds the fields and hands the record on.
func (h FieldsHandler) Handle(ctx context.Context, record slog.Record) error {
	if match := componentPattern.FindStringSubmatch(record.Message); match != nil {
		record.AddAttrs(slog.String("component", match[1]))
	}
	for _, id := range idPatterns {
		if found := id.pattern.FindString(record.Message); found != "" {
			record.AddAttrs(slog.String(id.key, found))
		}
	}
	return h.next.Handle(ctx, record)
}

// WithAttrs is next's, with the fields still added.
func (h FieldsHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return FieldsHandler{next: h.next.WithAttrs(attrs)}
}

// WithGroup is next's, with the fields still added.
func (h FieldsHandler) WithGroup(name string) slog.Handler {
	return FieldsHandler{next: h.next.WithGroup(name)}
}
