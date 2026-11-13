package daemon

import "github.com/underpass-ai/axlr-bots/application"

// levelLogger writes what happened to info and what went wrong to
// errors; with the text format both are the same logger.
type levelLogger struct {
	info   application.LogPrinter
	errors application.LogPrinter
}

// Printf is a line of what happened.
func (l levelLogger) Printf(format string, args ...any) { l.info.Printf(format, args...) }

// Errorf is a line of what went wrong.
func (l levelLogger) Errorf(format string, args ...any) { l.errors.Printf(format, args...) }

// With is a logger whose lines, of both levels, are about the subject.
func (l levelLogger) With(subject application.LogSubject) application.LogPrinter {
	return levelLogger{info: application.LogWith(l.info, subject), errors: application.LogWith(l.errors, subject)}
}

// errorf writes the line at the error level when the logger has one,
// and as a plain line when it does not (a test's).
func errorf(logger interface{ Printf(string, ...any) }, format string, args ...any) {
	if leveled, ok := logger.(interface{ Errorf(string, ...any) }); ok {
		leveled.Errorf(format, args...)
		return
	}
	logger.Printf(format, args...)
}
