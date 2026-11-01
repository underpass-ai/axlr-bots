package nodeproto

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func TestAssignmentRoundTrip(t *testing.T) {
	tools, _ := domain.NewAllowedTools([]string{"local_read", "local_write"})
	rule, _ := domain.NewToolRule("local_write", map[string]string{"path": "notes/**"})
	rules, _ := domain.NewToolRules([]domain.ToolRule{rule}, nil, tools)
	budgets, _ := domain.NewBudgets(1, 2, 0, time.Minute)
	run := application.Assignment{ExecutionID: "exec-1", Kind: domain.ExecutionRun, LeaseUntil: time.Date(2026, 10, 24, 1, 0, 0, 0, time.UTC), Run: &application.ExecutionRequest{
		ExecutionID: "exec-1", TaskID: "task-1", BotID: "bot", TenantID: "t", CorrelationID: "c", Objective: "list the files", Model: "m",
		Level: domain.AutonomyActWithApproval, AllowedTools: tools.Names(), ToolRules: rules, Budgets: budgets, Timeout: 90 * time.Second,
	}}
	raw, _ := json.Marshal(FromAssignment(run))
	var m AssignmentMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	back, err := ToAssignment(m)
	if err != nil {
		t.Fatal(err)
	}
	if back.ExecutionID != "exec-1" || back.Kind != domain.ExecutionRun || back.Run == nil || back.Run.Objective != "list the files" || back.Run.Level != domain.AutonomyActWithApproval || back.Run.Timeout != 90*time.Second || !back.Run.ToolRules.Constrains("local_write") || back.Run.CorrelationID != "c" || !back.LeaseUntil.Equal(run.LeaseUntil) {
		t.Errorf("run = %+v", back.Run)
	}
	decide := application.Assignment{ExecutionID: "exec-2", Kind: domain.ExecutionDecide, Decide: &application.DecisionRequest{
		ExecutionID: "exec-2", BotID: "bot", SessionID: "sess-1", CallID: "c1", Decision: "approve", Level: domain.AutonomyObserve, AllowedTools: tools.Names(), ToolRules: rules, Timeout: time.Minute,
	}}
	raw, _ = json.Marshal(FromAssignment(decide))
	_ = json.Unmarshal(raw, &m)
	back, err = ToAssignment(m)
	if err != nil || back.Decide == nil || back.Decide.SessionID != "sess-1" || back.Decide.Decision != "approve" || back.Decide.Timeout != time.Minute || back.Run != nil {
		t.Errorf("decide = %v %+v", err, back.Decide)
	}
	bad := []AssignmentMessage{
		{ExecutionID: "", Kind: "run"},
		{ExecutionID: "exec-1", Kind: "odd"},
		{ExecutionID: "exec-1", Kind: "run"},
		{ExecutionID: "exec-1", Kind: "decide"},
		{ExecutionID: "exec-1", Kind: "run", Run: &RunMessage{BotID: "Bad", Objective: "o"}},
		{ExecutionID: "exec-1", Kind: "run", Run: &RunMessage{BotID: "bot", Objective: ""}},
		{ExecutionID: "exec-1", Kind: "run", Run: &RunMessage{BotID: "bot", Objective: "o", Level: "odd"}},
		{ExecutionID: "exec-1", Kind: "run", Run: &RunMessage{BotID: "bot", Objective: "o", AllowedTools: []string{"Bad"}}},
		{ExecutionID: "exec-1", Kind: "run", Run: &RunMessage{BotID: "bot", Objective: "o", AllowedTools: []string{"local_read"}, ToolRules: domain.ToolRulesSnapshot{Allow: []domain.ToolRuleSnapshot{{Tool: "local_write"}}}}},
		{ExecutionID: "exec-1", Kind: "decide", Decide: &DecideMessage{BotID: "Bad"}},
		{ExecutionID: "exec-1", Kind: "decide", Decide: &DecideMessage{BotID: "bot", Level: "odd"}},
		{ExecutionID: "exec-1", Kind: "decide", Decide: &DecideMessage{BotID: "bot", SessionID: "", Decision: "approve"}},
	}
	for i, m := range bad {
		if _, err := ToAssignment(m); err == nil {
			t.Errorf("bad message %d accepted", i)
		}
	}
}

func TestReportRoundTrip(t *testing.T) {
	report := application.ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "done", SessionID: "s"}, Usage: domain.Usage{Requests: 2}, TracePath: "node:web-1:/t.jsonl", Policy: &domain.PolicyRecord{Level: "observe", Enforced: true}}
	raw, _ := json.Marshal(FromReport(report))
	var m ReportMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	back, err := ToReport(m)
	if err != nil || back.Outcome != domain.ExecutionCompleted || back.Result.Answer != "done" || back.TracePath != report.TracePath || back.Policy.Enforced != true || back.Usage.Requests != 2 {
		t.Errorf("report = %v %+v", err, back)
	}
	for i, bad := range []ReportMessage{{Outcome: "odd"}, {Outcome: "Running"}, {Outcome: "Completed", Usage: domain.Usage{Requests: -1}}} {
		if _, err := ToReport(bad); err == nil {
			t.Errorf("bad report %d accepted", i)
		}
	}
}

func TestEnrollTokenRoundTrip(t *testing.T) {
	token := EnrollToken{ID: "enr-1", Secret: "s3cret-s3cret-s3cret", NodeID: "web-2", CAPEM: "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n"}
	encoded := token.Encode()
	if !strings.HasPrefix(encoded, "axlr1.") || strings.ContainsAny(encoded, " \n+/=") {
		t.Errorf("encoded = %q", encoded)
	}
	back, err := DecodeEnrollToken(" " + encoded + "\n")
	if err != nil || back != token {
		t.Errorf("decode: %v %+v", err, back)
	}
	for name, bad := range map[string]string{
		"prefix":     "nope." + encoded[6:],
		"base64":     "axlr1.!!!",
		"json":       "axlr1." + base64.RawURLEncoding.EncodeToString([]byte("{")),
		"incomplete": (EnrollToken{ID: "enr-1"}).Encode(),
	} {
		if _, err := DecodeEnrollToken(bad); err == nil {
			t.Errorf("%s: decoded", name)
		}
	}
}

// A Jev bot's run carries its runtime and the task's input, and reads
// back as it was; a run without a runtime is AXLR's, as an older control
// plane's are.
func TestARunCarriesItsRuntimeAndInput(t *testing.T) {
	tools, _ := domain.NewAllowedTools([]string{"batches", "process", "listen", "stop"})
	runtime, err := domain.NewRuntime(&domain.RuntimeSnapshot{Kind: "jev", Jev: &domain.JevProgramSnapshot{
		Server: "queue", Start: "batches", Wait: "listen", Stop: "stop", Act: domain.JevActSnapshot{Tool: "process", Choice: "batch", Verdict: "judge", Pass: []string{"priority"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	input := json.RawMessage(`{"name":"info.notice.load","data":{"priority":"high"}}`)
	run := application.Assignment{ExecutionID: "exec-1", Kind: domain.ExecutionRun, Run: &application.ExecutionRequest{
		ExecutionID: "exec-1", TaskID: "task-1", BotID: "worker-1", TenantID: "t", Objective: "process", Level: domain.AutonomyActWithinPolicy, AllowedTools: tools.Names(), Runtime: runtime, Input: input,
	}}
	raw, _ := json.Marshal(FromAssignment(run))
	if !strings.Contains(string(raw), `"runtime":{"kind":"jev"`) || !strings.Contains(string(raw), `"input":{"name":"info.notice.load"`) {
		t.Fatalf("wire = %s", raw)
	}
	var m AssignmentMessage
	_ = json.Unmarshal(raw, &m)
	back, err := ToAssignment(m)
	if err != nil {
		t.Fatal(err)
	}
	program, ok := back.Run.Runtime.Jev()
	if !ok || program.Act() != "process" || program.Pass()[0] != "priority" || string(back.Run.Input) != string(input) {
		t.Errorf("back = %+v", back.Run)
	}
	m.Run.Runtime = &domain.RuntimeSnapshot{Kind: "jev"}
	if _, err := ToAssignment(m); err == nil {
		t.Error("a jev runtime without its program was accepted")
	}
	plain := application.Assignment{ExecutionID: "exec-2", Kind: domain.ExecutionRun, Run: &application.ExecutionRequest{ExecutionID: "exec-2", BotID: "bot", Objective: "o", Level: domain.AutonomyObserve, AllowedTools: tools.Names()}}
	raw, _ = json.Marshal(FromAssignment(plain))
	if strings.Contains(string(raw), `"runtime"`) || strings.Contains(string(raw), `"input"`) {
		t.Errorf("an AXLR run names a runtime: %s", raw)
	}
	var plainMessage AssignmentMessage
	_ = json.Unmarshal(raw, &plainMessage)
	if back, err := ToAssignment(plainMessage); err != nil || back.Run.Runtime.Kind() != domain.RuntimeAXLR {
		t.Errorf("plain = %v %+v", err, back.Run)
	}
}
