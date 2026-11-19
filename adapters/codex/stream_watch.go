package codex

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode"
)

const (
	// maxWatchedLine bounds one server-sent event line the proxy reads; a
	// longer line, an output item with a large tool call, is passed on
	// unread.
	maxWatchedLine = 64 << 10
	// maxLoggedMessage bounds the backend's message in the log.
	maxLoggedMessage = 300
)

// streamWatch reads the server-sent events the proxy passes on, without
// changing them, for the backend's failure: an error event, or a
// response.failed. On 18 and 19 November 2026 a few responses a day failed
// this way under HTTP 200, and the log said only "200".
type streamWatch struct {
	line     []byte
	skipping bool
	failure  string
	finished bool
}

// Write reads a piece of the stream; it never fails.
func (s *streamWatch) Write(piece []byte) (int, error) {
	read := len(piece)
	for len(piece) > 0 {
		end := bytes.IndexByte(piece, '\n')
		chunk := piece
		if end >= 0 {
			chunk = piece[:end]
		}
		if !s.skipping {
			if len(s.line)+len(chunk) > maxWatchedLine {
				s.line, s.skipping = s.line[:0], true
			} else {
				s.line = append(s.line, chunk...)
			}
		}
		if end < 0 {
			break
		}
		if !s.skipping {
			s.event(bytes.TrimRight(s.line, "\r"))
		}
		s.line, s.skipping = s.line[:0], false
		piece = piece[end+1:]
	}
	return read, nil
}

// event reads one line: a data line holding an event the watch knows.
func (s *streamWatch) event(line []byte) {
	data, ok := bytes.CutPrefix(line, []byte("data:"))
	if !ok {
		return
	}
	var event struct {
		Type     string          `json:"type"`
		Code     json.RawMessage `json:"code"`
		Message  string          `json:"message"`
		Error    *backendError   `json:"error"`
		Response *struct {
			Error *backendError `json:"error"`
		} `json:"response"`
	}
	if json.Unmarshal(bytes.TrimSpace(data), &event) != nil {
		return
	}
	switch event.Type {
	case "error":
		failure := event.Error
		if failure == nil {
			failure = &backendError{Code: event.Code, Message: event.Message}
		}
		s.fail("error", failure)
	case "response.failed":
		var failure *backendError
		if event.Response != nil {
			failure = event.Response.Error
		}
		s.fail("response.failed", failure)
		s.finished = true
	case "response.completed", "response.incomplete":
		s.finished = true
	}
}

// fail keeps the first failure the stream carried.
func (s *streamWatch) fail(kind string, failure *backendError) {
	if s.failure != "" {
		return
	}
	s.failure = kind
	if failure == nil {
		return
	}
	var code string
	if json.Unmarshal(failure.Code, &code) != nil || code == "" {
		code = failure.Type
	}
	if code != "" {
		s.failure += " " + oneLine(code, 64)
	}
	if message := oneLine(failure.Message, maxLoggedMessage); message != "" {
		s.failure += ": " + message
	}
}

// Outcome says how the stream ended, "" when it ended well or was no
// event stream at all.
func (s *streamWatch) Outcome(eventStream bool) string {
	switch {
	case s.failure != "":
		return "the backend failed the response: " + s.failure
	case eventStream && !s.finished:
		return "the stream ended before the response did"
	}
	return ""
}

// backendError is the error object of an error event or a failed response.
type backendError struct {
	Code    json.RawMessage `json:"code"`
	Type    string          `json:"type"`
	Message string          `json:"message"`
}

// oneLine keeps text on one line of printable characters, at most limit
// runes.
func oneLine(text string, limit int) string {
	text = strings.Join(strings.FieldsFunc(text, func(r rune) bool { return unicode.IsSpace(r) || !unicode.IsPrint(r) }), " ")
	if runes := []rune(text); len(runes) > limit {
		text = string(runes[:limit]) + "…"
	}
	return text
}
