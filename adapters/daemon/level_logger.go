package daemon

import "log"

// levelLogger writes what happened to info and what went wrong to
// errors; with the text format both are the same logger.
type levelLogger struct {
	info   *log.Logger
	errors *log.Logger
}

// Printf is a line of what happened.
func (l levelLogger) Printf(format string, args ...any) { l.info.Printf(format, args...) }

// Errorf is a line of what went wrong.
func (l levelLogger) Errorf(format string, args ...any) { l.errors.Printf(format, args...) }

// errorf writes the line at the error level when the logger has one,
// and as a plain line when it does not (a test's).
func errorf(logger interface{ Printf(string, ...any) }, format string, args ...any) {
	if leveled, ok := logger.(interface{ Errorf(string, ...any) }); ok {
		leveled.Errorf(format, args...)
		return
	}
	logger.Printf(format, args...)
}
