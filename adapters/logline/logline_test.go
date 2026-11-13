package logline

import (
	"bytes"
	"encoding/json"
	"log"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
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

func decode(t *testing.T, out *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("not JSON: %s", line)
		}
		records = append(records, record)
	}
	return records
}

func TestSubjectBecomesFieldsFromTheDataNotTheText(t *testing.T) {
	var out bytes.Buffer
	loggers, _ := New(&out, FormatJSON)
	subject := application.LogSubject{Bot: "writer", Node: "web-1", Task: "task-aaaaaaaa", Execution: "exec-bbbbbbbb", CorrelationID: "trace-1"}
	application.LogWith(loggers.Info, subject).Printf("node: finished")
	// The text names other ids; the subject's fields win over the regex.
	application.LogWith(loggers.Error, subject).Printf("daemon: task task-ffffffff0 and exec-eeeeeeee0 failed\n")
	records := decode(t, &out)
	for _, record := range records {
		for key, want := range map[string]string{"bot": "writer", "node": "web-1", "task": "task-aaaaaaaa", "execution": "exec-bbbbbbbb", "correlationId": "trace-1"} {
			if record[key] != want {
				t.Errorf("%s = %v, want %s in %v", key, record[key], want, record)
			}
		}
	}
	if records[0]["component"] != "node" || records[1]["level"] != "ERROR" || records[1]["msg"] != "daemon: task task-ffffffff0 and exec-eeeeeeee0 failed" {
		t.Errorf("records: %v", records)
	}
}

func TestEmptySubjectFieldsAreLeftOutAndRegexStaysTheFallback(t *testing.T) {
	var out bytes.Buffer
	loggers, _ := New(&out, FormatJSON)
	application.LogWith(loggers.Info, application.LogSubject{Node: "web-1"}).Printf("node: execution exec-0123456789abcdef is slow")
	record := decode(t, &out)[0]
	for _, key := range []string{"bot", "task", "correlationId"} {
		if _, ok := record[key]; ok {
			t.Errorf("%s is in %v", key, record)
		}
	}
	if record["node"] != "web-1" || record["execution"] != "exec-0123456789abcdef" {
		t.Errorf("record: %v", record)
	}
}

func TestSubjectsLayerOverEachOther(t *testing.T) {
	var out bytes.Buffer
	loggers, _ := New(&out, FormatJSON)
	node := application.LogWith(loggers.Info, application.LogSubject{Node: "web-1", Bot: "a"})
	application.LogWith(node, application.LogSubject{Bot: "b", Execution: "exec-1"}).Printf("x")
	record := decode(t, &out)[0]
	if record["node"] != "web-1" || record["bot"] != "b" || record["execution"] != "exec-1" {
		t.Errorf("record: %v", record)
	}
}

func TestTextIgnoresTheSubjectAndAPlainPrinterHasNone(t *testing.T) {
	var out bytes.Buffer
	loggers, _ := New(&out, FormatText)
	application.LogWith(loggers.Info, application.LogSubject{Bot: "writer", Task: "task-1"}).Printf("daemon: task task-1 completed")
	if strings.Contains(out.String(), "writer") || !strings.HasSuffix(out.String(), "daemon: task task-1 completed\n") {
		t.Errorf("text line: %q", out.String())
	}
	var plain bytes.Buffer
	std := log.New(&plain, "", 0)
	application.LogWith(std, application.LogSubject{Bot: "writer"}).Printf("hello")
	if plain.String() != "hello\n" {
		t.Errorf("plain: %q", plain.String())
	}
}

func TestWriterSendsStandardLogLinesTheSameWay(t *testing.T) {
	var out bytes.Buffer
	loggers, _ := New(&out, FormatJSON)
	std := log.New(loggers.Info.Writer(), "", 0)
	std.Printf("store: saved task task-0123456789abcdef")
	record := decode(t, &out)[0]
	if record["level"] != "INFO" || record["task"] != "task-0123456789abcdef" {
		t.Errorf("record: %v", record)
	}
	var text bytes.Buffer
	plain, _ := New(&text, FormatText)
	if plain.Info.Writer() == nil {
		t.Error("no writer in text")
	}
	(Logger{}).Printf("a zero logger discards")
}
