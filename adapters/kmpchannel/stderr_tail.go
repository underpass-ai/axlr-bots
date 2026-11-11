package kmpchannel

import (
	"bytes"
	"strings"
)

// tail keeps the last bytes the server wrote on stderr, for an error.
type tail struct{ buf bytes.Buffer }

func (t *tail) Write(p []byte) (int, error) {
	t.buf.Write(p)
	if over := t.buf.Len() - 2048; over > 0 {
		t.buf.Next(over)
	}
	return len(p), nil
}

// last is the last line of what was kept.
func (t *tail) last() string {
	lines := strings.Split(strings.TrimSpace(t.buf.String()), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
