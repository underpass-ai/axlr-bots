package transcriptaudit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxStateBytes is the most Jev takes as one state (adapters/jev's
// MaxStateBytes): a block, its header included, never exceeds it.
const MaxStateBytes = 24 << 10

// pieceRoom is what a piece's label may take.
const pieceRoom = 300

// sessionLines turns a session ({"messages": [...]}) into one line per
// message, the first left out: the mission and the playbook the bot was
// given, instructions, not activity. A tool's result is labelled with the
// call it answers.
func sessionLines(content []byte) ([]line, error) {
	var session struct {
		Messages []json.RawMessage `json:"messages"`
	}
	if err := json.Unmarshal(content, &session); err != nil {
		return nil, fmt.Errorf("not a session with messages: %w", err)
	}
	if len(session.Messages) == 0 {
		return nil, fmt.Errorf("not a session with messages: none")
	}
	calls := map[string]string{}
	lines := make([]line, 0, len(session.Messages))
	for _, raw := range session.Messages[1:] {
		var message struct {
			Role       string `json:"role"`
			ToolCallID string `json:"tool_call_id"`
			ToolCalls  []struct {
				ID        string          `json:"id"`
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"tool_calls"`
		}
		_ = json.Unmarshal(raw, &message)
		for _, call := range message.ToolCalls {
			calls[call.ID] = callLabel(call.Name, call.Arguments)
		}
		var compact bytes.Buffer
		if err := json.Compact(&compact, raw); err != nil {
			return nil, err
		}
		label := ""
		if message.Role == "tool" {
			label = calls[message.ToolCallID]
		}
		lines = append(lines, line{text: compact.String(), label: label})
	}
	return lines, nil
}

// callLabel names a call by its tool and what it named: a path, or a
// program's basename and its arguments; at most 160 bytes.
func callLabel(name string, arguments json.RawMessage) string {
	var args struct {
		Path    string   `json:"path"`
		Program string   `json:"program"`
		Args    []string `json:"args"`
	}
	_ = json.Unmarshal(arguments, &args)
	what := args.Path
	if what == "" && args.Program != "" {
		what = strings.TrimSpace(args.Program[strings.LastIndex(args.Program, "/")+1:] + " " + strings.Join(args.Args, " "))
	}
	return cutBytes(strings.TrimSpace(name+" "+what), 160)
}

// blocks cuts the lines into blocks of at most room bytes, between whole
// lines; a line longer than a block goes in pieces, each labelled with
// the call whose result it is.
func blocks(lines []line, room int) []string {
	var out []string
	var current []string
	size := 0
	flush := func() {
		if len(current) > 0 {
			out = append(out, strings.Join(current, "\n"))
			current, size = nil, 0
		}
	}
	for _, l := range lines {
		if len(l.text) > room-pieceRoom {
			flush()
			out = append(out, pieces(l, room-pieceRoom)...)
			continue
		}
		if size+len(l.text)+1 > room {
			flush()
		}
		current = append(current, l.text)
		size += len(l.text) + 1
	}
	flush()
	return out
}

// pieces cuts one long line into labelled pieces of at most step bytes of
// text, each cut on a rune boundary.
func pieces(l line, step int) []string {
	var parts []string
	text := l.text
	for len(text) > 0 {
		n := min(step, len(text))
		for n < len(text) && n > 0 && !utf8.RuneStart(text[n]) {
			n--
		}
		parts = append(parts, text[:n])
		text = text[n:]
	}
	what := l.label
	if what == "" {
		what = "message"
	}
	out := make([]string, len(parts))
	for i, part := range parts {
		out[i] = fmt.Sprintf("[Piece %d of %d of one transcript line: the result of tool call %s]\n%s", i+1, len(parts), what, part)
	}
	return out
}

// cutBytes keeps at most n bytes of s, on a rune boundary.
func cutBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
