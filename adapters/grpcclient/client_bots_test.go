package grpcclient

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func TestBotSnapshotMapsEveryField(t *testing.T) {
	h := newHarness(t)
	h.fake.answer(method("BotsService", "GetBot"), &operatorv1.GetBotResponse{Bot: fullBot()})
	got, err := h.client.GetBot(t.Context(), "bot-a")
	if err != nil {
		t.Fatal(err)
	}
	staged := json.RawMessage(`{"mission":"x"}`)
	fromTeam, interruptible := false, true
	want := domain.BotSnapshot{
		ID: "bot-a", TenantID: "default", Name: "Bot A", Owner: "ops", Mission: "Keep watch", AllowedTools: []string{"local_exec"},
		Model:   domain.ModelPolicySnapshot{Model: "m", RoutingPolicy: "local-only", AllowRemoteProviders: true, MaxContextTokens: 8000},
		Budgets: domain.BudgetsSnapshot{MaxModelCallsPerTask: 10, MaxToolCallsPerTask: 20, MaxDelegationDepth: 2, TaskTimeoutSeconds: 300},
		Level:   "act-with-approval",
		ToolRules: domain.ToolRulesSnapshot{
			Allow: []domain.ToolRuleSnapshot{{Tool: "local_exec", Arguments: map[string]string{"program": "ls"}}},
			Deny:  []domain.ToolRuleSnapshot{{Tool: "rm"}},
		},
		Triggers: []domain.TriggerSnapshot{
			{Type: "manual"},
			{Type: "timer", Name: "clock", Objective: "Look", Every: "15m"},
			{Type: "timer", Name: "hourly", Every: "1h", Cron: "0 * * * *"},
			{Type: "webhook", Name: "hook", Secret: "<redacted>", SigningKey: "<redacted>", Events: []string{"a", "b"}, Interrupt: true},
		},
		Placement: domain.PlacementSnapshot{Nodes: []string{"n1"}, Selector: map[string]string{"zone": "a"}},
		Approvals: domain.ApprovalsSnapshot{
			TTL: "1h", OnExpire: "deny", Approvers: []string{"ana"}, AutoApprove: []domain.ToolRuleSnapshot{{Tool: "local_exec"}},
			Notify: []domain.NotifyTargetSnapshot{{Type: "webhook", Name: "n", URL: "https://x", Secret: "<redacted>", Command: []string{"notify"}}},
		},
		Delegation:    &domain.DelegationSnapshot{To: []string{"bot-b"}, MaxDepth: 2, BudgetShare: 0.5, Split: "even", MaxChildren: 3, FromTeam: &fromTeam},
		Groups:        []string{"team"},
		Events:        &domain.EventsSnapshot{Emit: []domain.EmitRuleSnapshot{{Name: "info.*", MaxSeverity: "info"}}, PerMinute: 6, IntakePerMinute: 10, MaxDepth: 2, Interruptible: &interruptible},
		Subscriptions: []domain.SubscriptionSnapshot{{Topic: "t", Mode: "task", From: []string{"bot-b"}, Urgent: true, Objective: "o", Status: "active", AskedBy: "bot-a", Why: "w", AskedAt: &t0}},
		Runtime: &domain.RuntimeSnapshot{Kind: "jev", Jev: &domain.JevProgramSnapshot{
			Server: "s", Start: "st", Wait: "w", Stop: "sp", Model: "jm",
			Act:  domain.JevActSnapshot{Tool: "tool", Choice: "c", Verdict: "v", Pass: []string{"p"}},
			Tell: &domain.JevTellSnapshot{Name: "tell", Fields: []string{"f"}},
		}},
		Skills:  []domain.SkillPinSnapshot{{Name: "sk", Version: "1.0.0", Catalog: "cat"}},
		Version: "1.2.0", Staged: &domain.StagedDefinitionSnapshot{Version: "1.3.0", StagedAt: t1, Change: staged},
		Status: "Enabled", CreatedAt: t0, UpdatedAt: t1,
	}
	if !reflect.DeepEqual(got, want) {
		gotJSON, _ := json.MarshalIndent(got, "", " ")
		wantJSON, _ := json.MarshalIndent(want, "", " ")
		t.Fatalf("snapshot differs:\n got %s\nwant %s", gotJSON, wantJSON)
	}
}

func TestAnEmptyBotKeepsAbsentThingsAbsent(t *testing.T) {
	h := newHarness(t)
	h.fake.answer(method("BotsService", "ListBots"), &operatorv1.ListBotsResponse{Bots: []*operatorv1.Bot{{Id: "bare", Status: "Draft"}}})
	bots, err := h.client.ListBots(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(bots[0])
	for _, absent := range []string{`"delegation"`, `"events"`, `"runtime"`, `"staged"`, `"skills"`, `"subscriptions"`} {
		if strings.Contains(string(raw), absent) {
			t.Errorf("%s should be absent: %s", absent, raw)
		}
	}
	if !strings.Contains(string(raw), `"allowedTools":[]`) || !strings.Contains(string(raw), `"triggers":[]`) {
		t.Errorf("empty lists are [], not null: %s", raw)
	}
	h.fake.answer(method("BotsService", "ListBots"), &operatorv1.ListBotsResponse{})
	if bots, _ := h.client.ListBots(t.Context()); bots == nil || len(bots) != 0 {
		t.Errorf("no bots is [], got %#v", bots)
	}
}

func TestBotLifecycleCallsSendTheirIDAndAnswerTheBot(t *testing.T) {
	h := newHarness(t)
	bot := &operatorv1.Bot{Id: "bot-a", Status: "Enabled"}
	for _, name := range []string{"EnableBot", "PauseBot", "ApplyStagedBot", "UnstageBot", "StageBot", "UpdateBot", "RegisterBot"} {
		h.fake.answer(method("BotsService", name), answerWithBot(name, bot))
	}
	ctx := t.Context()
	calls := map[string]func() (domain.BotSnapshot, error){
		"EnableBot":      func() (domain.BotSnapshot, error) { return h.client.EnableBot(ctx, "bot-a") },
		"PauseBot":       func() (domain.BotSnapshot, error) { return h.client.PauseBot(ctx, "bot-a") },
		"ApplyStagedBot": func() (domain.BotSnapshot, error) { return h.client.ApplyStagedBot(ctx, "bot-a") },
		"UnstageBot":     func() (domain.BotSnapshot, error) { return h.client.UnstageBot(ctx, "bot-a") },
		"StageBot":       func() (domain.BotSnapshot, error) { return h.client.StageBot(ctx, "bot-a", application.BotPatch{}) },
		"UpdateBot":      func() (domain.BotSnapshot, error) { return h.client.UpdateBot(ctx, "bot-a", application.BotPatch{}) },
		"RegisterBot": func() (domain.BotSnapshot, error) {
			return h.client.RegisterBot(ctx, application.BotDefinition{Name: "x"})
		},
	}
	for name, call := range calls {
		got, err := call()
		if err != nil || got.ID != "bot-a" || got.Status != "Enabled" {
			t.Errorf("%s: %v %v", name, got, err)
		}
	}
	if request := h.fake.request(method("BotsService", "EnableBot")).(*operatorv1.EnableBotRequest); request.GetBotId() != "bot-a" {
		t.Errorf("enable request: %v", request)
	}
	if request := h.fake.request(method("BotsService", "RegisterBot")).(*operatorv1.RegisterBotRequest); request.GetDefinition().GetName() != "x" {
		t.Errorf("register request: %v", request)
	}
}

func answerWithBot(name string, bot *operatorv1.Bot) proto.Message {
	switch name {
	case "EnableBot":
		return &operatorv1.EnableBotResponse{Bot: bot}
	case "PauseBot":
		return &operatorv1.PauseBotResponse{Bot: bot}
	case "ApplyStagedBot":
		return &operatorv1.ApplyStagedBotResponse{Bot: bot}
	case "UnstageBot":
		return &operatorv1.UnstageBotResponse{Bot: bot}
	case "StageBot":
		return &operatorv1.StageBotResponse{Bot: bot}
	case "UpdateBot":
		return &operatorv1.UpdateBotResponse{Bot: bot}
	}
	return &operatorv1.RegisterBotResponse{Bot: bot}
}

func TestStopAndDeleteAnswerWhatTheyDid(t *testing.T) {
	h := newHarness(t)
	bot := &operatorv1.Bot{Id: "bot-a", Status: "Paused"}
	h.fake.answer(method("BotsService", "StopBot"), &operatorv1.StopBotResponse{Bot: bot, Cancelled: []string{"t-1"}, NotCancelled: []*operatorv1.StopRefusal{{TaskId: "t-2", Reason: "running"}}})
	h.fake.answer(method("BotsService", "DeleteBot"), &operatorv1.DeleteBotResponse{
		Bot: bot, Cancelled: []string{"t-1"}, ChildrenCancelled: []string{"t-3"}, TimersRemoved: 2, NotificationsDropped: 1,
		NamedBy: []*operatorv1.BotReference{{BotId: "bot-b", How: "delegation", Topic: "x"}},
	})
	stop, err := h.client.StopBot(t.Context(), "bot-a")
	if err != nil || stop.Bot.Status != "Paused" || len(stop.Cancelled) != 1 || stop.NotCancelled[0] != (application.StopRefusal{TaskID: "t-2", Reason: "running"}) {
		t.Fatalf("stop: %+v %v", stop, err)
	}
	deleted, err := h.client.DeleteBot(t.Context(), "bot-a")
	if err != nil || deleted.TimersRemoved != 2 || deleted.NotificationsDropped != 1 || deleted.ChildrenCancelled[0] != "t-3" || deleted.NamedBy[0] != (application.BotReference{BotID: "bot-b", How: "delegation", Topic: "x"}) {
		t.Fatalf("delete: %+v %v", deleted, err)
	}
	h.fake.answer(method("BotsService", "StopBot"), &operatorv1.StopBotResponse{Bot: bot})
	if stop, _ := h.client.StopBot(t.Context(), "bot-a"); stop.Cancelled == nil || stop.NotCancelled == nil {
		t.Errorf("nothing cancelled is [], not null: %+v", stop)
	}
}

func TestStagedChangeKeepsWhichFieldsItSets(t *testing.T) {
	h := newHarness(t)
	h.fake.answer(method("BotsService", "GetStagedBot"), &operatorv1.GetStagedBotResponse{
		Version: "2.0.0", StagedAt: ts(t1),
		Patch: &operatorv1.BotPatch{
			Version: strPtr("2.0.0"), Mission: strPtr("m"), Level: strPtr("observe"),
			AllowedTools: &operatorv1.StringList{}, ToolRules: &operatorv1.ToolRules{Allow: []*operatorv1.ToolRule{{Tool: "t"}}},
			Triggers:  &operatorv1.TriggerList{Values: []*operatorv1.Trigger{{Type: "timer", Every: durationpbMinutes(30)}}},
			Placement: &operatorv1.Placement{Nodes: []string{"n"}}, Approvals: &operatorv1.Approvals{OnExpire: "cancel"},
			AutoApprove: &operatorv1.ToolRuleList{}, Delegation: &operatorv1.Delegation{To: []string{"b"}},
			Groups: &operatorv1.StringList{Values: []string{"g"}}, Events: &operatorv1.Events{PerMinute: 3},
			Subscriptions: &operatorv1.SubscriptionList{}, Runtime: &operatorv1.Runtime{Kind: "axlr"},
			Skills: &operatorv1.SkillPinList{}, Model: &operatorv1.ModelPolicy{Model: "m"}, Budgets: &operatorv1.Budgets{MaxModelCallsPerTask: 4},
		},
	})
	view, err := h.client.GetStagedBot(t.Context(), "bot-a")
	if err != nil || view.Version != "2.0.0" || !view.StagedAt.Equal(t1) {
		t.Fatalf("view: %+v %v", view, err)
	}
	p := view.Patch
	if *p.Version != "2.0.0" || *p.Mission != "m" || *p.Level != "observe" {
		t.Errorf("scalars: %+v", p)
	}
	if p.AllowedTools == nil || len(*p.AllowedTools) != 0 || p.AutoApprove == nil || len(*p.AutoApprove) != 0 || p.Subscriptions == nil || len(*p.Subscriptions) != 0 || p.Skills == nil || len(*p.Skills) != 0 {
		t.Errorf("an empty list stays empty, not absent: %+v", p)
	}
	if (*p.Triggers)[0].Every != "30m" || p.Approvals.OnExpire != "cancel" || p.Delegation.To[0] != "b" || (*p.Groups)[0] != "g" || p.Events.PerMinute != 3 || p.Runtime.Kind != "axlr" {
		t.Errorf("composites: %+v", p)
	}
	if p.Model.Model != "m" || p.Budgets.MaxModelCallsPerTask != 4 || p.ToolRules.Allow[0].Tool != "t" || p.Placement.Nodes[0] != "n" {
		t.Errorf("model and budgets: %+v", p)
	}
	h.fake.answer(method("BotsService", "GetStagedBot"), &operatorv1.GetStagedBotResponse{Patch: &operatorv1.BotPatch{}})
	view, _ = h.client.GetStagedBot(t.Context(), "bot-a")
	if raw, _ := json.Marshal(view.Patch); string(raw) != "{}" {
		t.Errorf("an empty patch sets nothing: %s", raw)
	}
}

func durationpbMinutes(n int) *durationpbDuration {
	return durationpbNew(time.Duration(n) * time.Minute)
}

func TestPatchPresenceReachesTheWire(t *testing.T) {
	h := newHarness(t)
	h.fake.answer(method("BotsService", "UpdateBot"), &operatorv1.UpdateBotResponse{Bot: &operatorv1.Bot{Id: "bot-a"}})
	mission, none, noRules := "new", []string{}, application.AutoApproveRules{}
	fromTeam := true
	triggers := []domain.TriggerSnapshot{{Type: "timer", Every: "15m"}}
	subs, skills := []domain.SubscriptionSnapshot{}, []domain.SkillPinSnapshot{{Name: "s"}}
	groups := []string{"g"}
	patch := application.BotPatch{
		Mission: &mission, AllowedTools: &none, AutoApprove: &noRules, Triggers: &triggers, Groups: &groups, Subscriptions: &subs, Skills: &skills,
		Delegation: &domain.DelegationSnapshot{FromTeam: &fromTeam},
		Approvals:  &domain.ApprovalsSnapshot{TTL: "1h", Notify: []domain.NotifyTargetSnapshot{{Type: "webhook", URL: "u"}}},
		Model:      &application.ModelPolicyDefinition{Model: "m"}, Budgets: &application.BudgetsDefinition{TaskTimeoutSeconds: 60},
		ToolRules: &domain.ToolRulesSnapshot{Deny: []domain.ToolRuleSnapshot{{Tool: "x"}}}, Placement: &domain.PlacementSnapshot{Nodes: []string{"n"}},
		Events: &domain.EventsSnapshot{PerMinute: 2}, Runtime: &domain.RuntimeSnapshot{Kind: "jev", Jev: &domain.JevProgramSnapshot{Server: "s", Tell: &domain.JevTellSnapshot{Name: "t"}}},
	}
	if _, err := h.client.UpdateBot(t.Context(), "bot-a", patch); err != nil {
		t.Fatal(err)
	}
	sent := h.fake.request(method("BotsService", "UpdateBot")).(*operatorv1.UpdateBotRequest).GetPatch()
	if sent.Level != nil || sent.Version != nil || *sent.Mission != "new" {
		t.Errorf("unset scalars stay unset: %v", sent)
	}
	if sent.AllowedTools == nil || len(sent.AllowedTools.Values) != 0 || sent.AutoApprove == nil || sent.Subscriptions == nil || sent.Skills.Values[0].Name != "s" {
		t.Errorf("an empty list is the wrapper with no values: %v", sent)
	}
	if sent.Triggers.Values[0].Every.AsDuration() != 15*time.Minute || sent.Approvals.Ttl.AsDuration() != time.Hour || sent.Approvals.Notify[0].Url != "u" {
		t.Errorf("durations: %v", sent)
	}
	if sent.Delegation.FromTeam == nil || !*sent.Delegation.FromTeam || sent.Budgets.TaskTimeout.AsDuration() != time.Minute || sent.Runtime.Jev.Tell.Name != "t" {
		t.Errorf("optional and nested: %v", sent)
	}
	if sent.Groups.Values[0] != "g" || sent.Model.Model != "m" || sent.ToolRules.Deny[0].Tool != "x" || sent.Placement.Nodes[0] != "n" || sent.Events.PerMinute != 2 {
		t.Errorf("rest: %v", sent)
	}
	empty := application.BotPatch{}
	if _, err := h.client.UpdateBot(t.Context(), "bot-a", empty); err != nil {
		t.Fatal(err)
	}
	if sent := h.fake.request(method("BotsService", "UpdateBot")).(*operatorv1.UpdateBotRequest).GetPatch(); !proto.Equal(sent, &operatorv1.BotPatch{}) {
		t.Errorf("an empty patch sends nothing: %v", sent)
	}
}

func TestDefinitionRoundsTheWire(t *testing.T) {
	h := newHarness(t)
	h.fake.answer(method("BotsService", "RegisterBot"), &operatorv1.RegisterBotResponse{Bot: &operatorv1.Bot{Id: "bot-a"}})
	definition := application.BotDefinition{
		ID: "bot-a", Version: "1.0.0", Tenant: "default", Name: "A", Owner: "o", Mission: "m", AllowedTools: []string{"t"}, Level: "observe",
		Triggers: []domain.TriggerSnapshot{{Type: "timer", Every: "10m"}}, Approvals: domain.ApprovalsSnapshot{TTL: "30m"},
		Groups: []string{"g"}, Subscriptions: []domain.SubscriptionSnapshot{{Topic: "t", AskedAt: &t0}}, Skills: []domain.SkillPinSnapshot{{Name: "s"}},
		Model: application.ModelPolicyDefinition{Model: "m", MaxContextTokens: 5}, Budgets: application.BudgetsDefinition{MaxToolCallsPerTask: 7},
		Delegation: &domain.DelegationSnapshot{To: []string{"b"}}, Events: &domain.EventsSnapshot{Emit: []domain.EmitRuleSnapshot{{Name: "x"}}},
	}
	if _, err := h.client.RegisterBot(t.Context(), definition); err != nil {
		t.Fatal(err)
	}
	sent := h.fake.request(method("BotsService", "RegisterBot")).(*operatorv1.RegisterBotRequest).GetDefinition()
	if sent.GetId() != "bot-a" || sent.GetTenant() != "default" || sent.GetTriggers()[0].GetEvery().AsDuration() != 10*time.Minute || sent.GetApprovals().GetTtl().AsDuration() != 30*time.Minute {
		t.Errorf("definition: %v", sent)
	}
	if sent.GetSubscriptions()[0].GetAskedAt().AsTime() != t0 || sent.GetModel().GetMaxContextTokens() != 5 || sent.GetBudgets().GetMaxToolCallsPerTask() != 7 || sent.GetDelegation().GetTo()[0] != "b" {
		t.Errorf("definition: %v", sent)
	}
}

func TestADurationThatIsNoDurationIsRefusedBeforeTheCall(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	bad := []domain.TriggerSnapshot{{Type: "timer", Every: "soon"}}
	if _, err := h.client.RegisterBot(ctx, application.BotDefinition{Triggers: bad}); !isValidation(err, "triggers.every") {
		t.Errorf("trigger: %v", err)
	}
	if _, err := h.client.RegisterBot(ctx, application.BotDefinition{Approvals: domain.ApprovalsSnapshot{TTL: "later"}}); !isValidation(err, "approvals.ttl") {
		t.Errorf("ttl: %v", err)
	}
	if _, err := h.client.UpdateBot(ctx, "b", application.BotPatch{Triggers: &bad}); !isValidation(err, "triggers.every") {
		t.Errorf("patch trigger: %v", err)
	}
	if _, err := h.client.StageBot(ctx, "b", application.BotPatch{Approvals: &domain.ApprovalsSnapshot{TTL: "x"}}); !isValidation(err, "approvals.ttl") {
		t.Errorf("patch ttl: %v", err)
	}
	if _, err := h.client.CreateEnrollmentToken(ctx, enrollRequest("yesterday")); !isValidation(err, "ttl") {
		t.Errorf("enroll ttl: %v", err)
	}
	if h.fake.request(method("BotsService", "RegisterBot")) != nil {
		t.Error("nothing should have been sent")
	}
}

func isValidation(err error, field string) bool {
	var v domain.ValidationError
	return err != nil && asValidation(err, &v) && v.Field == field
}

func TestBotEventsAskForTheNewest(t *testing.T) {
	h := newHarness(t)
	h.fake.answer(method("BotsService", "ListBotEvents"), &operatorv1.ListBotEventsResponse{Events: []*operatorv1.Event{fullEvent("ev-1", "bot.enabled")}})
	events, err := h.client.ListEvents(t.Context(), "bot-a", 5)
	if err != nil || len(events) != 1 {
		t.Fatal(events, err)
	}
	request := h.fake.request(method("BotsService", "ListBotEvents")).(*operatorv1.ListBotEventsRequest)
	if request.GetBotId() != "bot-a" || request.GetLimit() != 5 {
		t.Errorf("request: %v", request)
	}
	event := events[0]
	if event.ID != "ev-1" || event.Type != "bot.enabled" || event.BotID != "bot-a" || event.GroupName != "team" || event.NodeID != "n1" || event.TaskID != "t-1" ||
		event.ExecutionID != "e-1" || event.CorrelationID != "corr" || event.TenantID != "default" || event.SchemaVersion != "1" || !event.CreatedAt.Equal(t0) {
		t.Errorf("event: %+v", event)
	}
	if items, _ := event.Payload["items"].([]any); len(items) != 1 {
		t.Errorf("payload: %v", event.Payload)
	}
	h.fake.answer(method("BotsService", "ListBotEvents"), &operatorv1.ListBotEventsResponse{Events: []*operatorv1.Event{{EventId: "x"}}})
	if events, _ := h.client.ListEvents(t.Context(), "bot-a", 0); events[0].Payload != nil {
		t.Errorf("no payload stays nil: %v", events[0].Payload)
	}
}
