package transcriptaudit

import (
	"strings"
	"testing"
)

func TestSessionLinesLeavesTheMissionOutAndLabelsResults(t *testing.T) {
	session := `{"messages":[
	 {"role":"user","content":"mission and playbook"},
	 {"role":"assistant","content":"","tool_calls":[{"id":"c1","name":"local_exec","arguments":{"program":"/x/bin/arreglo","args":["plan","a-1"]}},{"id":"c2","name":"local_read","arguments":{"path":"playbook.md"}}]},
	 {"role":"tool","tool_call_id":"c1","content":"{\"ok\": false}"},
	 {"role":"tool","tool_call_id":"c2","content":"rules"},
	 {"role":"tool","tool_call_id":"c9","content":"?"}]}`
	lines, err := sessionLines([]byte(session))
	if err != nil || len(lines) != 4 {
		t.Fatalf("lines: %+v %v", lines, err)
	}
	if strings.Contains(lines[0].text, "mission") || lines[1].label != "local_exec arreglo plan a-1" || lines[2].label != "local_read playbook.md" || lines[3].label != "" || lines[0].label != "" {
		t.Fatalf("lines = %+v", lines)
	}
	if strings.Contains(lines[1].text, "\n ") {
		t.Errorf("not compact: %s", lines[1].text)
	}
	for _, bad := range []string{"nope", `{"messages":[]}`, `{"messages":[{},{"a":}]}`} {
		if _, err := sessionLines([]byte(bad)); err == nil {
			t.Errorf("%q gave lines", bad)
		}
	}
	if label := callLabel("t", []byte(`{"path":"`+strings.Repeat("é", 200)+`"}`)); len(label) > 160 {
		t.Errorf("label of %d bytes", len(label))
	}
}

func TestBlocksKeepLinesWholeAndLabelPieces(t *testing.T) {
	small := line{text: strings.Repeat("a", 400)}
	long := line{text: strings.Repeat("é", 3000), label: "local_read playbook.md"}
	out := blocks([]line{small, small, small, long, small}, 1000)
	// Two small lines fit a block; the third starts another; the long line
	// goes in pieces; the last small line stands alone.
	if len(out) < 5 {
		t.Fatalf("%d blocks", len(out))
	}
	if out[0] != small.text+"\n"+small.text || out[1] != small.text {
		t.Errorf("first blocks = %q / %q", out[0][:20], out[1][:20])
	}
	pieceCount := 0
	for _, b := range out {
		if len(b) > 1000 {
			t.Errorf("block of %d bytes", len(b))
		}
		if strings.HasPrefix(b, "[Piece ") {
			pieceCount++
			if !strings.Contains(b, "the result of tool call local_read playbook.md]") {
				t.Errorf("piece without its call: %q", b[:80])
			}
		}
	}
	if pieceCount < 2 || out[len(out)-1] != small.text {
		t.Errorf("pieces %d, last %q", pieceCount, out[len(out)-1][:10])
	}
	if got := pieces(line{text: "xyz"}, 10); len(got) != 1 || !strings.Contains(got[0], "tool call message]") {
		t.Errorf("unlabelled piece = %q", got)
	}
	if cutBytes("abc", 5) != "abc" || cutBytes("éé", 3) != "é" {
		t.Error("cutBytes")
	}
}
