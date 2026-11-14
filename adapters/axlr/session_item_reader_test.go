package axlr

import (
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
)

// A session as AXLR writes it: words, calls, the envelope each tool
// answers, and the decisions per call.
const sessionFixture = `{"version":3,"id":"s1","messages":[
 {"role":"user","content":"go"},
 {"role":"assistant","content":"I will read.","tool_calls":[{"id":"c1","name":"local_read","arguments":{"path":"a.md"}}],"tool_call_id":""},
 {"role":"tool","content":"{\"request_id\":\"c1\",\"status\":\"completed\",\"started_at\":\"2026-11-12T08:54:26Z\",\"finished_at\":\"2026-11-12T08:54:27Z\",\"duration_ms\":1000,\"output\":{},\"error\":null}","tool_call_id":"c1"},
 {"role":"assistant","content":"","tool_calls":[{"id":"c2","name":"axlr_call_tool","arguments":{"name":"kmp_ask","arguments":{"q":"x"}}},{"id":"c3","name":"local_exec","arguments":{}}]},
 {"role":"tool","content":"{\"request_id\":\"c2\",\"status\":\"failed\",\"finished_at\":\"2026-11-12T08:55:00Z\",\"error\":\"unknown tool\\nsecond line\"}"},
 {"role":"tool","content":"plain text, not an envelope","tool_call_id":"c3"},
 {"role":"assistant","content":"","tool_calls":[{"id":"c4","name":"axlr_bots_delegate","arguments":{"bot":"helper","objective":"o"}}]},
 {"role":"tool","content":"{\"status\":\"timed_out\",\"error\":{\"message\":\"too slow\"}}","tool_call_id":"c4"},
 {"role":"assistant","content":"all done"}
],"activity":[{"call":{"id":"c3"},"decision":"deny","outcome":{}}]}`

func TestSessionItemReaderTurnsASessionIntoItems(t *testing.T) {
	items, err := SessionItemReader{}.ReadItems([]byte(sessionFixture))
	if err != nil {
		t.Fatal(err)
	}
	kinds := make([]application.ProgressKind, len(items))
	for i, it := range items {
		kinds[i] = it.Kind
	}
	want := []application.ProgressKind{
		application.ProgressText, application.ProgressToolStarted, application.ProgressToolEnded,
		application.ProgressToolStarted, application.ProgressToolStarted, application.ProgressToolEnded,
		application.ProgressToolEnded, application.ProgressToolStarted, application.ProgressToolEnded,
		application.ProgressDelegation, application.ProgressText,
	}
	if len(kinds) != len(want) {
		t.Fatalf("kinds %v", kinds)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds %v", kinds)
		}
	}
	read := items[2]
	if read.Tool != "local_read" || read.Status != "ok" || read.DurationMS != 1000 || !read.At.Equal(time.Date(2026, 11, 12, 8, 54, 27, 0, time.UTC)) {
		t.Errorf("ended: %+v", read)
	}
	if bridged := items[3]; bridged.MCPTool != "kmp_ask" || bridged.Summary == "" {
		t.Errorf("bridged call: %+v", bridged)
	}
	// An answer with no tool_call_id is matched by its request id.
	if failed := items[5]; failed.CallID != "c2" || failed.Status != "failed" || failed.Error != "unknown tool" || failed.Tool != "axlr_call_tool" {
		t.Errorf("failed: %+v", failed)
	}
	if denied := items[6]; denied.CallID != "c3" || denied.Status != "denied" {
		t.Errorf("denied: %+v", denied)
	}
	if timedOut := items[8]; timedOut.Status != "failed" || timedOut.Error != "too slow" {
		t.Errorf("timed out: %+v", timedOut)
	}
	if d := items[9]; d.Bot != "helper" {
		t.Errorf("delegation: %+v", d)
	}
	if last := items[10]; last.Text != "all done" || last.At.IsZero() {
		t.Errorf("last words: %+v", last)
	}
}

func TestSessionItemReaderRefusesWhatIsNotASession(t *testing.T) {
	if _, err := (SessionItemReader{}).ReadItems([]byte("not json")); err == nil {
		t.Error("read garbage")
	}
	if got := firstLine(string(make([]rune, 300))); len([]rune(got)) != 240 {
		t.Errorf("first line cut: %d", len([]rune(got)))
	}
	if errorText([]byte(`{"code":1}`)) != `{"code":1}` || !hasError([]byte(`"x"`)) || hasError([]byte(`null`)) {
		t.Error("error helpers")
	}
}
