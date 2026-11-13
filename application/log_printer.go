package application

// LogPrinter is where an adapter writes its log lines. *log.Logger is
// one; so is the JSON logger of the logline adapter.
type LogPrinter interface {
	Printf(format string, args ...any)
}

// SubjectPrinter is a LogPrinter that can say who its lines are about.
type SubjectPrinter interface {
	LogPrinter
	// With answers a printer whose lines carry the subject, laid over the
	// one it already had.
	With(subject LogSubject) LogPrinter
}

// LogWith is printer whose lines carry the subject, when printer can
// carry one (the JSON format); a plain printer (the text format, a
// test's) is returned as it is, so the line reads the same.
func LogWith(printer LogPrinter, subject LogSubject) LogPrinter {
	if scoped, ok := printer.(SubjectPrinter); ok {
		return scoped.With(subject)
	}
	return printer
}
