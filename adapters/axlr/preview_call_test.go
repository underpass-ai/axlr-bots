package axlr

import (
	"encoding/json"
	"testing"
	"time"
)

func TestPreviewCall(t *testing.T) {
	for raw, want := range map[string]bool{
		"":                  false,
		"not json":          false,
		`{}`:                false,
		`{"confirm":true}`:  false,
		`{"confirm":false}`: true,
		`{"name":"patch","arguments":{"confirm":false}}`: true,
		`{"name":"patch","arguments":{"confirm":true}}`:  false,
	} {
		if got := previewCall(json.RawMessage(raw)); got != want {
			t.Errorf("%q: %v", raw, got)
		}
	}
}

func TestTheParserMarksAPreview(t *testing.T) {
	p := newTurnParser(time.Now())
	p.feed([]byte(`{"type":"tool.requested","tool":"axlr_call_tool","call_id":"c1","arguments":{"name":"patch","arguments":{"confirm":false}}}`))
	p.feed([]byte(`{"type":"tool.completed","tool":"axlr_call_tool","call_id":"c1"}`))
	if len(p.calls) != 1 || !p.calls[0].Preview || p.calls[0].Status != "completed" {
		t.Fatalf("calls %+v", p.calls)
	}
}
