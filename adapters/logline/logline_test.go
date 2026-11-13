package logline

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestJSONLinesCarryLevelComponentAndIDs(t *testing.T) {
	var out bytes.Buffer
	loggers, err := New(&out, FormatJSON)
	if err != nil {
		t.Fatal(err)
	}
	loggers.Info.Printf("daemon: timer qa/ronda fired, created task task-d6689249d01ccb7d5a7571fa")
	loggers.Error.Printf("daemon: run: execution exec-0123456789abcdef: boom")
	loggers.Info.Printf("no component here")
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d:\n%s", len(lines), out.String())
	}
	var records []map[string]any
	for _, line := range lines {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("not JSON: %s", line)
		}
		records = append(records, record)
	}
	if records[0]["level"] != "INFO" || records[0]["component"] != "daemon" || records[0]["task"] != "task-d6689249d01ccb7d5a7571fa" {
		t.Errorf("first record: %v", records[0])
	}
	if records[1]["level"] != "ERROR" || records[1]["execution"] != "exec-0123456789abcdef" {
		t.Errorf("second record: %v", records[1])
	}
	if _, ok := records[2]["component"]; ok || records[2]["msg"] != "no component here" {
		t.Errorf("third record: %v", records[2])
	}
}

func TestTextStaysPlainAndUnknownFormatsAreRefused(t *testing.T) {
	var out bytes.Buffer
	loggers, err := New(&out, "")
	if err != nil {
		t.Fatal(err)
	}
	loggers.Error.Printf("daemon: run: boom")
	if strings.Contains(out.String(), "{") || !strings.HasSuffix(out.String(), "daemon: run: boom\n") {
		t.Errorf("text line: %q", out.String())
	}
	if _, err := New(&out, "yaml"); err == nil {
		t.Error("yaml was accepted")
	}
}
