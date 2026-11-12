package activityaudit

import (
	"encoding/json"
	"path"
	"regexp"
)

// callSummary is what a local_exec's tool.started summary shows of its
// arguments: the node cuts the summary (120 characters, an ellipsis
// last) and the model writes its why first, so the program is often
// gone. Program is the program's basename and Arg its first argument,
// each only when the summary shows it whole; ArgKnown says whether the
// arguments were seen (an empty list included).
type callSummary struct {
	Program  string
	Arg      string
	ArgKnown bool
}

var (
	programField = regexp.MustCompile(`"program":"((?:[^"\\]|\\.)*)"`)
	firstArg     = regexp.MustCompile(`"args":\[\s*(?:"((?:[^"\\]|\\.)*)"|(\]))`)
)

// readCallSummary reads a local_exec's summary.
func readCallSummary(summary string) callSummary {
	var whole struct {
		Program string   `json:"program"`
		Args    []string `json:"args"`
	}
	if json.Unmarshal([]byte(summary), &whole) == nil {
		found := callSummary{ArgKnown: true}
		if whole.Program != "" {
			found.Program = path.Base(whole.Program)
		}
		if len(whole.Args) > 0 {
			found.Arg = whole.Args[0]
		}
		return found
	}
	var found callSummary
	if m := programField.FindStringSubmatch(summary); m != nil {
		if program := unescape(m[1]); program != "" {
			found.Program = path.Base(program)
		}
	}
	if m := firstArg.FindStringSubmatch(summary); m != nil {
		found.ArgKnown = true
		found.Arg = unescape(m[1])
	}
	return found
}
