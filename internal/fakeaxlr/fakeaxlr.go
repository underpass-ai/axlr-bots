// Package fakeaxlr writes a shell stand-in for axlr-tui that the tests of
// the command, the API and the daemon run instead of the real binary: it
// answers --help, records the arguments of every run and plays one
// scenario of the runtime contract.
package fakeaxlr

import (
	"fmt"
	"os"
	"path/filepath"
)

// Options choose what the stand-in does.
type Options struct {
	// Scenario is one of completed, slow (completed after a second),
	// decision, failed or approval-flow.
	Scenario string
	// NoPolicy makes --help omit -policy, like a build older than the
	// execution policy.
	NoPolicy bool
	// Correlation makes --help advertise -correlation and -budget, like a
	// build that takes the contract's reserved fields.
	Correlation bool
	// Marker is a file the approval-flow scenario creates when, and only
	// when, it runs the write after --decide approve.
	Marker string
}

const resultCompleted = `{"type":"result","session":"sess-1","result":{"schema":"axlr.execution-result/v1","status":"completed","session_id":"sess-1","sessions":["sess-1"],"answer":"%s","tool_calls":[%s],"usage":{"requests":1,"prompt_tokens":10,"completion_tokens":1,"cost_usd":0.001},"artifacts":[]}}`

// Write writes the stand-in under dir and returns its path and the file
// it records the last run's arguments in, one per line.
func Write(dir string, options Options) (bin, argsFile string, err error) {
	argsFile = filepath.Join(dir, "args.txt")
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do if [ \"$a\" = --help ]; then echo '  -exec string' >&2; " + helpPolicy(options.NoPolicy) + helpCorrelation(options.Correlation) + "exit 0; fi; done\n" +
		"printf '%s\\n' \"$@\" > '" + argsFile + "'\n" +
		body(options) + "\n"
	bin = filepath.Join(dir, "fake-axlr-tui")
	return bin, argsFile, os.WriteFile(bin, []byte(script), 0o755)
}

func helpPolicy(noPolicy bool) string {
	if noPolicy {
		return ""
	}
	return "echo '  -policy string' >&2; "
}

func helpCorrelation(correlation bool) string {
	if !correlation {
		return ""
	}
	return "echo '  -correlation string' >&2; echo '  -budget string' >&2; "
}

func body(options Options) string {
	completed := func(answer, calls string) string {
		return `printf '%s\n' '{"type":"session","session":"sess-1"}' '{"type":"text","text":"` + answer + `"}' '{"type":"completed","session":"sess-1"}' '` + fmt.Sprintf(resultCompleted, answer, calls) + `'
exit 0`
	}
	switch options.Scenario {
	case "slow":
		return "sleep 1\n" + completed(`Done after a while.`, ``)
	case "decision":
		return `printf '%s\n' '{"type":"session","session":"sess-1"}' '{"type":"decision.required","session":"sess-1","tool":"local_exec","call_id":"c1","status":"approval"}'
exit 3`
	case "failed":
		return `echo "axlr-tui: no model provider" >&2
exit 1`
	case "approval-flow":
		return `decide=""
prev=""
for a in "$@"; do if [ "$prev" = --decide ]; then decide="$a"; fi; prev="$a"; done
case "$decide" in
  approve)
    : > '` + options.Marker + `'
    ` + completed(`Restarted the unit after approval.`, `{"call_id":"c1","tool":"local_exec","decision":"approve","status":"completed"}`) + `
    ;;
  deny)
    ` + completed(`The restart was denied; nothing changed.`, `{"call_id":"c1","tool":"local_exec","decision":"deny","status":"denied"}`) + `
    ;;
  *)
    printf '%s\n' '{"type":"session","session":"sess-1"}' '{"type":"tool.requested","tool":"local_exec","call_id":"c1"}' '{"type":"decision.required","session":"sess-1","tool":"local_exec","call_id":"c1","status":"approval"}' '{"type":"result","session":"sess-1","result":{"schema":"axlr.execution-result/v1","status":"needs_decision","session_id":"sess-1","sessions":["sess-1"],"answer":"I will restart the unit now.","pending":{"call_id":"c1","tool":"local_exec","arguments":{"program":"systemctl","args":["--user","restart","demo-broken.service"]}},"tool_calls":[{"call_id":"c1","tool":"local_exec","status":"pending"}],"usage":{"requests":1},"artifacts":[]}}'
    exit 3
    ;;
esac`
	}
	return completed(`# Report\\n\\nThe unit exited 1 on start.`, `{"call_id":"c1","tool":"local_exec","decision":"auto_approve","status":"completed"}`)
}
