// Package fakeaxlr writes a shell stand-in for axlr-tui that the tests of
// the command, the API and the daemon run instead of the real binary: it
// answers --help and --version, records the arguments of every run and
// plays one scenario of the runtime contract.
package fakeaxlr

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
)

// Options choose what the stand-in does.
type Options struct {
	// Scenario is one of completed, slow (completed after a second),
	// decision, failed, approval-flow, delivered (two artifacts written
	// to the workspace, one of them changed after its delivery, and a
	// receipt) or steered (answers the first message of its --steer-file).
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
	// Steer makes --help advertise -steer-file, like a build that reads
	// what a person writes to a run while it works.
	Steer bool
}

const resultCompleted = `{"type":"result","session":"sess-1","result":{"schema":"axlr.execution-result/v1","status":"completed","session_id":"sess-1","sessions":["sess-1"],"answer":"%s","tool_calls":[%s],"usage":{"requests":1,"prompt_tokens":10,"completion_tokens":1,"cost_usd":0.001},"artifacts":[]}}`

// DeliveredContent is what the delivered scenario writes as notes.md.
const DeliveredContent = "# notes\n\nThe disk is full.\n"

// delivered writes notes.md as listed and plan.md other than listed,
// then lists both with the receipt and the result.
func delivered() string {
	sum := sha256.Sum256([]byte(DeliveredContent))
	notes := fmt.Sprintf(`{"name":"notes.md","media_type":"text/markdown","size":%d,"sha256":"%s","reference":".axlr/deliverables/sess-1/notes.md","description":"What I found","source":"inline","session_id":"sess-1","call_id":"deliver-1"}`, len(DeliveredContent), hex.EncodeToString(sum[:]))
	plan := fmt.Sprintf(`{"name":"plan.md","media_type":"text/markdown","size":7,"sha256":"%s","reference":"plan.md","source":"file","session_id":"sess-1","call_id":"deliver-2"}`, hex.EncodeToString(sum[:]))
	receipt := `{"type":"receipt","receipt":{"schema":"axlr.execution-receipt/v1","status":"completed","session_id":"sess-1","complete":true}}`
	result := `{"type":"result","session":"sess-1","result":{"schema":"axlr.execution-result/v1","status":"completed","session_id":"sess-1","sessions":["sess-1"],"answer":"Delivered the notes.","tool_calls":[],"usage":{"requests":1},"artifacts":[` + notes + `,` + plan + `]}}`
	return `mkdir -p .axlr/deliverables/sess-1
printf '# notes\n\nThe disk is full.\n' > .axlr/deliverables/sess-1/notes.md
printf 'changed' > plan.md
printf '%s\n' '{"type":"session","session":"sess-1"}' '{"type":"artifact","artifact":` + notes + `}' '` + receipt + `' '` + result + `'
exit 0`
}

// Write writes the stand-in under dir and returns its path and the file
// it records the last run's arguments in, one per line.
func Write(dir string, options Options) (bin, argsFile string, err error) {
	argsFile = filepath.Join(dir, "args.txt")
	script := "#!/bin/sh\n" +
		"for a in \"$@\"; do if [ \"$a\" = --help ]; then echo '  -exec string' >&2; " + helpPolicy(options.NoPolicy) + helpCorrelation(options.Correlation) + helpSteer(options.Steer) + "exit 0; fi; " +
		"if [ \"$a\" = --version ]; then echo 'axlr-tui 0.0.0-fake'; exit 0; fi; done\n" +
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

func helpSteer(steer bool) string {
	if !steer {
		return ""
	}
	return "echo '  -steer-file string' >&2; "
}

// steered waits up to five seconds for the first line of its --steer-file
// and answers what it read, as a run steered between model steps would.
const steered = `steer=""
prev=""
for a in "$@"; do if [ "$prev" = --steer-file ]; then steer="$a"; fi; prev="$a"; done
printf '%s\n' '{"type":"session","session":"sess-1"}'
i=0
while [ $i -lt 100 ] && [ ! -s "$steer" ]; do sleep 0.05; i=$((i+1)); done
heard=$(head -n 1 "$steer" 2>/dev/null | sed 's/^{"text":"\(.*\)"}$/\1/')
printf '%s\n' "{\"type\":\"steered\",\"text\":\"$heard\"}"
printf '%s\n' "{\"type\":\"text\",\"text\":\"Heard: $heard\"}"
printf '%s\n' "{\"type\":\"result\",\"session\":\"sess-1\",\"result\":{\"schema\":\"axlr.execution-result/v1\",\"status\":\"completed\",\"session_id\":\"sess-1\",\"sessions\":[\"sess-1\"],\"answer\":\"Heard: $heard\",\"tool_calls\":[],\"usage\":{\"requests\":2},\"artifacts\":[]}}"
exit 0`

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
	case "delivered":
		return delivered()
	case "steered":
		return steered
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
