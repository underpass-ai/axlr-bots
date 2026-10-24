package axlr

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestHelperProcess is the fake axlr-tui: the executor runs this test
// binary with FAKE_AXLR naming a scenario, and it prints the JSON lines
// that scenario stands for. It records its arguments in FAKE_AXLR_ARGS.
func TestHelperProcess(t *testing.T) {
	scenario := os.Getenv("FAKE_AXLR")
	if scenario == "" {
		return
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	for _, a := range args {
		if a == "--version" {
			fmt.Fprintln(os.Stdout, "axlr-tui 0.14.0-dev")
			os.Exit(0)
		}
		if a == "--help" {
			// The real axlr-tui prints its flags on --help; one of them
			// is -policy unless the build is older than the policy.
			fmt.Fprintln(os.Stderr, "  -exec string\n    \trun one prompt without the console")
			if os.Getenv("FAKE_AXLR_NO_POLICY") == "" {
				fmt.Fprintln(os.Stderr, "  -policy string\n    \twith --exec or --decide: a JSON file with the execution's policy")
			}
			os.Exit(0)
		}
	}
	if path := os.Getenv("FAKE_AXLR_ARGS"); path != "" {
		_ = os.WriteFile(path, []byte(strings.Join(args, "\n")), 0o644)
	}
	say := func(lines ...string) {
		for _, l := range lines {
			fmt.Fprintln(os.Stdout, l)
		}
	}
	const (
		session = `{"type":"session","session":"sess-1"}`
		text    = `{"type":"text","text":"The spike "}`
		text2   = `{"type":"text","text":"came from pod api-3."}`
		reqd    = `{"type":"tool.requested","tool":"local_read","call_id":"c1","decision":"auto_approve"}`
		done    = `{"type":"tool.completed","tool":"local_read","call_id":"c1","decision":"auto_approve"}`
		reqd2   = `{"type":"tool.requested","tool":"local_exec","call_id":"c2"}`
	)
	resultCompleted := `{"type":"result","session":"sess-1","result":{"schema":"axlr.execution-result/v1","status":"completed","session_id":"sess-1","sessions":["sess-0","sess-1"],"answer":"The spike came from pod api-3.","tool_calls":[{"call_id":"c1","tool":"local_read","decision":"auto_approve","status":"completed"}],"usage":{"requests":2,"prompt_tokens":3120,"cached_tokens":2048,"cache_write_tokens":0,"completion_tokens":96,"reasoning_tokens":40,"cost_usd":0.0021,"cost_requests":2,"unmetered_requests":0},"artifacts":[{"name":"report.md","media_type":"text/markdown","reference":"artifacts/report.md"}]}}`
	switch scenario {
	case "result-completed":
		say(session, text, reqd, done, text2, `{"type":"completed","session":"sess-1","status":"idle"}`, resultCompleted)
		os.Exit(0)
	case "result-needs-decision":
		say(session, reqd2, `{"type":"decision.required","session":"sess-1","tool":"local_exec","call_id":"c2","status":"approval"}`,
			`{"type":"result","session":"sess-1","result":{"schema":"axlr.execution-result/v1","status":"needs_decision","session_id":"sess-1","sessions":["sess-1"],"answer":"I will run it now.","pending":{"call_id":"c2","tool":"local_exec","arguments":{"program":"touch"}},"tool_calls":[{"call_id":"c2","tool":"local_exec","status":"pending"}],"usage":{"requests":1},"artifacts":[]}}`)
		os.Exit(3)
	case "result-needs-decision-no-pending":
		say(session, `{"type":"result","session":"sess-1","result":{"schema":"axlr.execution-result/v1","status":"needs_decision","session_id":"sess-1","tool_calls":[],"usage":{},"artifacts":[]}}`)
		os.Exit(3)
	case "result-failed":
		say(session, `{"type":"failed","session":"sess-1","error":"provider refused"}`,
			`{"type":"result","session":"sess-1","result":{"schema":"axlr.execution-result/v1","status":"failed","session_id":"sess-1","error":"provider refused","tool_calls":[],"usage":{},"artifacts":[]}}`)
		os.Exit(1)
	case "result-failed-no-error":
		say(`{"type":"result","result":{"schema":"axlr.execution-result/v1","status":"failed","session_id":"sess-1"}}`)
		os.Exit(1)
	case "result-interrupted":
		say(session, `{"type":"result","session":"sess-1","result":{"schema":"axlr.execution-result/v1","status":"interrupted","session_id":"sess-1","error":"cancelled","tool_calls":[],"usage":{},"artifacts":[]}}`)
		os.Exit(1)
	case "result-interrupted-no-error":
		say(`{"type":"result","result":{"schema":"axlr.execution-result/v1","status":"interrupted","session_id":"sess-1"}}`)
		os.Exit(1)
	case "result-unknown-status":
		say(session, `{"type":"result","session":"sess-1","result":{"schema":"axlr.execution-result/v1","status":"meh","session_id":"sess-1"}}`)
		os.Exit(0)
	case "result-bad-schema":
		say(session, text, `{"type":"completed","session":"sess-1"}`, `{"type":"result","session":"sess-1","result":{"schema":"axlr.execution-result/v9","status":"failed"}}`)
		os.Exit(0)
	case "legacy-completed":
		say(session, text, reqd, done, text2, `{"type":"completed","session":"sess-1","status":"idle"}`)
		os.Exit(0)
	case "legacy-continued":
		say(session, `{"type":"session.continued","session":"sess-2"}`, `{"type":"session","session":"sess-2"}`, text, `{"type":"completed","session":"sess-2"}`)
		os.Exit(0)
	case "legacy-decision":
		say(session, reqd2, `{"type":"decision.required","session":"sess-1","tool":"local_exec","call_id":"c2","status":"approval"}`)
		os.Exit(3)
	case "legacy-decision-missing":
		say(session)
		os.Exit(3)
	case "legacy-failed":
		say(session, `{"type":"failed","session":"sess-1","error":"the prompt kept reaching the context cap"}`)
		os.Exit(1)
	case "no-json":
		fmt.Fprintln(os.Stderr, "axlr-tui: settings: /home/x/.config/axlr/settings.json")
		fmt.Fprintln(os.Stderr, "axlr-tui: no model provider: set OPENROUTER_API_KEY")
		fmt.Fprintln(os.Stderr, "")
		os.Exit(1)
	case "exit-7":
		os.Exit(7)
	case "garbage":
		say("not json at all", `{"type":"reasoning","text":"thinking"}`, `{"type":"unknown.kind"}`, `{"type":"tool.completed","tool":"orphan","call_id":"c9"}`, `{"type":"tool.completed","tool":"orphan","call_id":"c9"}`, `{"type":"tool.requested","tool":"anon"}`, `{"type":"tool.requested","tool":"anon2"}`, text, `{"type":"completed"}`)
		os.Exit(0)
	case "hang":
		say(session, text)
		time.Sleep(30 * time.Second)
		os.Exit(0)
	case "hang-ignoring-term":
		say(session)
		time.Sleep(30 * time.Second)
		os.Exit(0)
	case "huge-line":
		say(session, `{"type":"text","text":"`+strings.Repeat("x", maxLineBytes+1)+`"}`)
		os.Exit(0)
	}
	fmt.Fprintln(os.Stderr, "unknown scenario", scenario)
	os.Exit(2)
}
