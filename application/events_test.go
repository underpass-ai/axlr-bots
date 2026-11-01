package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

func emit(t *testing.T, h *harness, node domain.NodeID, execution domain.ExecutionID, command EmitEventCommand) EmitAnswer {
	t.Helper()
	answer, err := NewEmitEvent(h.deps).Execute(context.Background(), node, execution, command)
	if err != nil {
		t.Fatal(err)
	}
	return answer
}

// opsTeam is the guide case's team: a watcher and two workers in ops, a
// paused worker, and a stranger in another team.
func opsTeam(t *testing.T, h *harness) Assignment {
	t.Helper()
	onlineNode(t, h, "web-1")
	watcher := teamBot(t, h, "watcher", []string{"ops"}, nil)
	teamBot(t, h, "worker-1", []string{"ops"}, nil)
	teamBot(t, h, "worker-2", []string{"ops"}, nil)
	paused := teamBot(t, h, "worker-3", []string{"ops"}, nil)
	if _, err := NewPauseBot(h.deps).Execute(context.Background(), paused.ID()); err != nil {
		t.Fatal(err)
	}
	teamBot(t, h, "stranger", []string{"other"}, nil)
	return runOnNode(t, h, watcher, "web-1", "ask-1")
}

func TestTheAssignmentOffersTheEmitTool(t *testing.T) {
	h := newHarness()
	run := opsTeam(t, h).Run
	if run.Events == nil || !contains(run.AllowedTools, EmitEventToolName) || strings.Join(run.Events.Teammates, ",") != "worker-1,worker-2" || run.Events.Names[0] != "**" {
		t.Fatalf("events offer = %+v tools %v", run.Events, run.AllowedTools)
	}
	if got := run.NodeTools(); len(got) != 3 || got[1] != EmitEventToolName || got[2] != SubscribeToolName || !contains(run.AllowedTools, SubscribeToolName) {
		t.Errorf("node tools = %v", got)
	}
}

func TestAWarningAddressedToATeammateBecomesItsTask(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	parent := opsTeam(t, h)
	h.clock.now = t0.Add(10 * time.Second)
	answer := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "warn.deploy-slow", Message: "Deploy 244 is starting\non your service", To: []string{"worker-1"}, Data: json.RawMessage(`{"build": 244}`)})
	if answer.Refused != nil || len(answer.Deliveries) != 1 || answer.Deliveries[0].BotID != "worker-1" || !answer.Deliveries[0].Created || answer.Deliveries[0].Trigger != "team" || len(answer.Skipped) != 0 {
		t.Fatalf("answer = %+v", answer)
	}
	task, err := h.tasks.Get(ctx, domain.TaskID(answer.Deliveries[0].TaskID))
	if err != nil {
		t.Fatal(err)
	}
	objective := task.Objective().String()
	for _, want := range []string{"Your teammate watcher says (warn.deploy-slow): Deploy 244 is starting on your service", `The facts it gave: {"build":244}`} {
		if !strings.Contains(objective, want) {
			t.Errorf("objective lacks %q: %s", want, objective)
		}
	}
	lineage, ok := task.Lineage()
	parentTask, _ := h.tasks.Get(ctx, parent.Run.TaskID)
	if !ok || lineage.EventID != answer.EventID || lineage.Depth != 1 || lineage.Chain[0] != "watcher" || !lineage.Grant.Deadline.Equal(h.clock.now.Add(300*time.Second)) {
		t.Errorf("lineage = %+v", lineage)
	}
	if task.CorrelationID() != parentTask.CorrelationID() || task.Trigger().Type != domain.TriggerEvent {
		t.Errorf("task = %+v", task.Snapshot())
	}
	event, ok := lastEvent(h, domain.EventEventEmitted)
	if !ok || event.BotID != "watcher" || event.Payload["eventId"] != answer.EventID || event.Payload["severity"] != "warning" || len(event.Payload["deliveries"].([]map[string]any)) != 1 {
		t.Errorf("emitted = %+v", event)
	}
	if audit := h.log.audits[len(h.log.audits)-1]; audit.Action != "event.emit" || audit.Outcome != domain.AuditOK {
		t.Errorf("audit = %+v", audit)
	}
	// The node retries the same call: the same event, nothing new, even
	// when an older node sends a severity, which is ignored.
	again := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "warn.deploy-slow", Message: "Deploy 244 is starting on your service", Severity: "critical", To: []string{"worker-1"}, Data: json.RawMessage(`{ "build" : 244 }`)})
	if !again.Deduplicated || again.EventID != answer.EventID || again.Deliveries[0].Created || again.Deliveries[0].TaskID != answer.Deliveries[0].TaskID {
		t.Errorf("again = %+v", again)
	}
	emitted := 0
	for _, e := range h.log.events {
		if e.Type == domain.EventEventEmitted {
			emitted++
		}
	}
	if emitted != 1 {
		t.Errorf("emitted %d times", emitted)
	}
}

func TestBroadcastsReachWhoHearsThem(t *testing.T) {
	h := newHarness()
	parent := opsTeam(t, h)
	info := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "deploy.started", Message: "a deploy is starting"})
	if info.Refused != nil || len(info.Deliveries) != 0 || len(info.Skipped) != 0 {
		t.Errorf("info broadcast = %+v", info)
	}
	warning := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "warn.deploy-started", Message: "a deploy is starting"})
	if len(warning.Deliveries) != 0 || len(warning.Skipped) != 0 {
		t.Errorf("warning broadcast = %+v", warning)
	}
	danger := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "danger.deploy", Message: "a deploy is starting"})
	if len(danger.Deliveries) != 2 || !danger.Deliveries[0].Urgent || len(danger.Skipped) != 1 || danger.Skipped[0].BotID != "worker-3" || danger.Skipped[0].Reason != domain.RefusedTargetUnavailable {
		t.Errorf("danger broadcast = %+v", danger)
	}
}

func TestEventTriggersChooseWhatABotHears(t *testing.T) {
	h := newHarness()
	onlineNode(t, h, "web-1")
	watcher := teamBot(t, h, "watcher", []string{"ops"}, nil)
	teamBot(t, h, "reviewer", []string{"ops"}, func(d *BotDefinition) {
		d.Triggers = []domain.TriggerSnapshot{{Type: "manual"}, {Type: "event", Name: "deploys", Events: []string{"deploy.*"}, Objective: "Look at {{.data.where}}: {{.message}}"}}
	})
	teamBot(t, h, "broken", []string{"ops"}, func(d *BotDefinition) {
		d.Triggers = []domain.TriggerSnapshot{{Type: "event", Name: "deploys", Events: []string{"deploy.*"}, Objective: "Look at {{.data.where}}"}}
	})
	parent := runOnNode(t, h, watcher, "web-1", "ask-1")
	answer := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "deploy.started", Message: "a deploy is starting", Data: json.RawMessage(`{"where": "service-1"}`)})
	if len(answer.Deliveries) != 2 || answer.Deliveries[1].Trigger != "deploy.*" || answer.Deliveries[1].Mode != "task" {
		t.Fatalf("answer = %+v", answer)
	}
	task, _ := h.tasks.Get(context.Background(), domain.TaskID(answer.Deliveries[1].TaskID))
	if task.Objective().String() != "Look at service-1: a deploy is starting" {
		t.Errorf("objective = %q", task.Objective())
	}
	// An addressed event is always heard: by the implicit subscription.
	addressed := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "stop.halt", Message: "stop now", To: []string{"reviewer"}})
	if len(addressed.Deliveries) != 1 || addressed.Deliveries[0].Trigger != "team" || !addressed.Deliveries[0].Urgent {
		t.Errorf("addressed = %+v", addressed)
	}
	failing := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "deploy.failed", Message: "a deploy is starting", To: []string{"broken"}})
	if len(failing.Skipped) != 1 || failing.Skipped[0].Reason != "objective" {
		t.Errorf("failing = %+v", failing)
	}
}

func TestEventRefusals(t *testing.T) {
	h := newHarness()
	onlineNode(t, h, "web-1")
	alone := teamBot(t, h, "alone", nil, nil)
	strict := teamBot(t, h, "strict", []string{"ops"}, func(d *BotDefinition) {
		d.Events = &domain.EventsSnapshot{Emit: []domain.EmitRuleSnapshot{{Name: "halt", MaxSeverity: "info"}, {Name: "**.bot"}}, PerMinute: 1}
	})
	teamBot(t, h, "worker-1", []string{"ops"}, nil)
	teamBot(t, h, "stranger", []string{"other"}, nil)
	lonely := runOnNode(t, h, alone, "web-1", "ask-1")
	if answer := emit(t, h, "web-1", lonely.ExecutionID, EmitEventCommand{Name: "halt", Message: "x"}); answer.Refused == nil || answer.Refused.Reason != domain.RefusedNoTeam {
		t.Errorf("no team = %+v", answer)
	}
	if _, err := NewReportExecution(h.deps).Execute(context.Background(), "web-1", lonely.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "ok"}}); err != nil {
		t.Fatal(err)
	}
	run := runOnNode(t, h, strict, "web-1", "ask-2")
	cases := []struct {
		command EmitEventCommand
		reason  string
	}{
		{EmitEventCommand{Name: "weather", Message: "rain"}, domain.RefusedNotAllowed},
		{EmitEventCommand{Name: "error.bot", Message: "x"}, domain.RefusedNotAllowed},
		{EmitEventCommand{Name: "done.bot", Message: "x"}, domain.RefusedNotAllowed},
		{EmitEventCommand{Name: "halt", Message: "x", To: []string{"stranger"}}, domain.RefusedNotMember},
	}
	for _, c := range cases {
		if answer := emit(t, h, "web-1", run.ExecutionID, c.command); answer.Refused == nil || answer.Refused.Reason != c.reason {
			t.Errorf("%+v: %+v", c.command, answer)
		}
	}
	if refused, ok := lastEvent(h, domain.EventEventRefused); !ok || refused.Payload["reason"] != domain.RefusedNotMember {
		t.Errorf("refused event = %+v", refused)
	}
	if answer := emit(t, h, "web-1", run.ExecutionID, EmitEventCommand{Name: "halt", Message: "first"}); answer.Refused != nil {
		t.Fatalf("first = %+v", answer)
	}
	if answer := emit(t, h, "web-1", run.ExecutionID, EmitEventCommand{Name: "halt", Message: "second"}); answer.Refused == nil || answer.Refused.Reason != domain.RefusedRateLimited {
		t.Errorf("second = %+v", answer)
	}
	h.clock.now = h.clock.now.Add(time.Minute)
	if answer := emit(t, h, "web-1", run.ExecutionID, EmitEventCommand{Name: "halt", Message: "third"}); answer.Refused != nil {
		t.Errorf("after a minute = %+v", answer)
	}
}

func TestEventCallsAreValidated(t *testing.T) {
	h := newHarness()
	parent := opsTeam(t, h)
	bad := []EmitEventCommand{
		{Name: "Bad", Message: "x"}, {Name: "halt"},
		{Name: "halt", Message: "x", Data: json.RawMessage(`[1]`)}, {Name: "halt", Message: "x", Data: json.RawMessage(`{`)},
		{Name: "halt", Message: "x", Data: json.RawMessage(`{"a":"` + strings.Repeat("x", MaxEventData) + `"}`)},
		{Name: "halt", Message: "x", To: []string{"Bad Id"}}, {Name: "halt", Message: "x", To: make([]string, 17)},
	}
	for _, command := range bad {
		if _, err := NewEmitEvent(h.deps).Execute(context.Background(), "web-1", parent.ExecutionID, command); err == nil {
			t.Errorf("%+v accepted", command)
		}
	}
	if _, err := NewEmitEvent(h.deps).Execute(context.Background(), "web-2", parent.ExecutionID, EmitEventCommand{Name: "halt", Message: "x"}); err == nil {
		t.Error("another node emitted for the execution")
	}
	if answer := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "halt", Message: "x", Data: json.RawMessage(`null`), To: []string{"worker-1", "worker-1"}}); len(answer.Deliveries) != 1 {
		t.Errorf("null data, repeated target = %+v", answer)
	}
}

func TestEventChainsAreCutByDepthAndCycle(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	onlineNode(t, h, "web-1")
	deep := func(d *BotDefinition) { d.Delegation = &domain.DelegationSnapshot{MaxDepth: 2} }
	watcher := teamBot(t, h, "watcher", []string{"ops"}, deep)
	teamBot(t, h, "worker-1", []string{"ops"}, deep)
	teamBot(t, h, "worker-2", []string{"ops"}, deep)
	parent := runOnNode(t, h, watcher, "web-1", "ask-1")
	first := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "warn.halt", Message: "x", To: []string{"worker-1"}})
	if _, err := NewReportExecution(h.deps).Execute(ctx, "web-1", parent.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "told"}}); err != nil {
		t.Fatal(err)
	}
	child := nextFor(t, h, "web-1")
	if child.Run == nil || child.Run.TaskID.String() != first.Deliveries[0].TaskID {
		t.Fatalf("child = %+v", child.Run)
	}
	back := emit(t, h, "web-1", child.ExecutionID, EmitEventCommand{Name: "danger.ack", Message: "rolling back"})
	reasons := map[string]string{}
	for _, s := range back.Skipped {
		reasons[s.BotID] = s.Reason
	}
	if reasons["watcher"] != domain.RefusedCycle || len(back.Deliveries) != 1 || back.Deliveries[0].BotID != "worker-2" {
		t.Errorf("back = %+v", back)
	}
	if _, err := NewReportExecution(h.deps).Execute(ctx, "web-1", child.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "ok"}}); err != nil {
		t.Fatal(err)
	}
	grandchild := nextFor(t, h, "web-1")
	tooDeep := emit(t, h, "web-1", grandchild.ExecutionID, EmitEventCommand{Name: "danger.ack", Message: "and me", To: []string{"watcher"}})
	if len(tooDeep.Skipped) != 1 || tooDeep.Skipped[0].Reason != domain.RefusedDepthExceeded {
		t.Errorf("too deep = %+v", tooDeep)
	}
}

func TestEmitFailsWhenTheStoresDo(t *testing.T) {
	h := newHarness()
	parent := opsTeam(t, h)
	h.log.fail = errBoom
	if _, err := NewEmitEvent(h.deps).Execute(context.Background(), "web-1", parent.ExecutionID, EmitEventCommand{Name: "halt", Message: "x"}); !errors.Is(err, errBoom) {
		t.Errorf("reader failure: %v", err)
	}
	h.log.fail = nil
	h.bots.listFail = errBoom
	if _, err := NewEmitEvent(h.deps).Execute(context.Background(), "web-1", parent.ExecutionID, EmitEventCommand{Name: "halt", Message: "x"}); !errors.Is(err, errBoom) {
		t.Errorf("list failure: %v", err)
	}
}

func TestATeamsTimeline(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	parent := opsTeam(t, h)
	emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "halt", Message: "x", Severity: "warning", To: []string{"worker-1"}})
	all, err := NewListGroupEvents(h.deps).Execute(ctx, GroupEventsQuery{Name: "ops"})
	if err != nil || len(all) == 0 {
		t.Fatalf("timeline = %v %v", len(all), err)
	}
	for _, e := range all {
		if e.BotID == "stranger" {
			t.Error("a stranger's event is in the team's timeline")
		}
	}
	cut := all[len(all)-3]
	after, err := NewListGroupEvents(h.deps).Execute(ctx, GroupEventsQuery{Name: "ops", After: cut.ID.String()})
	if err != nil || len(after) != 2 {
		t.Errorf("after = %d %v", len(after), err)
	}
	if got, _ := NewListGroupEvents(h.deps).Execute(ctx, GroupEventsQuery{Name: "ops", After: "evt-unknown", Limit: 2}); len(got) != 2 {
		t.Errorf("unknown cursor = %d", len(got))
	}
	if got, _ := NewListGroupEvents(h.deps).Execute(ctx, GroupEventsQuery{Name: "ops", Limit: 5000}); len(got) != len(all) {
		t.Errorf("capped = %d", len(got))
	}
	if _, err := NewListGroupEvents(h.deps).Execute(ctx, GroupEventsQuery{Name: "ops", Limit: -1}); err == nil {
		t.Error("a negative limit was accepted")
	}
	if _, err := NewListGroupEvents(h.deps).Execute(ctx, GroupEventsQuery{Name: "sre"}); !domain.IsNotFound(err) {
		t.Errorf("missing team: %v", err)
	}
	h.log.fail = errBoom
	if _, err := NewListGroupEvents(h.deps).Execute(ctx, GroupEventsQuery{Name: "ops"}); !errors.Is(err, errBoom) {
		t.Errorf("reader failure: %v", err)
	}
}

func TestSerialRunnerEmits(t *testing.T) {
	h := newHarness()
	parent := opsTeam(t, h)
	runner := NewSerialRunner(h.deps)
	answer, err := runner.Emit(context.Background(), "web-1", parent.ExecutionID, EmitEventCommand{Name: "halt", Message: "x", To: []string{"worker-2"}})
	if err != nil || len(answer.Deliveries) != 1 {
		t.Errorf("runner emit = %+v %v", answer, err)
	}
}
