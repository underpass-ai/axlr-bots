package activityaudit

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// callError is what a failed call's error says, as the run's line carries
// it: AXLR writes a host tool's refusal as {"call_id", "error"} and a
// bridged or local call's answer as its response envelope
// ({"protocol_version", "status", "output": {"exit_code", …}}), and cuts
// the line (240 characters, an ellipsis last). Text is the error's words;
// Exit a local program's exit code ("" when unknown); Cut that the line
// ended before the words.
type callError struct {
	Text string
	Exit string
	Cut  bool
}

// The fields read from a line that is cut and no longer JSON: a string's
// value up to its closing quote (or the cut), and a number.
var (
	errorField   = regexp.MustCompile(`"error":"((?:[^"\\]|\\.)*)("?)`)
	messageField = regexp.MustCompile(`"message":"((?:[^"\\]|\\.)*)("?)`)
	stderrField  = regexp.MustCompile(`"stderr":"((?:[^"\\]|\\.)*)("?)`)
	// stdoutErrorField is a program's error inside its cut JSON stdout.
	stdoutErrorField = regexp.MustCompile(`"error": ?"((?:[^"\\]|\\.)*)`)
	stdoutField      = regexp.MustCompile(`"stdout":"((?:[^"\\]|\\.)*)("?)`)
	exitField        = regexp.MustCompile(`"exit_code":(-?\d+)[,}]`)
	envelope         = regexp.MustCompile(`^\{"protocol_version":`)
	completedRun     = regexp.MustCompile(`"status":"completed"`)
)

// readCallError reads the error of a call's tool.ended item.
func readCallError(raw string) callError {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return callError{Cut: true}
	}
	if !strings.HasPrefix(raw, "{") {
		return callError{Text: raw}
	}
	var whole map[string]any
	if json.Unmarshal([]byte(raw), &whole) == nil {
		return wholeError(whole)
	}
	return cutError(raw)
}

// wholeError reads an error line that is JSON to its end.
func wholeError(fields map[string]any) callError {
	var found callError
	switch e := fields["error"].(type) {
	case string:
		found.Text = e
	case map[string]any:
		code, _ := e["code"].(string)
		message, _ := e["message"].(string)
		found.Text = strings.TrimPrefix(code+": "+message, ": ")
	case nil:
		// A host tool may answer its refusal as {"code", "message"} with no
		// "error" around it (local_read, 13 November 2026).
		code, _ := fields["code"].(string)
		message, _ := fields["message"].(string)
		found.Text = strings.TrimPrefix(code+": "+message, ": ")
	}
	if output, ok := fields["output"].(map[string]any); ok {
		if exit, ok := output["exit_code"].(float64); ok {
			found.Exit = strconv.Itoa(int(exit))
		}
		if stderr, ok := output["stderr"].(string); ok && found.Text == "" {
			found.Text = stderr
		}
		// A program may answer its failure on stdout, as JSON with an error,
		// and leave stderr empty.
		if stdout, ok := output["stdout"].(string); ok && strings.TrimSpace(found.Text) == "" {
			found.Text = programWords(stdout)
		}
	}
	found.Text = strings.TrimSpace(found.Text)
	return found
}

// cutError reads what is left of a line cut before its end.
func cutError(raw string) callError {
	var found callError
	if m := exitField.FindStringSubmatch(raw); m != nil {
		found.Exit = m[1]
	}
	for _, field := range []*regexp.Regexp{errorField, messageField, stderrField, stdoutField} {
		m := field.FindStringSubmatch(raw)
		if m == nil {
			continue
		}
		text := unescape(m[1])
		if m[2] == "" {
			// The cut fell inside the string: what is there is still its
			// first words.
			text = strings.TrimSuffix(text, "…")
		}
		if field == stdoutField {
			text = programWords(text)
		}
		if text = strings.TrimSpace(text); text != "" {
			found.Text = text
			return found
		}
	}
	if envelope.MatchString(raw) {
		// The response envelope, cut before its output: only that the
		// call failed is known.
		found.Cut = true
		return found
	}
	found.Text = strings.TrimSuffix(raw, "…")
	return found
}

// unescape reads a JSON string's body, a trailing broken escape dropped.
func unescape(body string) string {
	for len(body) > 0 {
		if text, err := strconv.Unquote(`"` + body + `"`); err == nil {
			return text
		}
		body = body[:len(body)-1]
	}
	return ""
}

// programWords reads a program's stdout: its "error" when stdout is a JSON
// answer that has one; otherwise stdout itself.
func programWords(stdout string) string {
	stdout = strings.TrimSpace(stdout)
	var answer struct {
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(stdout), &answer) == nil && answer.Error != "" {
		return answer.Error
	}
	if text, ok := strings.CutPrefix(stdout, "{"); ok {
		if m := stdoutErrorField.FindStringSubmatch("{" + text); m != nil {
			return strings.TrimSuffix(unescape(m[1]), "…")
		}
	}
	return stdout
}
