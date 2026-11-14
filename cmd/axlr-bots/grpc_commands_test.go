package main

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/underpass-ai/axlr-bots/application"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
)

// grpcCLI runs commands over gRPC against the fake, the socket given.
func grpcCLI(t *testing.T, socket string, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	global := []string{"--state", t.TempDir(), "--grpc-socket", socket, "--actor", "test:grpc"}
	code := run(append(global, args...), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func detail(t *testing.T, code codes.Code, d *operatorv1.ErrorDetail) error {
	t.Helper()
	s, err := status.New(code, d.GetMessage()).WithDetails(d)
	if err != nil {
		t.Fatal(err)
	}
	return s.Err()
}

func TestCommandsSpeakGRPCToTheDaemonSocket(t *testing.T) {
	api := newFakeOperatorAPI()
	socket := api.serveSocket(t)
	api.answer(apiMethod("BotsService", "ListBots"), &operatorv1.ListBotsResponse{Bots: []*operatorv1.Bot{{Id: "bot-a", Name: "A", Status: "Enabled", AllowedTools: []string{"local_read"}}}})
	api.answer(apiMethod("TasksService", "SubmitTask"), &operatorv1.SubmitTaskResponse{Created: true, Task: &operatorv1.Task{Id: "t-1", BotId: "bot-a", Status: "Pending"}})
	api.answer(apiMethod("TasksService", "DecideTask"), &operatorv1.DecideTaskResponse{Task: &operatorv1.Task{Id: "t-1", Status: "Running"}, Execution: &operatorv1.Execution{Id: "e-1", Outcome: "Running"}})
	api.answer(apiMethod("OverviewService", "GetOverview"), &operatorv1.GetOverviewResponse{Tasks: map[string]int32{"Pending": 2}})

	code, out, errText := grpcCLI(t, socket, "bot", "list")
	var bots []map[string]any
	if code != exitOK || json.Unmarshal([]byte(out), &bots) != nil || len(bots) != 1 || bots[0]["id"] != "bot-a" {
		t.Fatalf("bot list: %d %q %q", code, out, errText)
	}
	code, out, errText = grpcCLI(t, socket, "task", "submit", "bot-a", "--objective", "go", "--key", "k1")
	if code != exitOK || !strings.Contains(out, `"created": true`) || !strings.Contains(out, `"id": "t-1"`) {
		t.Fatalf("task submit: %d %q %q", code, out, errText)
	}
	code, out, errText = grpcCLI(t, socket, "task", "approve", "t-1", "--call", "c1")
	if code != exitOK || !strings.Contains(out, `"outcome": "Running"`) {
		t.Fatalf("task approve: %d %q %q", code, out, errText)
	}
	if got := api.seen(apiMethod("TasksService", "DecideTask")).Get("x-axlr-actor"); len(got) != 1 || got[0] != "test:grpc" {
		t.Errorf("the --actor goes as x-axlr-actor: %v", got)
	}
	if code, out, _ = grpcCLI(t, socket, "overview"); code != exitOK || !strings.Contains(out, `"Pending": 2`) {
		t.Errorf("overview: %d %q", code, out)
	}
	if code, out, _ := grpcCLI(t, socket, "health"); code != exitOK || strings.TrimSpace(out) != "ok" {
		t.Errorf("health: %d %q", code, out)
	}
}

func TestAnErrorDetailBecomesTheMessageTheCommandPrinted(t *testing.T) {
	api := newFakeOperatorAPI()
	socket := api.serveSocket(t)
	api.fail(apiMethod("BotsService", "GetBot"), detail(t, codes.NotFound, &operatorv1.ErrorDetail{Kind: "not_found", Message: "bot nope not found", Resource: "bot", Id: "nope"}))
	api.fail(apiMethod("TasksService", "RunTask"), detail(t, codes.FailedPrecondition, &operatorv1.ErrorDetail{Kind: "state", Message: "task t-1 is Completed, not Pending"}))
	api.fail(apiMethod("TasksService", "GetTask"), detail(t, codes.Unauthenticated, &operatorv1.ErrorDetail{Kind: "unauthorized", Message: "x"}))
	for args, want := range map[string]string{
		"bot get nope": "axlr-bots: bot nope not found\n",
		"task run t-1": "axlr-bots: task t-1 is Completed, not Pending\n",
		"task get t-1": "axlr-bots: unauthorized: the operator address takes Authorization: Bearer <token>\n",
	} {
		code, out, errText := grpcCLI(t, socket, strings.Fields(args)...)
		if code != exitError || out != "" || errText != want {
			t.Errorf("%s: %d %q %q, want %q", args, code, out, errText, want)
		}
	}
}

func TestCommandsReachARemoteControlPlaneOverGRPC(t *testing.T) {
	api := newFakeOperatorAPI()
	address, tokenFile, caFile := api.serveTLS(t)
	api.answer(apiMethod("BotsService", "ListBots"), &operatorv1.ListBotsResponse{Bots: []*operatorv1.Bot{{Id: "remote-bot"}}})
	api.answer(apiMethod("OverviewService", "GetBuild"), &operatorv1.GetBuildResponse{})
	state := filepath.Join(t.TempDir(), "must-stay-untouched")
	run1 := func(control string, args ...string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		global := []string{"--state", state, "--control", control, "--token-file", tokenFile, "--ca-file", caFile, "--cert-file", api.certFile, "--key-file", api.keyFile}
		code := run(append(global, args...), &stdout, &stderr)
		return code, stdout.String(), stderr.String()
	}
	for _, control := range []string{address, "https://" + address, "grpcs://" + address + "/"} {
		if code, out, errText := run1(control, "bot", "list"); code != exitOK || !strings.Contains(out, "remote-bot") {
			t.Fatalf("%s: %d %q %q", control, code, out, errText)
		}
	}
	if got := api.seen(apiMethod("BotsService", "ListBots")).Get("authorization"); len(got) != 1 || got[0] != "Bearer remote-token" {
		t.Errorf("the token goes as bearer metadata: %v", got)
	}
	if len(api.operators) == 0 || api.operators[0] != "alice" {
		t.Errorf("the calls carry the operator's certificate: %v", api.operators)
	}
	if code, out, _ := run1(address, "health"); code != exitOK || strings.TrimSpace(out) != "ok" {
		t.Errorf("remote health: %d %q", code, out)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Error("a remote command must not open the state directory")
	}
	if code, _, errText := run1("127.0.0.1:1", "health"); code != exitError || !strings.Contains(errText, "no service answers on 127.0.0.1:1") {
		t.Errorf("nobody there: %d %q", code, errText)
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--control", address, "--token-file", filepath.Join(t.TempDir(), "missing"), "health"}, &stdout, &stderr); code != exitError || !strings.Contains(stderr.String(), "--token-file") {
		t.Errorf("a missing token file: %d %q", code, stderr.String())
	}
}

func TestTheControlFlagsAreChecked(t *testing.T) {
	for control, ok := range map[string]bool{"host:7880": true, "https://host:7880": true, "grpcs://host:7880": true, "host": false, "https://host:7880/v1": false, ":7880": false, "host:": false} {
		err := remoteControl{URL: control, TokenFile: "t"}.validate()
		if (err == nil) != ok {
			t.Errorf("%q: %v", control, err)
		}
	}
	if err := (remoteControl{URL: "host:7880"}).validate(); err == nil {
		t.Error("a control address needs the token file")
	}
	if err := (remoteControl{TokenFile: "t"}).validate(); err == nil {
		t.Error("a token file goes with --control")
	}
	var stdout, stderr bytes.Buffer
	if code := run([]string{"--transport", "rest", "health"}, &stdout, &stderr); code != exitUsage {
		t.Errorf("--transport is gone: %d", code)
	}
}

func TestTheGRPCSocketDefaultsNextToTheOther(t *testing.T) {
	if got := grpcSocketPath("", false, false, "/s/axlr-bots.sock", "/state"); got != filepath.Join("/state", grpcSocketFileName) {
		t.Errorf("default: %q", got)
	}
	if got := grpcSocketPath("", false, true, "", "/state"); got != "" {
		t.Errorf("an explicit empty --socket means no daemon: %q", got)
	}
	if got := grpcSocketPath("/x.sock", true, true, "", "/state"); got != "/x.sock" {
		t.Errorf("named: %q", got)
	}
	if got := grpcSocketPath("", true, true, "", "/state"); got != "" {
		t.Errorf("an explicit empty --grpc-socket: %q", got)
	}
}

func TestFiringATriggerOverGRPCNeedsTheHTTPSocket(t *testing.T) {
	api := newFakeOperatorAPI()
	socket := api.serveSocket(t)
	code, _, errText := grpcCLI(t, socket, "trigger", "fire", "bot-a", "hook", "--body", `{"a":1}`)
	if code != exitError || errText == "" {
		t.Errorf("no HTTP socket answers: %d %q", code, errText)
	}
	var stdout, stderr bytes.Buffer
	args := []string{"--state", t.TempDir(), "--grpc-socket", socket, "--socket", "", "trigger", "fire", "bot-a", "hook", "--body", `{"a":1}`}
	if code := run(args, &stdout, &stderr); code != exitError || !strings.Contains(stderr.String(), "not part of the gRPC operator API") {
		t.Errorf("without any HTTP socket it says so: %d %q", code, stderr.String())
	}
}

func activityFixture() *operatorv1.TaskActivity {
	at := func(s int) *timestamppb.Timestamp {
		return timestamppb.New(time.Date(2026, 11, 14, 10, 0, s, 0, time.UTC))
	}
	payload, _ := structpb.NewStruct(map[string]any{"items": []any{
		map[string]any{"kind": "tool.started", "tool": "local_exec", "callId": "c1", "summary": "ls -l"},
		map[string]any{"kind": "tool.ended", "tool": "local_exec", "callId": "c1", "status": "ok", "durationMs": 1200},
		map[string]any{"kind": "tool.started", "tool": "local_read", "callId": "c2"},
		map[string]any{"kind": "tool.ended", "tool": "local_read", "callId": "c2", "error": "denied by policy", "durationMs": 5},
		map[string]any{"kind": "decision.required", "tool": "local_write"},
		map[string]any{"kind": "delegation", "bot": "bot-b", "taskId": "t-2"},
		map[string]any{"kind": "text", "text": "words are not tools"},
	}})
	child := &operatorv1.TaskActivity{
		Task:         &operatorv1.Task{Id: "t-2", BotId: "bot-b", Status: "Completed", Objective: "child work", Delegation: &operatorv1.TaskLineage{ParentBotId: "bot-a", ParentTaskId: "t-1"}},
		Executions:   []*operatorv1.ExecutionActivity{{Execution: &operatorv1.Execution{Id: "e-2", Outcome: "Completed", StartedAt: at(5), EndedAt: at(9), Usage: &operatorv1.Usage{Requests: 1, CostUsd: 0.01}}}},
		SubtreeUsage: &operatorv1.Usage{Requests: 1, CostUsd: 0.01},
	}
	return &operatorv1.TaskActivity{
		Task: &operatorv1.Task{Id: "t-1", BotId: "bot-a", Status: "Completed", Objective: "main\nwork", Waiting: "no node"},
		Executions: []*operatorv1.ExecutionActivity{{
			Execution: &operatorv1.Execution{Id: "e-1", Outcome: "Completed", NodeId: "n1", StartedAt: at(0), EndedAt: at(4), Usage: &operatorv1.Usage{Requests: 3, ToolCalls: 2, PromptTokens: 100, CompletionTokens: 20, CostUnknown: true}, FailureReason: "partial"},
			ProgressCut: &operatorv1.ProgressCut{
				Reason: operatorv1.ProgressCutReason_PROGRESS_CUT_REASON_EVENT_LIMIT, EventsKept: 400, BytesKept: 2048, CutAt: at(3), Dropped: 2,
				Completion: operatorv1.CompletionSource_COMPLETION_SOURCE_TRANSCRIPT, TranscriptSessions: []string{"sess-1"}, CompletionBounded: true,
			},
			CompletedItems: []*operatorv1.ProgressItem{
				{Kind: "tool_started", Tool: "late_tool", CallId: "c9", McpTool: "srv.tool"},
				{Kind: "tool_ended", Tool: "late_tool", CallId: "c9", Status: "ok", Duration: durationpb.New(2 * time.Second)},
			},
		}},
		Events:       []*operatorv1.Event{{EventId: "ev-1", Type: "bot.task.submitted", CreatedAt: at(0)}, {EventId: "ev-2", Type: "bot.task.progress", TaskId: "t-1", ExecutionId: "e-1", CreatedAt: at(1), Payload: payload}},
		Children:     []*operatorv1.TaskActivity{child},
		SubtreeUsage: &operatorv1.Usage{Requests: 4, ToolCalls: 2, CostUsd: 0.11, CostUnknown: true},
	}
}

func TestTaskActivityPrintsTheTreeAsText(t *testing.T) {
	api := newFakeOperatorAPI()
	socket := api.serveSocket(t)
	api.answer(apiMethod("ActivityService", "GetTaskActivity"), &operatorv1.GetTaskActivityResponse{Activity: activityFixture()})
	code, out, errText := grpcCLI(t, socket, "task", "activity", "t-1", "--depth", "3")
	if code != exitOK || errText != "" {
		t.Fatalf("exit %d: %s", code, errText)
	}
	for _, want := range []string{
		"Task t-1  bot bot-a  Completed", "objective: main work", "waiting: no node",
		"Execution e-1  Completed  node n1  4s  cost unknown", "3 model request(s), 2 tool call(s), 100 prompt / 20 completion tokens", "failed: partial",
		"ok       local_exec  1.2s  ls -l", "FAILED   local_read  5ms  denied by policy", "approval local_write", "delegated bot-b  task t-2",
		"progress timeline cut (event_limit)", "400 event(s), 2048 byte(s) kept, 2 item(s) dropped", "completed from the transcript (sess-1) (bounded: later items are not shown)",
		"ok       late_tool (srv.tool)  2s  [from transcript]", "bot.task.submitted", "  Task t-2  bot bot-b  Completed  delegated by bot-a (task t-1)",
		"Execution e-2  Completed  4s  $0.0100", "in all: $0.1100 and more (some cost unknown), 4 model request(s), 2 tool call(s)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "words are not tools") || strings.Contains(out, "bot.task.progress") {
		t.Errorf("text and progress events are not tools:\n%s", out)
	}
}

func TestTaskActivityPrintsJSON(t *testing.T) {
	api := newFakeOperatorAPI()
	socket := api.serveSocket(t)
	api.answer(apiMethod("ActivityService", "GetTaskActivity"), &operatorv1.GetTaskActivityResponse{Activity: activityFixture()})
	code, out, errText := grpcCLI(t, socket, "task", "activity", "t-1", "--json")
	var tree struct {
		Task       struct{ ID string } `json:"task"`
		Executions []struct {
			ProgressCut struct{ Reason string } `json:"progressCut"`
		} `json:"executions"`
		Children []struct{ Task struct{ ID string } } `json:"children"`
	}
	if code != exitOK || json.Unmarshal([]byte(out), &tree) != nil || tree.Task.ID != "t-1" || tree.Executions[0].ProgressCut.Reason != "event_limit" || tree.Children[0].Task.ID != "t-2" {
		t.Fatalf("json: %d %s %s", code, out, errText)
	}
}

func TestTaskActivityFollowsTheTreeUntilItEnds(t *testing.T) {
	api := newFakeOperatorAPI()
	socket := api.serveSocket(t)
	api.watch = []*operatorv1.WatchTaskActivityResponse{
		{Cursor: "1", Update: &operatorv1.WatchTaskActivityResponse_Snapshot{Snapshot: &operatorv1.TaskActivity{Task: &operatorv1.Task{Id: "t-1", BotId: "bot-a", Status: "Running"}}}},
		{Cursor: "2", Update: &operatorv1.WatchTaskActivityResponse_Heartbeat{Heartbeat: &operatorv1.Heartbeat{}}},
		{Cursor: "3", Update: &operatorv1.WatchTaskActivityResponse_TaskChanged{TaskChanged: &operatorv1.TaskChanged{Task: &operatorv1.Task{Id: "t-1", BotId: "bot-a", Status: "Completed"}}}},
	}
	code, out, errText := grpcCLI(t, socket, "task", "activity", "t-1", "--watch")
	if code != exitOK || errText != "" || !strings.Contains(out, "Running") || !strings.Contains(out, "Completed") || strings.Count(out, "--- ") != 1 {
		t.Fatalf("watch: %d %q %q", code, out, errText)
	}
	code, out, _ = grpcCLI(t, socket, "task", "activity", "t-1", "--watch", "--json")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if code != exitOK || len(lines) != 2 || !strings.Contains(lines[1], `"status":"Completed"`) {
		t.Errorf("watch --json is one line per change: %d %q", code, out)
	}
	api.watch = nil
	api.fail(apiMethod("ActivityService", "GetTaskActivity"), detail(t, codes.NotFound, &operatorv1.ErrorDetail{Kind: "not_found", Message: "x", Resource: "task", Id: "nope"}))
	if code, _, errText := grpcCLI(t, socket, "task", "activity", "nope"); code != exitError || errText != "axlr-bots: task nope not found\n" {
		t.Errorf("a missing task: %d %q", code, errText)
	}
}

type noActivityService struct{ application.OperatorService }

func TestTaskActivityNeedsGRPC(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := taskActivity(context.Background(), noActivityService{}, []string{"t-1"}, &stdout, &stderr); code != exitError || !strings.Contains(stderr.String(), "over gRPC") {
		t.Errorf("the in-process service: %d %q", code, stderr.String())
	}
	for _, args := range [][]string{nil, {"--json"}, {"t-1", "extra"}, {"t-1", "--depth", "-1"}, {"t-1", "--bogus"}} {
		stdout.Reset()
		stderr.Reset()
		if code := taskActivity(context.Background(), noActivityService{}, args, &stdout, &stderr); code != exitUsage {
			t.Errorf("%v: exit %d", args, code)
		}
	}
}
