package logline

import (
	"fmt"
	"io"
	"log"
	"log/slog"
)

// Formats of the log lines.
const (
	FormatText = "text"
	FormatJSON = "json"
)

// Loggers are the two loggers a command hands its adapters: Info for
// what happened, Error for what went wrong. In the text format both
// write the same plain lines; in JSON they write level INFO and ERROR.
type Loggers struct {
	Info  *log.Logger
	Error *log.Logger
}

// New is the loggers writing to w in the format ("" is text).
func New(w io.Writer, format string) (Loggers, error) {
	switch format {
	case "", FormatText:
		plain := log.New(w, "", log.LstdFlags)
		return Loggers{Info: plain, Error: plain}, nil
	case FormatJSON:
		handler := NewFieldsHandler(slog.NewJSONHandler(w, nil))
		return Loggers{Info: slog.NewLogLogger(handler, slog.LevelInfo), Error: slog.NewLogLogger(handler, slog.LevelError)}, nil
	default:
		return Loggers{}, fmt.Errorf("log format %q: want text or json", format)
	}
}
