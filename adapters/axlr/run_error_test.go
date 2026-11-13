package axlr

import (
	"strings"
	"testing"
)

func TestCompactRunErrorKeepsTheProgramsWords(t *testing.T) {
	raw := `{"protocol_version":1,"request_id":"call_ec3e8d6dfdac4f01a0a9309af5315ca1","tool":"exec","status":"completed","started_at":"2026-11-12T19:44:40.175910879Z","finished_at":"2026-11-12T19:44:40.204272178Z","duration_ms":28,"output":{"exit_code":1,"stdout":"{\"ok\": false, \"error\": \"input file is empty\"}\n","stderr":"","captured_bytes":84,"discarded_bytes":0,"truncated":false},"error":null}`
	got := compactRunError(raw)
	want := `{"protocol_version":1,"status":"completed","output":{"exit_code":1,"stdout":"{\"ok\": false, \"error\": \"input file is empty\"}"}}`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if n := len([]rune(got)); n > 240 {
		t.Fatalf("%d characters: the progress item would cut the program's words", n)
	}
}

func TestCompactRunErrorLeavesOtherErrorsAlone(t *testing.T) {
	for _, raw := range []string{
		"permission denied",
		`{"call_id":"call_x","error":"send the continuation alone"}`,
		`{"protocol_version":1,"status":"failed","error":{"code":"x"}}`,
		`{"protocol_version":1,"output":{"exit_code":2,"stdout":"cut…`,
	} {
		if got := compactRunError(raw); got != raw {
			t.Errorf("%s: changed to %s", raw, got)
		}
	}
}

func TestCompactRunErrorKeepsStderrAndAnError(t *testing.T) {
	got := compactRunError(`{"protocol_version":1,"request_id":"r","status":"completed","output":{"exit_code":2,"stdout":"","stderr":"fatal: <boom>\n"},"error":"x"}`)
	if !strings.Contains(got, `"stderr":"fatal: <boom>"`) || !strings.Contains(got, `"error":"x"`) || strings.Contains(got, "request_id") {
		t.Fatal(got)
	}
}
