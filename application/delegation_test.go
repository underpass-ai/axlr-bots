package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// delegationBot registers and enables a bot that takes manual and
// delegated tasks, with the delegation and budgets given.
func delegationBot(t *testing.T, h *harness, id string, delegation *domain.DelegationSnapshot, budgets BudgetsDefinition) domain.Bot {
	t.Helper()
	ctx := context.Background()
	d := sampleDefinition()
	d.ID, d.Name, d.Tenant = id, id, "default"
	d.AllowedTools = []string{"local_read"}
	d.Triggers = []domain.TriggerSnapshot{{Type: "manual"}, {Type: "delegation"}}
	d.Delegation = delegation
	d.Budgets = budgets
	bot, err := NewRegisterBot(h.deps).Execute(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if bot, err = NewEnableBot(h.deps).Execute(ctx, bot.ID()); err != nil {
		t.Fatal(err)
	}
	return bot
}

// runOnNode submits a task to the bot and hands it to the node: the
// execution the node then holds.
func runOnNode(t *testing.T, h *harness, bot domain.Bot, node domain.NodeID, key string) Assignment {
	t.Helper()
	ctx := context.Background()
	if _, err := NewSubmitTask(h.deps).Execute(ctx, SubmitTaskCommand{BotID: bot.ID().String(), Objective: "Find out what the other bot knows", IdempotencyKey: key}); err != nil {
		t.Fatal(err)
	}
	return nextFor(t, h, node)
}

func nextFor(t *testing.T, h *harness, node domain.NodeID) Assignment {
	t.Helper()
	assignment, found, err := NewNextAssignment(h.deps).Execute(context.Background(), node)
	if err != nil || !found {
		t.Fatalf("next assignment: %v %v", found, err)
	}
	return assignment
}

func delegate(t *testing.T, h *harness, node domain.NodeID, execution domain.ExecutionID, bot, objective string) DelegationAnswer {
	t.Helper()
	answer, err := NewDelegateTask(h.deps).Execute(context.Background(), node, execution, DelegateCommand{Bot: bot, Objective: objective, Wait: true})
	if err != nil {
		t.Fatal(err)
	}
	return answer
}

func lastEvent(h *harness, kind domain.EventType) (domain.Event, bool) {
	for i := len(h.log.events) - 1; i >= 0; i-- {
		if h.log.events[i].Type == kind {
			return h.log.events[i], true
		}
	}
	return domain.Event{}, false
}

func TestDelegateCreatesTheChildUnderTheParentsTrace(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	onlineNode(t, h, "web-1")
	a := delegationBot(t, h, "bot-a", &domain.DelegationSnapshot{To: []string{"bot-b"}, MaxDepth: 1, BudgetShare: 0.5}, BudgetsDefinition{MaxModelCallsPerTask: 10, MaxToolCallsPerTask: 20, TaskTimeoutSeconds: 300})
	delegationBot(t, h, "bot-b", nil, BudgetsDefinition{MaxModelCallsPerTask: 8, MaxToolCallsPerTask: 6, TaskTimeoutSeconds: 600})
	parent := runOnNode(t, h, a, "web-1", "ask-1")
	if parent.Run == nil || !contains(parent.Run.AllowedTools, DelegateToolName) {
		t.Fatalf("the delegating bot's run has no delegation tool: %+v", parent.Run)
	}
	if offer := parent.Run.Delegation; offer == nil || len(offer.Targets) != 1 || offer.Targets[0] != "bot-b" || offer.Depth != 0 || offer.MaxDepth != 1 {
		t.Fatalf("offer = %+v", parent.Run.Delegation)
	}
	h.clock.now = t0.Add(100 * time.Second)
	answer := delegate(t, h, "web-1", parent.ExecutionID, "bot-b", "Say what you know about the deploy")
	if answer.Refused != nil || !answer.Created || answer.Depth != 1 || answer.Status != domain.TaskPending {
		t.Fatalf("answer = %+v", answer)
	}
	// 0.5 of 10 model calls and of 20 tool calls, under bot-b's own 8
	// and 6; half of the 200 s the parent has left.
	if answer.Grant.MaxModelCalls != 5 || answer.Grant.MaxToolCalls != 6 || !answer.Grant.Deadline.Equal(h.clock.now.Add(100*time.Second)) {
		t.Errorf("grant = %+v", answer.Grant)
	}
	if !answer.WaitUntil.Equal(t0.Add(300*time.Second - DelegationWaitMargin)) {
		t.Errorf("wait until %s", answer.WaitUntil)
	}
	child, _ := h.tasks.Get(ctx, answer.TaskID)
	parentTask, _ := h.tasks.Get(ctx, parent.Run.TaskID)
	lineage, ok := child.Lineage()
	if !ok || lineage.ParentTaskID != parentTask.ID().String() || lineage.ParentExecutionID != parent.ExecutionID.String() || len(lineage.Chain) != 1 || lineage.Chain[0] != "bot-a" {
		t.Errorf("lineage = %+v", lineage)
	}
	if child.CorrelationID() != parentTask.CorrelationID() || child.Trigger().Type != domain.TriggerDelegation || child.BotID() != "bot-b" {
		t.Errorf("child = %+v", child.Snapshot())
	}
	if kids := parentTask.Children(); len(kids) != 1 || kids[0] != child.ID() {
		t.Errorf("children = %v", kids)
	}
	if model, tools := parentTask.Granted(); model != 5 || tools != 6 {
		t.Errorf("granted = %d %d", model, tools)
	}
	event, ok := lastEvent(h, domain.EventTaskDelegated)
	if !ok || event.BotID != "bot-a" || event.TaskID != parentTask.ID() || event.ExecutionID != parent.ExecutionID || event.Payload["childTaskId"] != child.ID().String() {
		t.Errorf("delegated event = %+v", event)
	}
	if submitted, ok := lastEvent(h, domain.EventTaskSubmitted); !ok || submitted.BotID != "bot-b" {
		t.Errorf("submitted event = %+v", submitted)
	}
	if audit := h.log.audits[len(h.log.audits)-1]; audit.Action != "task.delegate" || audit.Outcome != domain.AuditOK {
		t.Errorf("audit = %+v", audit)
	}

	// The same ask again is the same child, granted once.
	again := delegate(t, h, "web-1", parent.ExecutionID, "bot-b", "Say what you know about the deploy")
	if again.Created || again.TaskID != child.ID() || again.Depth != 1 {
		t.Errorf("again = %+v", again)
	}
	parentTask, _ = h.tasks.Get(ctx, parent.Run.TaskID)
	if model, _ := parentTask.Granted(); model != 5 || len(parentTask.Children()) != 1 {
		t.Errorf("granted twice: %d %v", model, parentTask.Children())
	}

	// The child runs with its grant and until its deadline.
	h.clock.now = t0.Add(110 * time.Second)
	run := nextFor(t, h, "web-1")
	if run.Run == nil || run.Run.TaskID != child.ID() || run.Run.Budgets.MaxModelCallsPerTask() != 5 || run.Run.Budgets.MaxToolCallsPerTask() != 6 || run.Run.Timeout != 90*time.Second {
		t.Fatalf("child run = %+v", run.Run)
	}
	if run.Run.Delegation != nil || contains(run.Run.AllowedTools, DelegateToolName) {
		t.Errorf("bot-b may not delegate, yet its run offers the tool")
	}
	waiting, err := NewGetDelegation(h.deps).Execute(ctx, "web-1", parent.ExecutionID, child.ID())
	if err != nil || waiting.Done || waiting.Status != domain.TaskRunning {
		t.Errorf("while it runs: %+v %v", waiting, err)
	}
	if _, err := NewReportExecution(h.deps).Execute(ctx, "web-1", run.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "The deploy is green."}}); err != nil {
		t.Fatal(err)
	}
	done, err := NewGetDelegation(h.deps).Execute(ctx, "web-1", parent.ExecutionID, child.ID())
	if err != nil || !done.Done || done.Status != domain.TaskCompleted || done.Answer != "The deploy is green." || done.Depth != 1 {
		t.Errorf("after it ended: %+v %v", done, err)
	}
}

func TestDelegationIsRefusedForATargetNotAllowedOrUnavailable(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	onlineNode(t, h, "web-1")
	a := delegationBot(t, h, "bot-a", &domain.DelegationSnapshot{To: []string{"bot-b", "bot-paused", "bot-closed", "bot-ghost", "bot-other"}}, BudgetsDefinition{TaskTimeoutSeconds: 300})
	delegationBot(t, h, "bot-b", nil, BudgetsDefinition{})
	delegationBot(t, h, "bot-c", nil, BudgetsDefinition{})
	delegationBot(t, h, "bot-paused", nil, BudgetsDefinition{})
	if _, err := NewPauseBot(h.deps).Execute(ctx, "bot-paused"); err != nil {
		t.Fatal(err)
	}
	closed := sampleDefinition()
	closed.ID, closed.Name, closed.Tenant = "bot-closed", "bot-closed", "default"
	if bot, err := NewRegisterBot(h.deps).Execute(ctx, closed); err != nil {
		t.Fatal(err)
	} else if _, err := NewEnableBot(h.deps).Execute(ctx, bot.ID()); err != nil {
		t.Fatal(err)
	}
	other := sampleDefinition()
	other.ID, other.Name, other.Tenant = "bot-other", "bot-other", "another"
	other.Triggers = []domain.TriggerSnapshot{{Type: "delegation"}}
	if bot, err := NewRegisterBot(h.deps).Execute(ctx, other); err != nil {
		t.Fatal(err)
	} else if _, err := NewEnableBot(h.deps).Execute(ctx, bot.ID()); err != nil {
		t.Fatal(err)
	}
	parent := runOnNode(t, h, a, "web-1", "ask-1")
	cases := []struct{ target, reason, says string }{
		{"bot-c", domain.RefusedNotAllowed, "may not delegate to bot-c"},
		{"bot-paused", domain.RefusedTargetUnavailable, "Paused"},
		{"bot-closed", domain.RefusedTargetUnavailable, "no delegated task"},
		{"bot-ghost", domain.RefusedTargetUnavailable, "does not exist"},
		{"bot-other", domain.RefusedTargetUnavailable, "another tenant"},
	}
	for _, c := range cases {
		answer := delegate(t, h, "web-1", parent.ExecutionID, c.target, "Do it")
		if answer.Refused == nil || answer.Refused.Reason != c.reason || !strings.Contains(answer.Refused.Message, c.says) || answer.TaskID != "" {
			t.Errorf("%s: %+v", c.target, answer.Refused)
			continue
		}
		event, ok := lastEvent(h, domain.EventDelegationRefused)
		if !ok || event.Payload["reason"] != c.reason || event.Payload["target"] != c.target || event.BotID != "bot-a" || event.ExecutionID != parent.ExecutionID {
			t.Errorf("%s: event %+v", c.target, event)
		}
		if audit := h.log.audits[len(h.log.audits)-1]; audit.Action != "task.delegate" || audit.Outcome != domain.AuditDenied || audit.Details["reason"] != c.reason {
			t.Errorf("%s: audit %+v", c.target, audit)
		}
	}
	parentTask, _ := h.tasks.Get(ctx, parent.Run.TaskID)
	if len(parentTask.Children()) != 0 || parentTask.Status() != domain.TaskRunning {
		t.Errorf("a refusal changed the parent: %+v", parentTask.Snapshot())
	}
}

func TestDelegationDepthIsCutAlongTheChain(t *testing.T) {
	h := newHarness()
	onlineNode(t, h, "web-1")
	a := delegationBot(t, h, "bot-a", &domain.DelegationSnapshot{To: []string{"bot-b"}, MaxDepth: 2}, BudgetsDefinition{TaskTimeoutSeconds: 600})
	delegationBot(t, h, "bot-b", &domain.DelegationSnapshot{To: []string{"bot-c"}, MaxDepth: 5}, BudgetsDefinition{TaskTimeoutSeconds: 600})
	delegationBot(t, h, "bot-c", &domain.DelegationSnapshot{To: []string{"bot-d"}, MaxDepth: 5}, BudgetsDefinition{TaskTimeoutSeconds: 600})
	delegationBot(t, h, "bot-d", nil, BudgetsDefinition{})
	root := runOnNode(t, h, a, "web-1", "ask-1")
	first := delegate(t, h, "web-1", root.ExecutionID, "bot-b", "Ask bot-c")
	b := nextFor(t, h, "web-1")
	if b.Run.Delegation == nil || b.Run.Delegation.Depth != 1 || b.Run.Delegation.MaxDepth != 2 {
		t.Fatalf("bot-b's offer = %+v", b.Run.Delegation)
	}
	second := delegate(t, h, "web-1", b.ExecutionID, "bot-c", "Ask bot-d")
	if second.Refused != nil || second.Depth != 2 {
		t.Fatalf("depth 2 = %+v", second)
	}
	c := nextFor(t, h, "web-1")
	// bot-c allows 5, but bot-a's chain allows 2.
	third := delegate(t, h, "web-1", c.ExecutionID, "bot-d", "Do it")
	if third.Refused == nil || third.Refused.Reason != domain.RefusedDepthExceeded || !strings.Contains(third.Refused.Message, "depth 3") {
		t.Errorf("depth 3 = %+v", third)
	}
	if first.Depth != 1 {
		t.Errorf("first = %+v", first)
	}
}

func TestCircularDelegationIsCutByDepthAndByTheChain(t *testing.T) {
	cases := []struct {
		name     string
		maxDepth int
		reason   string
	}{
		{"by depth", 1, domain.RefusedDepthExceeded},
		{"by the visited set", 3, domain.RefusedCycle},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness()
			onlineNode(t, h, "web-1")
			a := delegationBot(t, h, "bot-a", &domain.DelegationSnapshot{To: []string{"bot-b"}, MaxDepth: c.maxDepth}, BudgetsDefinition{TaskTimeoutSeconds: 600})
			delegationBot(t, h, "bot-b", &domain.DelegationSnapshot{To: []string{"bot-a"}, MaxDepth: c.maxDepth}, BudgetsDefinition{TaskTimeoutSeconds: 600})
			root := runOnNode(t, h, a, "web-1", "ask-1")
			delegate(t, h, "web-1", root.ExecutionID, "bot-b", "Ask bot-a back")
			b := nextFor(t, h, "web-1")
			back := delegate(t, h, "web-1", b.ExecutionID, "bot-a", "Answer bot-b")
			if back.Refused == nil || back.Refused.Reason != c.reason {
				t.Fatalf("A→B→A = %+v", back)
			}
			if c.reason == domain.RefusedCycle && !strings.Contains(back.Refused.Message, "bot-a → bot-b") {
				t.Errorf("message = %s", back.Refused.Message)
			}
			if _, found, _ := NewNextAssignment(h.deps).Execute(context.Background(), "web-1"); found {
				t.Error("the loop created a task")
			}
		})
	}
}

func TestDelegationBudgetShareIsEnforced(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	onlineNode(t, h, "web-1")
	a := delegationBot(t, h, "bot-a", &domain.DelegationSnapshot{To: []string{"bot-b"}, BudgetShare: 0.5}, BudgetsDefinition{MaxModelCallsPerTask: 3, TaskTimeoutSeconds: 600})
	delegationBot(t, h, "bot-b", nil, BudgetsDefinition{MaxToolCallsPerTask: 4})
	parent := runOnNode(t, h, a, "web-1", "ask-1")
	first := delegate(t, h, "web-1", parent.ExecutionID, "bot-b", "One")
	second := delegate(t, h, "web-1", parent.ExecutionID, "bot-b", "Two")
	third := delegate(t, h, "web-1", parent.ExecutionID, "bot-b", "Three")
	// floor(0.5 × 3) = 1, floor(0.5 × 2) = 1, floor(0.5 × 1) = 0. bot-a
	// has no tool-call limit, so the children have bot-b's own 4.
	if first.Grant.MaxModelCalls != 1 || second.Grant.MaxModelCalls != 1 || first.Grant.MaxToolCalls != 4 {
		t.Errorf("grants = %+v %+v", first.Grant, second.Grant)
	}
	if third.Refused == nil || third.Refused.Reason != domain.RefusedBudgetExhausted || !strings.Contains(third.Refused.Message, "model calls") {
		t.Errorf("third = %+v", third)
	}
	parentTask, _ := h.tasks.Get(ctx, parent.Run.TaskID)
	if model, _ := parentTask.Granted(); model != 2 || len(parentTask.Children()) != 2 {
		t.Errorf("granted %d to %v", model, parentTask.Children())
	}
	// A grandchild shares its parent's grant, not the root's budget.
	child := nextFor(t, h, "web-1")
	if child.Run.Budgets.MaxModelCallsPerTask() != 1 {
		t.Errorf("child budgets = %+v", child.Run.Budgets)
	}
}

// askFour asks bot-b four tasks from one execution of bot-a, whose
// budgets are a pool of 20 model calls, and returns the grants.
func askFour(t *testing.T, delegation *domain.DelegationSnapshot) (*harness, Assignment, []DelegationAnswer) {
	t.Helper()
	h := newHarness()
	onlineNode(t, h, "web-1")
	a := delegationBot(t, h, "bot-a", delegation, BudgetsDefinition{MaxModelCallsPerTask: 20, TaskTimeoutSeconds: 600})
	delegationBot(t, h, "bot-b", nil, BudgetsDefinition{MaxModelCallsPerTask: 10, MaxToolCallsPerTask: 6, TaskTimeoutSeconds: 900})
	parent := runOnNode(t, h, a, "web-1", "ask-1")
	var answers []DelegationAnswer
	for _, objective := range []string{"One", "Two", "Three", "Four"} {
		answers = append(answers, delegate(t, h, "web-1", parent.ExecutionID, "bot-b", objective))
	}
	return h, parent, answers
}

func modelCallsOf(answers []DelegationAnswer) []int {
	grants := make([]int, len(answers))
	for i, answer := range answers {
		grants[i] = answer.Grant.MaxModelCalls
	}
	return grants
}

func TestTheGeometricSplitGivesLaterChildrenLess(t *testing.T) {
	// The issue's table: 0.5 of 20 model calls, children of 10.
	_, _, answers := askFour(t, &domain.DelegationSnapshot{To: []string{"bot-b"}, BudgetShare: 0.5})
	if got := modelCallsOf(answers); got[0] != 10 || got[1] != 5 || got[2] != 2 || got[3] != 1 {
		t.Errorf("geometric grants = %v, want [10 5 2 1]", got)
	}
}

func TestTheEvenSplitGivesEveryChildTheSameGrant(t *testing.T) {
	ctx := context.Background()
	h, parent, answers := askFour(t, &domain.DelegationSnapshot{To: []string{"bot-b"}, BudgetShare: 0.5, Split: "even", MaxChildren: 4})
	// floor(0.5 × 20 / 4) = 2 for each, under bot-b's own 10. bot-a has
	// no tool-call limit, so each has bot-b's own 6. The time is still
	// the share of what is left: half of 600 s, under bot-b's 900 s.
	for i, answer := range answers {
		if answer.Refused != nil || !answer.Created || answer.Grant.MaxModelCalls != 2 || answer.Grant.MaxToolCalls != 6 || !answer.Grant.Deadline.Equal(t0.Add(300*time.Second)) {
			t.Errorf("child %d = %+v", i+1, answer)
		}
	}
	parentTask, _ := h.tasks.Get(ctx, parent.Run.TaskID)
	if model, _ := parentTask.Granted(); model != 8 || len(parentTask.Children()) != 4 {
		t.Errorf("granted %d to %v", model, parentTask.Children())
	}

	// A fifth ask in the same task is refused, and says why.
	fifth := delegate(t, h, "web-1", parent.ExecutionID, "bot-b", "Five")
	if fifth.Refused == nil || fifth.Refused.Reason != domain.RefusedBudgetExhausted || !strings.Contains(fifth.Refused.Message, "maxChildren reached") {
		t.Errorf("fifth = %+v", fifth)
	}
	if event, ok := lastEvent(h, domain.EventDelegationRefused); !ok || event.Payload["reason"] != domain.RefusedBudgetExhausted || !strings.Contains(event.Payload["message"].(string), "maxChildren reached") {
		t.Errorf("refused event = %+v", event)
	}

	// Asking the fourth again, at the cap, is the same child: no second
	// grant and no refusal.
	again := delegate(t, h, "web-1", parent.ExecutionID, "bot-b", "Four")
	if again.Refused != nil || again.Created || again.TaskID != answers[3].TaskID || again.Grant.MaxModelCalls != 2 {
		t.Errorf("again = %+v", again)
	}
	parentTask, _ = h.tasks.Get(ctx, parent.Run.TaskID)
	if model, _ := parentTask.Granted(); model != 8 || len(parentTask.Children()) != 4 {
		t.Errorf("granted twice: %d to %v", model, parentTask.Children())
	}
}

func TestTheEvenSplitIsCappedByTheChildsOwnBudgetAndRefusesUnderOne(t *testing.T) {
	h := newHarness()
	onlineNode(t, h, "web-1")
	// The port fleet's dispatcher: 0.5 of 80 model calls and 48 tool
	// calls over 4 pilots of 10 and 6 is exactly each pilot's own.
	a := delegationBot(t, h, "bot-a", &domain.DelegationSnapshot{To: []string{"bot-b"}, BudgetShare: 0.5, Split: "even", MaxChildren: 4}, BudgetsDefinition{MaxModelCallsPerTask: 80, MaxToolCallsPerTask: 48, TaskTimeoutSeconds: 3600})
	delegationBot(t, h, "bot-b", nil, BudgetsDefinition{MaxModelCallsPerTask: 10, MaxToolCallsPerTask: 6, TaskTimeoutSeconds: 1500})
	parent := runOnNode(t, h, a, "web-1", "ask-1")
	for _, objective := range []string{"Plane", "Quad 0", "Quad 1", "Quad 2"} {
		if answer := delegate(t, h, "web-1", parent.ExecutionID, "bot-b", objective); answer.Grant.MaxModelCalls != 10 || answer.Grant.MaxToolCalls != 6 || !answer.Grant.Deadline.Equal(t0.Add(1500*time.Second)) {
			t.Errorf("%s = %+v", objective, answer)
		}
	}

	// A larger pool still gives no child more than its own budget:
	// floor(0.5 × 100 / 4) = 12, capped at 10 and 6.
	big := newHarness()
	onlineNode(t, big, "web-1")
	rich := delegationBot(t, big, "bot-a", &domain.DelegationSnapshot{To: []string{"bot-b"}, Split: "even", MaxChildren: 4}, BudgetsDefinition{MaxModelCallsPerTask: 100, MaxToolCallsPerTask: 100, TaskTimeoutSeconds: 600})
	delegationBot(t, big, "bot-b", nil, BudgetsDefinition{MaxModelCallsPerTask: 10, MaxToolCallsPerTask: 6})
	if answer := delegate(t, big, "web-1", runOnNode(t, big, rich, "web-1", "ask-1").ExecutionID, "bot-b", "One"); answer.Grant.MaxModelCalls != 10 || answer.Grant.MaxToolCalls != 6 {
		t.Errorf("capped = %+v", answer.Grant)
	}

	// With a pool of 3 model calls, 0.5 of them over 4 children is under
	// one each: refused before any child. The same for 3 tool calls over 2.
	for _, c := range []struct {
		budgets  BudgetsDefinition
		children int
		message  string
	}{
		{BudgetsDefinition{MaxModelCallsPerTask: 3, TaskTimeoutSeconds: 600}, 4, "3 model calls, and 0.50 of them split among 4 children"},
		{BudgetsDefinition{MaxModelCallsPerTask: 40, MaxToolCallsPerTask: 3, TaskTimeoutSeconds: 600}, 2, "3 tool calls, and 0.50 of them split among 2 children"},
	} {
		h := newHarness()
		onlineNode(t, h, "web-1")
		a := delegationBot(t, h, "bot-a", &domain.DelegationSnapshot{To: []string{"bot-b"}, Split: "even", MaxChildren: c.children}, c.budgets)
		delegationBot(t, h, "bot-b", nil, BudgetsDefinition{})
		parent := runOnNode(t, h, a, "web-1", "ask-1")
		if answer := delegate(t, h, "web-1", parent.ExecutionID, "bot-b", "One"); answer.Refused == nil || answer.Refused.Reason != domain.RefusedBudgetExhausted || !strings.Contains(answer.Refused.Message, c.message) {
			t.Errorf("under one = %+v", answer)
		}
	}
}

func TestMaxChildrenAlsoCapsTheGeometricSplit(t *testing.T) {
	h := newHarness()
	onlineNode(t, h, "web-1")
	a := delegationBot(t, h, "bot-a", &domain.DelegationSnapshot{To: []string{"bot-b"}, MaxChildren: 2}, BudgetsDefinition{MaxModelCallsPerTask: 40, TaskTimeoutSeconds: 600})
	delegationBot(t, h, "bot-b", nil, BudgetsDefinition{MaxModelCallsPerTask: 10})
	parent := runOnNode(t, h, a, "web-1", "ask-1")
	first := delegate(t, h, "web-1", parent.ExecutionID, "bot-b", "One")
	second := delegate(t, h, "web-1", parent.ExecutionID, "bot-b", "Two")
	third := delegate(t, h, "web-1", parent.ExecutionID, "bot-b", "Three")
	if first.Grant.MaxModelCalls != 10 || second.Grant.MaxModelCalls != 10 {
		t.Errorf("grants = %+v %+v", first.Grant, second.Grant)
	}
	if third.Refused == nil || third.Refused.Reason != domain.RefusedBudgetExhausted || !strings.Contains(third.Refused.Message, "maxChildren reached") {
		t.Errorf("third = %+v", third)
	}
}

func TestDelegationRespectsTheParentsDeadline(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	onlineNode(t, h, "web-1")
	a := delegationBot(t, h, "bot-a", &domain.DelegationSnapshot{To: []string{"bot-b"}, BudgetShare: 1}, BudgetsDefinition{TaskTimeoutSeconds: 120})
	delegationBot(t, h, "bot-b", nil, BudgetsDefinition{TaskTimeoutSeconds: 900})
	parent := runOnNode(t, h, a, "web-1", "ask-1")
	h.clock.now = t0.Add(60 * time.Second)
	answer := delegate(t, h, "web-1", parent.ExecutionID, "bot-b", "Quick")
	// The whole share of what is left, never past the parent's end,
	// though bot-b's own timeout is 15 minutes.
	if !answer.Grant.Deadline.Equal(t0.Add(120*time.Second)) || !answer.WaitUntil.Equal(t0.Add(110*time.Second)) {
		t.Errorf("grant %s, wait until %s", answer.Grant.Deadline, answer.WaitUntil)
	}
	// With under 30 s left there is nothing to share.
	h.clock.now = t0.Add(95 * time.Second)
	late := delegate(t, h, "web-1", parent.ExecutionID, "bot-b", "Too late")
	if late.Refused == nil || late.Refused.Reason != domain.RefusedBudgetExhausted || !strings.Contains(late.Refused.Message, "left") {
		t.Errorf("late = %+v", late)
	}
	// The child never ran (its node was busy): at its deadline it fails,
	// and the parent reads why.
	h.clock.now = t0.Add(121 * time.Second)
	if _, found, err := NewNextAssignment(h.deps).Execute(ctx, "web-1"); err != nil || found {
		t.Fatalf("a child past its deadline was assigned: %v %v", found, err)
	}
	child, _ := h.tasks.Get(ctx, answer.TaskID)
	if child.Status() != domain.TaskFailed || !strings.HasPrefix(child.FailureReason(), "deadline:") {
		t.Errorf("child = %s %q", child.Status(), child.FailureReason())
	}
	if event, ok := lastEvent(h, domain.EventTaskFailed); !ok || event.Payload["reason"] != "deadline" || event.TaskID != child.ID() {
		t.Errorf("failed event = %+v", event)
	}
}

func TestDelegatedTasksPastTheirDeadlineAreFailedWhereTheyWait(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	onlineNode(t, h, "web-1")
	a := delegationBot(t, h, "bot-a", &domain.DelegationSnapshot{To: []string{"bot-b"}}, BudgetsDefinition{TaskTimeoutSeconds: 600})
	delegationBot(t, h, "bot-b", nil, BudgetsDefinition{})
	parent := runOnNode(t, h, a, "web-1", "ask-1")
	waiting := delegate(t, h, "web-1", parent.ExecutionID, "bot-b", "Write it")
	pending := delegate(t, h, "web-1", parent.ExecutionID, "bot-b", "Write it again")
	run := nextFor(t, h, "web-1")
	if run.Run.TaskID != waiting.TaskID {
		t.Fatalf("ran %s", run.Run.TaskID)
	}
	if _, err := NewReportExecution(h.deps).Execute(ctx, "web-1", run.ExecutionID, ExecutionReport{Outcome: domain.ExecutionDecisionRequired, Result: domain.Result{SessionID: "sess-9", PendingDecision: &domain.PendingDecision{SessionID: "sess-9", CallID: "call-1", Tool: "local_write"}}}); err != nil {
		t.Fatal(err)
	}
	h.clock.now = waiting.Grant.Deadline.Add(time.Second)
	// The parent's node is alive and renews its lease.
	if _, err := NewRenewLease(h.deps).Execute(ctx, "web-1", parent.ExecutionID); err != nil {
		t.Fatal(err)
	}
	if _, err := NewApproveTask(h.deps).Execute(ctx, waiting.TaskID, DecisionApprove, ""); err == nil {
		t.Error("a decision after the delegation's deadline was taken")
	}
	result, err := NewReconcileTasks(h.deps).Execute(ctx, time.Hour)
	if err != nil || !containsTask(result.Failed, pending.TaskID) {
		t.Errorf("reconcile = %+v %v", result, err)
	}
	// Nobody decided the waiting one in time: it ends Expired, apart
	// from the one that never ran.
	for id, want := range map[domain.TaskID]domain.TaskStatus{waiting.TaskID: domain.TaskExpired, pending.TaskID: domain.TaskFailed} {
		task, _ := h.tasks.Get(ctx, id)
		if task.Status() != want || !strings.HasPrefix(task.FailureReason(), "deadline:") {
			t.Errorf("%s = %s %q", id, task.Status(), task.FailureReason())
		}
	}
	if !h.log.has("bot.task.expired", waiting.TaskID) || !h.log.has("bot.task.failed", pending.TaskID) {
		t.Errorf("events = %v", h.log.eventTypes())
	}
	read, err := NewGetDelegation(h.deps).Execute(ctx, "web-1", parent.ExecutionID, pending.TaskID)
	if err != nil || !read.Done || read.Status != domain.TaskFailed || !strings.HasPrefix(read.FailureReason, "deadline:") {
		t.Errorf("read = %+v %v", read, err)
	}
	read, err = NewGetDelegation(h.deps).Execute(ctx, "web-1", parent.ExecutionID, waiting.TaskID)
	if err != nil || !read.Done || read.Status != domain.TaskExpired || !strings.HasPrefix(read.FailureReason, "deadline:") {
		t.Errorf("read waiting = %+v %v", read, err)
	}
}

func TestDelegationIsOnlyForTheNodeThatHoldsARunningExecution(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	onlineNode(t, h, "web-1")
	onlineNode(t, h, "web-2")
	a := delegationBot(t, h, "bot-a", &domain.DelegationSnapshot{To: []string{"bot-b"}}, BudgetsDefinition{})
	delegationBot(t, h, "bot-b", nil, BudgetsDefinition{})
	parent := runOnNode(t, h, a, "web-1", "ask-1")
	child := delegate(t, h, "web-1", parent.ExecutionID, "bot-b", "Do it")
	if _, err := NewDelegateTask(h.deps).Execute(ctx, "web-2", parent.ExecutionID, DelegateCommand{Bot: "bot-b", Objective: "Do it"}); !isState(err) {
		t.Errorf("another node delegated: %v", err)
	}
	if _, err := NewGetDelegation(h.deps).Execute(ctx, "web-2", parent.ExecutionID, child.TaskID); !isState(err) {
		t.Errorf("another node read: %v", err)
	}
	if _, err := NewDelegateTask(h.deps).Execute(ctx, "web-1", parent.ExecutionID, DelegateCommand{Bot: "Not An Id", Objective: "Do it"}); err == nil {
		t.Error("a bad bot id was taken")
	}
	if _, err := NewDelegateTask(h.deps).Execute(ctx, "web-1", parent.ExecutionID, DelegateCommand{Bot: "bot-b"}); err == nil {
		t.Error("an empty objective was taken")
	}
	if _, err := NewDelegateTask(h.deps).Execute(ctx, "web-1", "exec-404", DelegateCommand{Bot: "bot-b", Objective: "x"}); !domain.IsNotFound(err) {
		t.Errorf("unknown execution: %v", err)
	}
	// Another task's child is not this execution's to read.
	other := runOnNode(t, h, a, "web-2", "ask-2")
	if _, err := NewGetDelegation(h.deps).Execute(ctx, "web-2", other.ExecutionID, child.TaskID); !domain.IsNotFound(err) {
		t.Errorf("read another task's child: %v", err)
	}
	if _, err := NewReportExecution(h.deps).Execute(ctx, "web-1", parent.ExecutionID, ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "ok"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := NewDelegateTask(h.deps).Execute(ctx, "web-1", parent.ExecutionID, DelegateCommand{Bot: "bot-b", Objective: "Late"}); !isState(err) {
		t.Errorf("an ended execution delegated: %v", err)
	}
}

func TestADelegatingBotIsDefinedAndPatched(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	d := sampleDefinition()
	d.ID = "bot-a"
	d.Delegation = &domain.DelegationSnapshot{To: []string{"bot-a"}}
	if _, err := NewRegisterBot(h.deps).Execute(ctx, d); err == nil {
		t.Error("a bot delegating to itself was registered")
	}
	d.Delegation = &domain.DelegationSnapshot{To: []string{"bot-b"}, MaxDepth: 9}
	if _, err := NewRegisterBot(h.deps).Execute(ctx, d); err == nil {
		t.Error("maxDepth 9 was taken")
	}
	d.Delegation = nil
	bot, err := NewRegisterBot(h.deps).Execute(ctx, d)
	if err != nil || bot.Delegation().Enabled() || bot.DelegationDepth() != 2 {
		t.Fatalf("plain bot: %v %+v depth %d", err, bot.Delegation(), bot.DelegationDepth())
	}
	patched, err := NewUpdateBot(h.deps).Execute(ctx, "bot-a", BotPatch{Delegation: &domain.DelegationSnapshot{To: []string{"bot-b"}, MaxDepth: 3, BudgetShare: 0.25}})
	if err != nil || !patched.Delegation().Allows("bot-b") || patched.DelegationDepth() != 3 || patched.Delegation().BudgetShare() != 0.25 {
		t.Fatalf("patch: %v %+v", err, patched.Snapshot().Delegation)
	}
	if _, err := NewUpdateBot(h.deps).Execute(ctx, "bot-a", BotPatch{Delegation: &domain.DelegationSnapshot{To: []string{"bot-a"}}}); err == nil {
		t.Error("a patch to delegate to itself was taken")
	}
	if _, err := NewUpdateBot(h.deps).Execute(ctx, "bot-a", BotPatch{Delegation: &domain.DelegationSnapshot{BudgetShare: 2}}); err == nil {
		t.Error("a share of 2 was taken")
	}
	even, err := NewUpdateBot(h.deps).Execute(ctx, "bot-a", BotPatch{Delegation: &domain.DelegationSnapshot{To: []string{"bot-b"}, Split: "even", MaxChildren: 4}})
	if err != nil || even.Delegation().Split() != domain.SplitEven || even.Delegation().MaxChildren() != 4 {
		t.Fatalf("even patch: %v %+v", err, even.Snapshot().Delegation)
	}
	if _, err := NewUpdateBot(h.deps).Execute(ctx, "bot-a", BotPatch{Delegation: &domain.DelegationSnapshot{To: []string{"bot-b"}, Split: "even"}}); err == nil {
		t.Error("an even split without maxChildren was taken")
	}
}

func TestSerialRunnerDelegates(t *testing.T) {
	h := newHarness()
	onlineNode(t, h, "web-1")
	a := delegationBot(t, h, "bot-a", &domain.DelegationSnapshot{To: []string{"bot-b"}}, BudgetsDefinition{})
	delegationBot(t, h, "bot-b", nil, BudgetsDefinition{})
	parent := runOnNode(t, h, a, "web-1", "ask-1")
	answer, err := NewSerialRunner(h.deps).Delegate(context.Background(), "web-1", parent.ExecutionID, DelegateCommand{Bot: "bot-b", Objective: "Do it"})
	if err != nil || !answer.Created {
		t.Errorf("runner: %+v %v", answer, err)
	}
}

func isState(err error) bool {
	var state domain.StateError
	return errors.As(err, &state)
}

func contains(items []string, item string) bool {
	for _, candidate := range items {
		if candidate == item {
			return true
		}
	}
	return false
}

func containsTask(items []domain.TaskID, item domain.TaskID) bool {
	for _, candidate := range items {
		if candidate == item {
			return true
		}
	}
	return false
}
