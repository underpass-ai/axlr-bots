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

// portTeam is the guide case's team: a plane and two quads in port, a
// paused quad, and a stranger in another team.
func portTeam(t *testing.T, h *harness) Assignment {
	t.Helper()
	onlineNode(t, h, "web-1")
	plane := teamBot(t, h, "plane", []string{"port"}, nil)
	teamBot(t, h, "quad-1", []string{"port"}, nil)
	teamBot(t, h, "quad-2", []string{"port"}, nil)
	paused := teamBot(t, h, "quad-3", []string{"port"}, nil)
	if _, err := NewPauseBot(h.deps).Execute(context.Background(), paused.ID()); err != nil {
		t.Fatal(err)
	}
	teamBot(t, h, "stranger", []string{"other"}, nil)
	return runOnNode(t, h, plane, "web-1", "ask-1")
}

func TestTheAssignmentOffersTheEmitTool(t *testing.T) {
	h := newHarness()
	run := portTeam(t, h).Run
	if run.Events == nil || !contains(run.AllowedTools, EmitEventToolName) || strings.Join(run.Events.Teammates, ",") != "quad-1,quad-2" || run.Events.Names[0] != "**" {
		t.Fatalf("events offer = %+v tools %v", run.Events, run.AllowedTools)
	}
	if got := run.NodeTools(); len(got) != 2 || got[1] != EmitEventToolName {
		t.Errorf("node tools = %v", got)
	}
}

func TestAWarningAddressedToATeammateBecomesItsTask(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	parent := portTeam(t, h)
	h.clock.now = t0.Add(10 * time.Second)
	answer := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "warn.ship-near", Message: "Ship 244 is entering\nyour zone", To: []string{"quad-1"}, Data: json.RawMessage(`{"mmsi": 244}`)})
	if answer.Refused != nil || len(answer.Deliveries) != 1 || answer.Deliveries[0].BotID != "quad-1" || !answer.Deliveries[0].Created || answer.Deliveries[0].Trigger != "team" || len(answer.Skipped) != 0 {
		t.Fatalf("answer = %+v", answer)
	}
	task, err := h.tasks.Get(ctx, domain.TaskID(answer.Deliveries[0].TaskID))
	if err != nil {
		t.Fatal(err)
	}
	objective := task.Objective().String()
	for _, want := range []string{"Your teammate plane says (warning, event warn.ship-near): Ship 244 is entering your zone", `The facts it gave: {"mmsi":244}`} {
		if !strings.Contains(objective, want) {
			t.Errorf("objective lacks %q: %s", want, objective)
		}
	}
	lineage, ok := task.Lineage()
	parentTask, _ := h.tasks.Get(ctx, parent.Run.TaskID)
	if !ok || lineage.EventID != answer.EventID || lineage.Depth != 1 || lineage.Chain[0] != "plane" || !lineage.Grant.Deadline.Equal(h.clock.now.Add(300*time.Second)) {
		t.Errorf("lineage = %+v", lineage)
	}
	if task.CorrelationID() != parentTask.CorrelationID() || task.Trigger().Type != domain.TriggerEvent {
		t.Errorf("task = %+v", task.Snapshot())
	}
	event, ok := lastEvent(h, domain.EventEventEmitted)
	if !ok || event.BotID != "plane" || event.Payload["eventId"] != answer.EventID || event.Payload["severity"] != "warning" || len(event.Payload["deliveries"].([]map[string]any)) != 1 {
		t.Errorf("emitted = %+v", event)
	}
	if audit := h.log.audits[len(h.log.audits)-1]; audit.Action != "event.emit" || audit.Outcome != domain.AuditOK {
		t.Errorf("audit = %+v", audit)
	}
	// The node retries the same call: the same event, nothing new, even
	// when an older node sends a severity, which is ignored.
	again := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "warn.ship-near", Message: "Ship 244 is entering your zone", Severity: "critical", To: []string{"quad-1"}, Data: json.RawMessage(`{ "mmsi" : 244 }`)})
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
	parent := portTeam(t, h)
	info := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "ship.sighted", Message: "a ship at the quay"})
	if info.Refused != nil || len(info.Deliveries) != 0 || len(info.Skipped) != 0 {
		t.Errorf("info broadcast = %+v", info)
	}
	warning := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "warn.ship-sighted", Message: "a ship in the lane"})
	if len(warning.Deliveries) != 2 || len(warning.Skipped) != 1 || warning.Skipped[0].BotID != "quad-3" || warning.Skipped[0].Reason != domain.RefusedTargetUnavailable {
		t.Errorf("warning broadcast = %+v", warning)
	}
}

func TestEventTriggersChooseWhatABotHears(t *testing.T) {
	h := newHarness()
	onlineNode(t, h, "web-1")
	plane := teamBot(t, h, "plane", []string{"port"}, nil)
	teamBot(t, h, "watcher", []string{"port"}, func(d *BotDefinition) {
		d.Triggers = []domain.TriggerSnapshot{{Type: "manual"}, {Type: "event", Name: "ships", Events: []string{"ship.*"}, Objective: "Look at {{.data.where}}: {{.message}}"}}
	})
	teamBot(t, h, "broken", []string{"port"}, func(d *BotDefinition) {
		d.Triggers = []domain.TriggerSnapshot{{Type: "event", Name: "ships", Events: []string{"ship.*"}, Objective: "Look at {{.data.where}}"}}
	})
	parent := runOnNode(t, h, plane, "web-1", "ask-1")
	answer := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "ship.sighted", Message: "a ship", Data: json.RawMessage(`{"where": "the north quay"}`)})
	if len(answer.Deliveries) != 2 || answer.Deliveries[1].Trigger != "ships" {
		t.Fatalf("answer = %+v", answer)
	}
	task, _ := h.tasks.Get(context.Background(), domain.TaskID(answer.Deliveries[1].TaskID))
	if task.Objective().String() != "Look at the north quay: a ship" {
		t.Errorf("objective = %q", task.Objective())
	}
	addressed := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "recall", Message: "come back", Severity: "warning", To: []string{"watcher"}})
	if len(addressed.Deliveries) != 0 || addressed.Skipped[0].Reason != "not_listening" {
		t.Errorf("addressed = %+v", addressed)
	}
	failing := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "ship.lost", Message: "a ship", To: []string{"broken"}})
	if len(failing.Skipped) != 1 || failing.Skipped[0].Reason != "objective" {
		t.Errorf("failing = %+v", failing)
	}
}

func TestEventRefusals(t *testing.T) {
	h := newHarness()
	onlineNode(t, h, "web-1")
	alone := teamBot(t, h, "alone", nil, nil)
	strict := teamBot(t, h, "strict", []string{"port"}, func(d *BotDefinition) {
		d.Events = &domain.EventsSnapshot{Emit: []domain.EmitRuleSnapshot{{Name: "recall", MaxSeverity: "info"}, {Name: "**.bot"}}, PerMinute: 1}
	})
	teamBot(t, h, "quad-1", []string{"port"}, nil)
	teamBot(t, h, "stranger", []string{"other"}, nil)
	lonely := runOnNode(t, h, alone, "web-1", "ask-1")
	if answer := emit(t, h, "web-1", lonely.ExecutionID, EmitEventCommand{Name: "recall", Message: "x"}); answer.Refused == nil || answer.Refused.Reason != domain.RefusedNoTeam {
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
		{EmitEventCommand{Name: "recall", Message: "x", To: []string{"stranger"}}, domain.RefusedNotMember},
	}
	for _, c := range cases {
		if answer := emit(t, h, "web-1", run.ExecutionID, c.command); answer.Refused == nil || answer.Refused.Reason != c.reason {
			t.Errorf("%+v: %+v", c.command, answer)
		}
	}
	if refused, ok := lastEvent(h, domain.EventEventRefused); !ok || refused.Payload["reason"] != domain.RefusedNotMember {
		t.Errorf("refused event = %+v", refused)
	}
	if answer := emit(t, h, "web-1", run.ExecutionID, EmitEventCommand{Name: "recall", Message: "first"}); answer.Refused != nil {
		t.Fatalf("first = %+v", answer)
	}
	if answer := emit(t, h, "web-1", run.ExecutionID, EmitEventCommand{Name: "recall", Message: "second"}); answer.Refused == nil || answer.Refused.Reason != domain.RefusedRateLimited {
		t.Errorf("second = %+v", answer)
	}
	h.clock.now = h.clock.now.Add(time.Minute)
	if answer := emit(t, h, "web-1", run.ExecutionID, EmitEventCommand{Name: "recall", Message: "third"}); answer.Refused != nil {
		t.Errorf("after a minute = %+v", answer)
	}
}

func TestEventCallsAreValidated(t *testing.T) {
	h := newHarness()
	parent := portTeam(t, h)
	bad := []EmitEventCommand{
		{Name: "Bad", Message: "x"}, {Name: "recall"},
		{Name: "recall", Message: "x", Data: json.RawMessage(`[1]`)}, {Name: "recall", Message: "x", Data: json.RawMessage(`{`)},
		{Name: "recall", Message: "x", Data: json.RawMessage(`{"a":"` + strings.Repeat("x", MaxEventData) + `"}`)},
		{Name: "recall", Message: "x", To: []string{"Bad Id"}}, {Name: "recall", Message: "x", To: make([]string, 17)},
	}
	for _, command := range bad {
		if _, err := NewEmitEvent(h.deps).Execute(context.Background(), "web-1", parent.ExecutionID, command); err == nil {
			t.Errorf("%+v accepted", command)
		}
	}
	if _, err := NewEmitEvent(h.deps).Execute(context.Background(), "web-2", parent.ExecutionID, EmitEventCommand{Name: "recall", Message: "x"}); err == nil {
		t.Error("another node emitted for the execution")
	}
	if answer := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "recall", Message: "x", Data: json.RawMessage(`null`), To: []string{"quad-1", "quad-1"}}); len(answer.Deliveries) != 1 {
		t.Errorf("null data, repeated target = %+v", answer)
	}
}

func TestEventChainsAreCutByDepthAndCycle(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	onlineNode(t, h, "web-1")
	deep := func(d *BotDefinition) { d.Delegation = &domain.DelegationSnapshot{MaxDepth: 2} }
	plane := teamBot(t, h, "plane", []string{"port"}, deep)
	teamBot(t, h, "quad-1", []string{"port"}, deep)
	teamBot(t, h, "quad-2", []string{"port"}, deep)
	parent := runOnNode(t, h, plane, "web-1", "ask-1")
	first := emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "warn.recall", Message: "x", To: []string{"quad-1"}})
	if _, err := NewReportExecution(h.deps).Execute(ctx, "web-1", parent.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "told"}}); err != nil {
		t.Fatal(err)
	}
	child := nextFor(t, h, "web-1")
	if child.Run == nil || child.Run.TaskID.String() != first.Deliveries[0].TaskID {
		t.Fatalf("child = %+v", child.Run)
	}
	back := emit(t, h, "web-1", child.ExecutionID, EmitEventCommand{Name: "warn.ack", Message: "going home"})
	reasons := map[string]string{}
	for _, s := range back.Skipped {
		reasons[s.BotID] = s.Reason
	}
	if reasons["plane"] != domain.RefusedCycle || len(back.Deliveries) != 1 || back.Deliveries[0].BotID != "quad-2" {
		t.Errorf("back = %+v", back)
	}
	if _, err := NewReportExecution(h.deps).Execute(ctx, "web-1", child.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "ok"}}); err != nil {
		t.Fatal(err)
	}
	grandchild := nextFor(t, h, "web-1")
	tooDeep := emit(t, h, "web-1", grandchild.ExecutionID, EmitEventCommand{Name: "warn.ack", Message: "and me", To: []string{"plane"}})
	if len(tooDeep.Skipped) != 1 || tooDeep.Skipped[0].Reason != domain.RefusedDepthExceeded {
		t.Errorf("too deep = %+v", tooDeep)
	}
}

func TestEmitFailsWhenTheStoresDo(t *testing.T) {
	h := newHarness()
	parent := portTeam(t, h)
	h.log.fail = errBoom
	if _, err := NewEmitEvent(h.deps).Execute(context.Background(), "web-1", parent.ExecutionID, EmitEventCommand{Name: "recall", Message: "x"}); !errors.Is(err, errBoom) {
		t.Errorf("reader failure: %v", err)
	}
	h.log.fail = nil
	h.bots.listFail = errBoom
	if _, err := NewEmitEvent(h.deps).Execute(context.Background(), "web-1", parent.ExecutionID, EmitEventCommand{Name: "recall", Message: "x"}); !errors.Is(err, errBoom) {
		t.Errorf("list failure: %v", err)
	}
}

func TestATeamsTimeline(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	parent := portTeam(t, h)
	emit(t, h, "web-1", parent.ExecutionID, EmitEventCommand{Name: "recall", Message: "x", Severity: "warning", To: []string{"quad-1"}})
	all, err := NewListGroupEvents(h.deps).Execute(ctx, GroupEventsQuery{Name: "port"})
	if err != nil || len(all) == 0 {
		t.Fatalf("timeline = %v %v", len(all), err)
	}
	for _, e := range all {
		if e.BotID == "stranger" {
			t.Error("a stranger's event is in the team's timeline")
		}
	}
	cut := all[len(all)-3]
	after, err := NewListGroupEvents(h.deps).Execute(ctx, GroupEventsQuery{Name: "port", After: cut.ID.String()})
	if err != nil || len(after) != 2 {
		t.Errorf("after = %d %v", len(after), err)
	}
	if got, _ := NewListGroupEvents(h.deps).Execute(ctx, GroupEventsQuery{Name: "port", After: "evt-unknown", Limit: 2}); len(got) != 2 {
		t.Errorf("unknown cursor = %d", len(got))
	}
	if got, _ := NewListGroupEvents(h.deps).Execute(ctx, GroupEventsQuery{Name: "port", Limit: 5000}); len(got) != len(all) {
		t.Errorf("capped = %d", len(got))
	}
	if _, err := NewListGroupEvents(h.deps).Execute(ctx, GroupEventsQuery{Name: "port", Limit: -1}); err == nil {
		t.Error("a negative limit was accepted")
	}
	if _, err := NewListGroupEvents(h.deps).Execute(ctx, GroupEventsQuery{Name: "sea"}); !domain.IsNotFound(err) {
		t.Errorf("missing team: %v", err)
	}
	h.log.fail = errBoom
	if _, err := NewListGroupEvents(h.deps).Execute(ctx, GroupEventsQuery{Name: "port"}); !errors.Is(err, errBoom) {
		t.Errorf("reader failure: %v", err)
	}
}

func TestSerialRunnerEmits(t *testing.T) {
	h := newHarness()
	parent := portTeam(t, h)
	runner := NewSerialRunner(h.deps)
	answer, err := runner.Emit(context.Background(), "web-1", parent.ExecutionID, EmitEventCommand{Name: "recall", Message: "x", To: []string{"quad-2"}})
	if err != nil || len(answer.Deliveries) != 1 {
		t.Errorf("runner emit = %+v %v", answer, err)
	}
}
