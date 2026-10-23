package axlr

import (
	"bytes"
	"strings"
)

// tailWriter keeps the last limit bytes written to it, so a failure can
// quote the runtime's last line of stderr.
type tailWriter struct {
	limit int
	buf   []byte
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	if len(w.buf) > w.limit {
		w.buf = w.buf[len(w.buf)-w.limit:]
	}
	return len(p), nil
}

// lastLine is the last non-empty line seen, trimmed.
func (w *tailWriter) lastLine() string {
	lines := bytes.Split(w.buf, []byte{'\n'})
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(string(lines[i])); line != "" {
			return line
		}
	}
	return ""
}
