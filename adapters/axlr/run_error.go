package axlr

import (
	"bytes"
	"encoding/json"
	"strings"
)

// runEnvelope is AXLR's response envelope of a local or bridged call, as a
// failed call's error carries it: what the operator reads of it is the exit
// code and the program's own words.
type runEnvelope struct {
	ProtocolVersion json.RawMessage `json:"protocol_version"`
	Status          string          `json:"status,omitempty"`
	Output          *runOutput      `json:"output"`
	Error           json.RawMessage `json:"error,omitempty"`
}

// runOutput is the envelope's output, stderr before stdout: a program
// writes its reason to one or the other.
type runOutput struct {
	ExitCode *int   `json:"exit_code,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	Stdout   string `json:"stdout,omitempty"`
}

// compactRunError keeps of a failed call's error the envelope's status,
// exit code, stderr and stdout, and drops the request id, the timings and
// the byte counts. The progress item keeps 240 characters of an error, and
// on 12 November 2026 the envelope's own fields filled them before the
// program's words: a failed run read "cut before the error" in the
// Activity and in its audit. Anything that is not such
// an envelope is returned as it came.
func compactRunError(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, `{"protocol_version"`) {
		return raw
	}
	var env runEnvelope
	if json.Unmarshal([]byte(trimmed), &env) != nil || env.Output == nil {
		return raw
	}
	if string(env.Error) == "null" {
		env.Error = nil
	}
	env.Output.Stderr = strings.TrimSpace(env.Output.Stderr)
	env.Output.Stdout = strings.TrimSpace(env.Output.Stdout)
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if enc.Encode(env) != nil {
		return raw
	}
	return strings.TrimSpace(out.String())
}
