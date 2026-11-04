package axlr

import (
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
)

func TestTheRunsLinesBecomeProgressItems(t *testing.T) {
	var told []application.ProgressItem
	tracker := newProgressTracker(func(item application.ProgressItem) { told = append(told, item) })
	clock := time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)
	tracker.now = func() time.Time {
		clock = clock.Add(250 * time.Millisecond)
		return clock
	}
	for _, line := range []string{
		`{"type":"session","session":"sess-1"}`,
		`{"type":"text","text":"Looking at the pods."}`,
		`{"type":"tool.requested","tool":"axlr_call_tool","call_id":"c1","arguments":{"name":"list_pods","arguments":{"namespace":"web","labels":"` + strings.Repeat("x", 300) + `"}}}`,
		`{"type":"tool.completed","tool":"axlr_call_tool","call_id":"c1","decision":"auto_approve"}`,
		`{"type":"tool.requested","tool":"local_read","call_id":"c2","arguments":{"path":"app.log"}}`,
		`{"type":"tool.completed","tool":"local_read","call_id":"c2","decision":"deny"}`,
		`{"type":"tool.requested","tool":"local_exec","call_id":"c3"}`,
		`{"type":"tool.completed","tool":"local_exec","call_id":"c3","error":"exit 1"}`,
		`{"type":"tool.completed","tool":"local_grep","call_id":"c9"}`,
		`{"type":"decision.required","tool":"service_restart","call_id":"c4"}`,
		`not json`,
	} {
		tracker.feed([]byte(line))
	}
	kinds := []application.ProgressKind{}
	for _, item := range told {
		kinds = append(kinds, item.Kind)
	}
	want := []application.ProgressKind{"text", "tool.started", "tool.ended", "tool.started", "tool.ended", "tool.started", "tool.ended", "tool.ended", "decision.required"}
	if len(kinds) != len(want) {
		t.Fatalf("%v", kinds)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("%v", kinds)
		}
	}
	if started := told[1]; started.MCPTool != "list_pods" || !strings.HasPrefix(started.Summary, `{"namespace":"web","labels":"xxx`) || len([]rune(started.Summary)) != maxProgressSummary || started.CallID != "c1" {
		t.Fatalf("the MCP tool and a short summary: %+v", started)
	}
	if ended := told[2]; ended.Status != "ok" || ended.DurationMS != 250 {
		t.Fatalf("%+v", ended)
	}
	if told[3].Summary != `{"path":"app.log"}` || told[3].MCPTool != "" || told[4].Status != "denied" || told[6].Status != "failed" {
		t.Fatalf("%+v", told[3:7])
	}
	if unmatched := told[7]; unmatched.DurationMS != 0 || unmatched.Status != "ok" {
		t.Fatalf("a call never seen starting has no duration: %+v", unmatched)
	}
	if waiting := told[8]; waiting.Tool != "service_restart" || waiting.CallID != "c4" {
		t.Fatalf("%+v", waiting)
	}

	var none *progressTracker
	none.feed([]byte(`{"type":"text","text":"x"}`))
	newProgressTracker(nil).feed([]byte(`{"type":"text","text":"x"}`))
}

func TestTheRunsWordsAreCappedOnce(t *testing.T) {
	var told []application.ProgressItem
	tracker := newProgressTracker(func(item application.ProgressItem) { told = append(told, item) })
	chunk := strings.Repeat("é", 1000) // two bytes each
	for range 20 {
		tracker.feed([]byte(`{"type":"text","text":"` + chunk + `"}`))
	}
	tracker.feed([]byte(`{"type":"text","text":""}`))
	total, cut := 0, 0
	for _, item := range told {
		switch item.Kind {
		case application.ProgressText:
			total += len(item.Text)
			if !strings.HasSuffix(item.Text, "é") {
				t.Fatal("the text is cut between characters")
			}
		case application.ProgressTextTruncated:
			cut++
		}
	}
	if total > maxProgressText || total < maxProgressText-1 || cut != 1 || told[len(told)-1].Kind != application.ProgressTextTruncated {
		t.Fatalf("total %d, cut %d", total, cut)
	}
}

func TestArgumentsSayLittle(t *testing.T) {
	for _, c := range []struct{ tool, raw, mcp, summary string }{
		{"local_read", ``, "", ""},
		{"local_read", `{}`, "", ""},
		{"local_read", `null`, "", ""},
		{"local_read", `not json`, "", ""},
		{"axlr_call_tool", `{"arguments":{"a":1}}`, "", `{"arguments":{"a":1}}`},
		{"axlr_call_tool", `{"name":"kmp_ask","arguments":{}}`, "kmp_ask", ""},
	} {
		mcp, summary := describeArguments(c.tool, []byte(c.raw))
		if mcp != c.mcp || summary != c.summary {
			t.Errorf("%s %s: %q %q", c.tool, c.raw, mcp, summary)
		}
	}
}
