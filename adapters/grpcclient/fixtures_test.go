package grpcclient

import (
	"time"

	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
)

var (
	t0 = time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	t1 = t0.Add(90 * time.Second)
)

func ts(t time.Time) *timestamppb.Timestamp { return timestamppb.New(t) }

func boolPtr(b bool) *bool { return &b }

func strPtr(s string) *string { return &s }

// fullBot has every field of a bot set.
func fullBot() *operatorv1.Bot {
	return &operatorv1.Bot{
		Id: "bot-a", TenantId: "default", Name: "Bot A", Owner: "ops", Mission: "Keep watch", AllowedTools: []string{"local_exec"},
		Model:   &operatorv1.ModelPolicy{Model: "m", RoutingPolicy: "local-only", AllowRemoteProviders: true, MaxContextTokens: 8000},
		Budgets: &operatorv1.Budgets{MaxModelCallsPerTask: 10, MaxToolCallsPerTask: 20, MaxDelegationDepth: 2, TaskTimeout: durationpb.New(5 * time.Minute)},
		Level:   "act-with-approval",
		ToolRules: &operatorv1.ToolRules{
			Allow: []*operatorv1.ToolRule{{Tool: "local_exec", Arguments: map[string]string{"program": "ls"}}},
			Deny:  []*operatorv1.ToolRule{{Tool: "rm"}},
		},
		Triggers: []*operatorv1.Trigger{
			{Type: "manual"},
			{Type: "timer", Name: "clock", Objective: "Look", Every: durationpb.New(15 * time.Minute)},
			{Type: "timer", Name: "hourly", Every: durationpb.New(time.Hour), Cron: "0 * * * *"},
			{Type: "webhook", Name: "hook", Secret: "<redacted>", SigningKey: "<redacted>", Events: []string{"a", "b"}, Interrupt: true},
		},
		Placement: &operatorv1.Placement{Nodes: []string{"n1"}, Selector: map[string]string{"zone": "a"}},
		Approvals: &operatorv1.Approvals{
			Ttl: durationpb.New(time.Hour), OnExpire: "deny", Approvers: []string{"ana"},
			Notify:      []*operatorv1.NotifyTarget{{Type: "webhook", Name: "n", Url: "https://x", Secret: "<redacted>", Command: []string{"notify"}}},
			AutoApprove: []*operatorv1.ToolRule{{Tool: "local_exec"}},
		},
		Delegation:    &operatorv1.Delegation{To: []string{"bot-b"}, MaxDepth: 2, BudgetShare: 0.5, Split: "even", MaxChildren: 3, FromTeam: boolPtr(false)},
		Groups:        []string{"team"},
		Events:        &operatorv1.Events{Emit: []*operatorv1.EmitRule{{Name: "info.*", MaxSeverity: "info"}}, PerMinute: 6, IntakePerMinute: 10, MaxDepth: 2, Interruptible: boolPtr(true)},
		Subscriptions: []*operatorv1.Subscription{{Topic: "t", Mode: "task", From: []string{"bot-b"}, Urgent: true, Objective: "o", Status: "active", AskedBy: "bot-a", Why: "w", AskedAt: ts(t0)}},
		Runtime: &operatorv1.Runtime{Kind: "jev", Jev: &operatorv1.JevProgram{
			Server: "s", Start: "st", Wait: "w", Stop: "sp", Model: "jm",
			Act:  &operatorv1.JevAct{Tool: "tool", Choice: "c", Verdict: "v", Pass: []string{"p"}},
			Tell: &operatorv1.JevTell{Name: "tell", Fields: []string{"f"}},
		}},
		Skills:  []*operatorv1.SkillPin{{Name: "sk", Version: "1.0.0", Catalog: "cat"}},
		Version: "1.2.0", Status: "Enabled", CreatedAt: ts(t0), UpdatedAt: ts(t1),
		Staged: &operatorv1.StagedDefinition{Version: "1.3.0", StagedAt: ts(t1), Change: []byte(`{"mission":"x"}`)},
	}
}

func fullTask(id string) *operatorv1.Task {
	return &operatorv1.Task{
		Id: id, BotId: "bot-a", Objective: "Do it", Status: "WaitingApproval", IdempotencyKey: "k", CorrelationId: "corr",
		Trigger: &operatorv1.TriggerRef{Type: "timer", Name: "clock"}, Input: []byte(`{"a":1}`), ExecutionIds: []string{"e-1"},
		FailureReason: "boom", Priority: "urgent", Waiting: "no node", Children: []string{"t-2"}, GrantedModelCalls: 4, GrantedToolCalls: 5,
		Skills: []*operatorv1.SkillPin{{Name: "sk", Version: "1", Catalog: "c"}},
		Approval: &operatorv1.ApprovalWait{
			CallId: "c1", Tool: "local_exec", Since: ts(t0), Deadline: ts(t1), OnExpire: "deny", Outcome: "approved", ClosedAt: ts(t1),
		},
		Delegation: &operatorv1.TaskLineage{
			ParentTaskId: "t-0", ParentExecutionId: "e-0", ParentBotId: "bot-p", Depth: 1, MaxDepth: 3, Chain: []string{"bot-p"}, EventId: "ev",
			Grant: &operatorv1.DelegationGrant{MaxModelCalls: 2, MaxToolCalls: 3, Deadline: ts(t1)},
		},
		CreatedAt: ts(t0), UpdatedAt: ts(t1),
	}
}

func fullExecution(id string) *operatorv1.Execution {
	return &operatorv1.Execution{
		Id: id, TaskId: "t-1", BotId: "bot-a", StartedAt: ts(t0), EndedAt: ts(t1), Outcome: "Completed",
		Result: &operatorv1.Result{
			Answer: "done", SessionId: "s1", Sessions: []string{"s1", "s2"}, NotStarted: true,
			Artifacts:       []*operatorv1.Artifact{{Name: "r.md", MediaType: "text/markdown", Reference: "ref", Size: 5, Sha256: "ab", Description: "d", Source: "run", SessionId: "s1", CallId: "c1", Stored: true}},
			ToolCalls:       []*operatorv1.ToolCall{{Name: "local_exec", CallId: "c1", Decision: "approve", Status: "ok", Preview: true}},
			PendingDecision: &operatorv1.PendingDecision{Tool: "local_exec", CallId: "c2", SessionId: "s1", Arguments: []byte(`{"x":1}`)},
		},
		Usage:         &operatorv1.Usage{Requests: 3, PromptTokens: 100, CachedTokens: 10, CompletionTokens: 20, ReasoningTokens: 5, ToolCalls: 2, CostUsd: 0.25, CostUnknown: true},
		FailureReason: "none", TracePath: "/trace",
		Policy:      &operatorv1.PolicyRecord{Level: "observe", AllowedTools: []string{"a"}, RuntimeLevel: "r", Enforced: true, Path: "/p", Delegation: "d"},
		Correlation: &operatorv1.CorrelationRecord{BotId: "bot-a", TaskId: "t-1", ExecutionId: id, TraceId: "tr", Carried: true, Path: "/c"},
		Kind:        "run", Decision: "approve", NodeId: "n1", LeaseUntil: ts(t1), Receipt: []byte(`{"r":1}`),
	}
}

func fullEvent(id, typ string) *operatorv1.Event {
	payload, _ := structpb.NewStruct(map[string]any{"items": []any{map[string]any{"kind": "tool_started", "tool": "x"}}})
	return &operatorv1.Event{
		EventId: id, SchemaVersion: "1", Type: typ, TenantId: "default", BotId: "bot-a", GroupName: "team", NodeId: "n1", TaskId: "t-1",
		ExecutionId: "e-1", CorrelationId: "corr", CreatedAt: ts(t0), Payload: payload,
	}
}

func fullNode() *operatorv1.Node {
	return &operatorv1.Node{
		Id: "n1", HostName: "host", Labels: map[string]string{"a": "1"}, OperatorLabels: map[string]string{"a": "1"}, ReportedLabels: map[string]string{"b": "2"},
		Capabilities: &operatorv1.NodeCapabilities{AxlrVersion: "0.20", NodeVersion: "v", NodeRevision: "r", PolicySupported: true, CorrelationSupported: true, Dedicated: true, Runtimes: []string{"axlr"}, Providers: []string{"p"}, SkillsSupported: true},
		Status:       "Online", RegisteredAt: ts(t0), LastSeenAt: ts(t1), CertificateSerial: "01", CertificateNotAfter: ts(t1), RevokedAt: ts(t1),
	}
}
