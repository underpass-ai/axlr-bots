package grpcapi

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/domain"
)

func yes() *bool { v := true; return &v }

// A definition that sets every field goes to the use case's types and a
// patch with the same content comes back identical: no field is lost on
// the way.
func TestPatchSurvivesTheRoundTripThroughTheUseCaseTypes(t *testing.T) {
	at := timestamppb.New(time.Unix(1_700_000_000, 0))
	version, mission, level := "1.2.3", "m", "observe"
	patch := &operatorv1.BotPatch{
		Version: &version, Mission: &mission, Level: &level,
		AllowedTools: &operatorv1.StringList{Values: []string{"a", "b"}},
		ToolRules: &operatorv1.ToolRules{
			Allow: []*operatorv1.ToolRule{{Tool: "t", Arguments: map[string]string{"k": "v"}}}, Deny: []*operatorv1.ToolRule{{Tool: "u"}},
		},
		Triggers: &operatorv1.TriggerList{Values: []*operatorv1.Trigger{
			{Type: "timer", Name: "tick", Objective: "o", Every: durationpb.New(15 * time.Minute), Interrupt: true},
			{Type: "webhook", Name: "w", Objective: "o", Secret: "s", SigningKey: "k", Events: []string{"e"}},
		}},
		Placement: &operatorv1.Placement{Nodes: []string{"n"}, Selector: map[string]string{"z": "a"}},
		Approvals: &operatorv1.Approvals{
			Ttl: durationpb.New(2 * time.Hour), OnExpire: "deny", Approvers: []string{"alice"},
			Notify:      []*operatorv1.NotifyTarget{{Type: "webhook", Name: "n", Url: "https://example.test", Secret: "s", Command: []string{"c"}}},
			AutoApprove: []*operatorv1.ToolRule{{Tool: "x"}},
		},
		AutoApprove: &operatorv1.ToolRuleList{Values: []*operatorv1.ToolRule{{Tool: "y", Arguments: map[string]string{"a": "b"}}}},
		Delegation:  &operatorv1.Delegation{To: []string{"d"}, MaxDepth: 2, BudgetShare: 0.5, Split: "equal", MaxChildren: 3, FromTeam: yes()},
		Groups:      &operatorv1.StringList{Values: []string{"g"}},
		Events:      &operatorv1.Events{Emit: []*operatorv1.EmitRule{{Name: "warn.x", MaxSeverity: "warning"}}, PerMinute: 4, IntakePerMinute: 5, MaxDepth: 6, Interruptible: yes()},
		Subscriptions: &operatorv1.SubscriptionList{Values: []*operatorv1.Subscription{
			{Topic: "t", Mode: "task", From: []string{"f"}, Urgent: true, Objective: "o", Status: "approved", AskedBy: "b", Why: "w", AskedAt: at},
		}},
		Runtime: &operatorv1.Runtime{Kind: "jev", Jev: &operatorv1.JevProgram{
			Server: "s", Start: "st", Wait: "w", Stop: "sp", Model: "m",
			Act:  &operatorv1.JevAct{Tool: "t", Choice: "c", Verdict: "v", Pass: []string{"p"}},
			Tell: &operatorv1.JevTell{Name: "n", Fields: []string{"f"}},
		}},
		Skills:  &operatorv1.SkillPinList{Values: []*operatorv1.SkillPin{{Name: "s", Version: "1.0.0", Catalog: "c"}}},
		Model:   &operatorv1.ModelPolicy{Model: "m", RoutingPolicy: "r", AllowRemoteProviders: true, MaxContextTokens: 9},
		Budgets: &operatorv1.Budgets{MaxModelCallsPerTask: 1, MaxToolCallsPerTask: 2, MaxDelegationDepth: 3, TaskTimeout: durationpb.New(90 * time.Second)},
	}
	back := patchToProto(patchFromProto(patch))
	if !proto.Equal(patch, back) {
		t.Errorf("the patch changed on the way:\n%v\n%v", patch, back)
	}
	if empty := patchFromProto(nil); empty.Mission != nil || empty.Groups != nil || empty.AllowedTools != nil {
		t.Errorf("a nil patch is not empty: %+v", empty)
	}
	cleared := patchFromProto(&operatorv1.BotPatch{AllowedTools: &operatorv1.StringList{}, Triggers: &operatorv1.TriggerList{}})
	if cleared.AllowedTools == nil || *cleared.AllowedTools == nil || cleared.Triggers == nil {
		t.Errorf("empty lists must clear, not be absent: %+v", cleared)
	}

	definition := definitionFromProto(&operatorv1.BotDefinition{
		Id: "b", Version: "1.0.0", Tenant: "t", Name: "n", Owner: "o", Mission: "m", Level: "observe",
		Model: patch.Model, Budgets: patch.Budgets, Triggers: patch.GetTriggers().GetValues(), Groups: []string{"g"},
	})
	if definition.ID != "b" || definition.Tenant != "t" || definition.Budgets.TaskTimeoutSeconds != 90 || definition.Model.MaxContextTokens != 9 || len(definition.Triggers) != 2 || definition.Triggers[0].Every != "15m0s" {
		t.Errorf("definition: %+v", definition)
	}
}

func TestSnapshotsGoToTheWireWithoutLosingWhatTheyHold(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	closed := now.Add(time.Minute)
	task := taskToProto(domain.TaskSnapshot{
		ID: "t", BotID: "b", Status: "WaitingApproval", Input: []byte(`{"a":1}`), ExecutionIDs: []string{"e"}, Children: []string{"c"},
		Approval:   &domain.ApprovalWait{CallID: "c1", Tool: "x", Since: now, Deadline: closed, OnExpire: domain.ExpireDeny, Outcome: "approved", ClosedAt: &closed},
		Delegation: &domain.TaskLineage{ParentTaskID: "p", Depth: 1, MaxDepth: 2, Chain: []string{"a"}, Grant: domain.DelegationGrant{MaxModelCalls: 3, Deadline: closed}},
		Skills:     []domain.SkillPinSnapshot{{Name: "s"}}, CreatedAt: now, UpdatedAt: now,
	})
	if string(task.GetInput()) != `{"a":1}` || task.GetApproval().GetOnExpire() != "deny" || task.GetApproval().GetClosedAt() == nil ||
		task.GetDelegation().GetGrant().GetMaxModelCalls() != 3 || task.GetDelegation().GetChain()[0] != "a" || len(task.GetSkills()) != 1 {
		t.Errorf("task: %v", task)
	}
	execution := executionToProto(domain.ExecutionSnapshot{
		ID: "e", EndedAt: &closed, LeaseUntil: &closed,
		Result: domain.Result{Answer: "a", ToolCalls: []domain.ToolCall{{Name: "n", CallID: "c", Preview: true}},
			PendingDecision: &domain.PendingDecision{Tool: "t", Arguments: []byte(`{"x":1}`)}, NotStarted: true},
		Usage:  domain.Usage{CostUnknown: true, ToolCalls: 2},
		Policy: &domain.PolicyRecord{Level: "observe", Enforced: true}, Correlation: &domain.CorrelationRecord{TraceID: "tr", Carried: true},
	})
	if !execution.GetResult().GetNotStarted() || !execution.GetResult().GetToolCalls()[0].GetPreview() || string(execution.GetResult().GetPendingDecision().GetArguments()) != `{"x":1}` ||
		!execution.GetUsage().GetCostUnknown() || !execution.GetPolicy().GetEnforced() || execution.GetCorrelation().GetTraceId() != "tr" {
		t.Errorf("execution: %v", execution)
	}
	if executionOptionalToProto(nil) != nil || timestamp(time.Time{}) != nil || timestampOf(nil) != nil || instant(nil) != (time.Time{}) {
		t.Error("absent is not nil")
	}
	if event := eventToProto(domain.Event{ID: "ev", Payload: map[string]any{"list": []string{"a"}, "n": 3, "nested": map[string]any{"k": true}}}); event.GetPayload().GetFields()["list"].GetListValue() == nil || event.GetPayload().GetFields()["n"].GetNumberValue() != 3 {
		t.Errorf("payload: %v", event.GetPayload())
	}
	if structOf(map[string]any{"bad": make(chan int)}) != nil || structOf(nil) != nil {
		t.Error("a payload that is not JSON should be absent")
	}
	bot := botToProto(domain.BotSnapshot{ID: "b", Staged: &domain.StagedDefinitionSnapshot{Version: "2.0.0", StagedAt: now}, Runtime: &domain.RuntimeSnapshot{Kind: "axlr"},
		Delegation: &domain.DelegationSnapshot{FromTeam: yes()}, Events: &domain.EventsSnapshot{Interruptible: yes()}, Budgets: domain.BudgetsSnapshot{TaskTimeoutSeconds: 60}})
	if bot.GetStaged().GetVersion() != "2.0.0" || bot.GetRuntime().GetJev() != nil || !bot.GetDelegation().GetFromTeam() || !bot.GetEvents().GetInterruptible() || bot.GetBudgets().GetTaskTimeout().AsDuration() != time.Minute {
		t.Errorf("bot: %v", bot)
	}
	if textOf(nil) != "" || durationText("") != nil || durationText("nope") != nil || seconds(0) != nil {
		t.Error("absent durations")
	}
}
