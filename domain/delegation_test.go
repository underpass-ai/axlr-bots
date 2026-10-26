package domain

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"
)

func TestDelegationValidatesItsTargetsDepthAndShare(t *testing.T) {
	d, err := NewDelegation(DelegationSnapshot{To: []string{"bot-b", "bot-c"}, MaxDepth: 2, BudgetShare: 0.25})
	if err != nil || !d.Enabled() || !d.Allows("bot-c") || d.Allows("bot-x") || d.MaxDepth() != 2 || d.BudgetShare() != 0.25 || len(d.To()) != 2 {
		t.Fatalf("delegation = %+v %v", d, err)
	}
	if s := d.Snapshot(); s == nil || s.To[1] != "bot-c" || s.MaxDepth != 2 || s.BudgetShare != 0.25 {
		t.Errorf("snapshot = %+v", s)
	}
	var none Delegation
	if none.Enabled() || none.Snapshot() != nil || none.BudgetShare() != DefaultBudgetShare {
		t.Errorf("zero value = %+v", none)
	}
	many := make([]string, 17)
	for i := range many {
		many[i] = "bot-" + string(rune('a'+i))
	}
	for _, bad := range []DelegationSnapshot{
		{To: many}, {To: []string{"Bad Id"}}, {To: []string{"bot-b", "bot-b"}},
		{MaxDepth: -1}, {MaxDepth: 9}, {BudgetShare: -0.1}, {BudgetShare: 1.5}, {BudgetShare: math.NaN()},
	} {
		if _, err := NewDelegation(bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}

func TestDelegationValidatesItsSplitAndMaxChildren(t *testing.T) {
	var none Delegation
	if none.Split() != SplitGeometric || none.MaxChildren() != 0 {
		t.Errorf("zero value: %s %d", none.Split(), none.MaxChildren())
	}
	even, err := NewDelegation(DelegationSnapshot{To: []string{"bot-b"}, Split: "even", MaxChildren: 4})
	if err != nil || even.Split() != SplitEven || even.MaxChildren() != 4 {
		t.Fatalf("even = %+v %v", even, err)
	}
	if s := even.Snapshot(); s == nil || s.Split != "even" || s.MaxChildren != 4 {
		t.Errorf("snapshot = %+v", s)
	}
	geometric, err := NewDelegation(DelegationSnapshot{To: []string{"bot-b"}, Split: "geometric", MaxChildren: 32})
	if err != nil || geometric.Split() != SplitGeometric || geometric.MaxChildren() != 32 || geometric.Snapshot().Split != "geometric" {
		t.Fatalf("geometric = %+v %v", geometric, err)
	}
	// A split named alone is kept, so the definition reads back as given.
	if alone, _ := NewDelegation(DelegationSnapshot{Split: "geometric"}); alone.Snapshot() == nil || alone.Snapshot().Split != "geometric" {
		t.Errorf("split alone = %+v", alone.Snapshot())
	}
	if limit, _ := NewDelegation(DelegationSnapshot{MaxChildren: 2}); limit.Snapshot() == nil || limit.Snapshot().MaxChildren != 2 || limit.Split() != SplitGeometric {
		t.Errorf("maxChildren alone = %+v", limit.Snapshot())
	}
	for _, bad := range []DelegationSnapshot{
		{Split: "fair", MaxChildren: 4}, {Split: "Even", MaxChildren: 4}, {Split: "even"}, {Split: "even", MaxChildren: 33},
		{MaxChildren: -1}, {MaxChildren: 33},
	} {
		if _, err := NewDelegation(bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	if split, err := ParseDelegationSplit(""); err != nil || split != SplitGeometric || split.String() != "geometric" {
		t.Errorf("empty split = %q %v", split, err)
	}
}

func TestBotDelegationDepthAndSnapshot(t *testing.T) {
	bot := sampleBot(t)
	if bot.DelegationDepth() != 2 {
		t.Errorf("from the budgets: %d", bot.DelegationDepth())
	}
	self, _ := NewDelegation(DelegationSnapshot{To: []string{bot.ID().String()}})
	if err := bot.SetDelegation(self); err == nil {
		t.Error("a bot delegating to itself")
	}
	d, _ := NewDelegation(DelegationSnapshot{To: []string{"bot-b"}, MaxDepth: 4})
	if err := bot.SetDelegation(d); err != nil || bot.DelegationDepth() != 4 {
		t.Fatalf("set: %v depth %d", err, bot.DelegationDepth())
	}
	restored, err := RestoreBot(bot.Snapshot())
	if err != nil || !restored.Delegation().Allows("bot-b") || restored.DelegationDepth() != 4 {
		t.Errorf("restored: %v %+v", err, restored.Snapshot().Delegation)
	}
	even, _ := NewDelegation(DelegationSnapshot{To: []string{"bot-b"}, Split: "even", MaxChildren: 4})
	if err := bot.SetDelegation(even); err != nil {
		t.Fatal(err)
	}
	if restored, err := RestoreBot(bot.Snapshot()); err != nil || restored.Delegation().Split() != SplitEven || restored.Delegation().MaxChildren() != 4 {
		t.Errorf("restored even: %v %+v", err, restored.Snapshot().Delegation)
	}
	broken := bot.Snapshot()
	broken.Delegation = &DelegationSnapshot{MaxDepth: 12}
	if _, err := RestoreBot(broken); err == nil {
		t.Error("restored a bad delegation")
	}
	broken.Delegation = &DelegationSnapshot{To: []string{"bot-b"}, Split: "even"}
	if _, err := RestoreBot(broken); err == nil {
		t.Error("restored an even split without maxChildren")
	}
	broken.Delegation = &DelegationSnapshot{To: []string{bot.ID().String()}}
	if _, err := RestoreBot(broken); err == nil {
		t.Error("restored a bot delegating to itself")
	}
	plain := sampleBot(t)
	budgets, _ := NewBudgets(0, 0, 0, 0)
	if err := plain.Redefine(plain.Mission(), plain.AllowedTools(), plain.ModelPolicy(), budgets); err != nil || plain.DelegationDepth() != DefaultDelegationDepth {
		t.Errorf("default depth: %v %d", err, plain.DelegationDepth())
	}
	deep, _ := NewBudgets(0, 0, 20, 0)
	_ = plain.Redefine(plain.Mission(), plain.AllowedTools(), plain.ModelPolicy(), deep)
	if plain.DelegationDepth() != MaxDelegationDepthLimit {
		t.Errorf("capped depth: %d", plain.DelegationDepth())
	}
}

func lineage() TaskLineage {
	return TaskLineage{
		ParentTaskID: "task-1", ParentExecutionID: "exec-1", ParentBotID: "bot-b", Depth: 2, MaxDepth: 3,
		Chain: []string{"bot-a", "bot-b"}, Grant: DelegationGrant{MaxModelCalls: 3, Deadline: t0.Add(time.Minute)},
	}
}

func TestTaskLineageInvariants(t *testing.T) {
	l := lineage()
	if err := l.Validate(); err != nil || !l.Visited("bot-a") || l.Visited("bot-c") {
		t.Fatalf("lineage: %v", err)
	}
	for name, change := range map[string]func(*TaskLineage){
		"parent task":      func(l *TaskLineage) { l.ParentTaskID = "" },
		"parent execution": func(l *TaskLineage) { l.ParentExecutionID = "Bad" },
		"parent bot":       func(l *TaskLineage) { l.ParentBotID = "" },
		"depth zero":       func(l *TaskLineage) { l.Depth = 0 },
		"over the limit":   func(l *TaskLineage) { l.Depth = 4 },
		"limit over 8":     func(l *TaskLineage) { l.MaxDepth = 9 },
		"short chain":      func(l *TaskLineage) { l.Chain = []string{"bot-b"} },
		"bad chain":        func(l *TaskLineage) { l.Chain = []string{"Bad", "bot-b"} },
		"chain's end":      func(l *TaskLineage) { l.Chain = []string{"bot-b", "bot-a"} },
		"no deadline":      func(l *TaskLineage) { l.Grant.Deadline = time.Time{} },
		"negative grant":   func(l *TaskLineage) { l.Grant.MaxToolCalls = -1 },
	} {
		bad := lineage()
		change(&bad)
		if err := bad.Validate(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

func TestADelegatedTaskKeepsItsLineageChildrenAndDeadline(t *testing.T) {
	task, _ := NewTask("task-2", "bot-c", "Do it", "delegation:task-1:abc", "trace-1", t0)
	if task.Depth() != 0 || task.DeadlinePassed(t0.Add(time.Hour)) {
		t.Error("a task nobody delegated has a depth or a deadline")
	}
	if _, ok := task.Lineage(); ok {
		t.Error("lineage on a plain task")
	}
	bad := lineage()
	bad.Depth = 0
	if err := task.Link(bad); err == nil {
		t.Error("linked a bad lineage")
	}
	if err := task.Link(lineage()); err != nil || task.Depth() != 2 {
		t.Fatalf("link: %v", err)
	}
	got, _ := task.Lineage()
	got.Chain[0] = "changed"
	if again, _ := task.Lineage(); again.Chain[0] != "bot-a" {
		t.Error("the lineage's chain is shared")
	}
	grant := DelegationGrant{MaxModelCalls: 1, MaxToolCalls: 2, Deadline: t0.Add(30 * time.Second)}
	if err := task.AdoptChild("task-3", grant, t0); err == nil {
		t.Error("a Pending task adopted a child")
	}
	_ = task.Start("exec-2", t0)
	for _, child := range []TaskID{"", "task-2"} {
		if err := task.AdoptChild(child, grant, t0); err == nil {
			t.Errorf("adopted %q", child)
		}
	}
	if err := task.AdoptChild("task-3", DelegationGrant{}, t0); err == nil {
		t.Error("adopted a child without a deadline")
	}
	if err := task.AdoptChild("task-3", grant, t0); err != nil {
		t.Fatal(err)
	}
	_ = task.AdoptChild("task-4", grant, t0)
	if model, tools := task.Granted(); model != 2 || tools != 4 || len(task.Children()) != 2 {
		t.Errorf("granted %d %d to %v", model, tools, task.Children())
	}
	snapshot := task.Snapshot()
	data, _ := json.Marshal(snapshot)
	var decoded TaskSnapshot
	_ = json.Unmarshal(data, &decoded)
	restored, err := RestoreTask(decoded)
	if err != nil || restored.Depth() != 2 || len(restored.Children()) != 2 {
		t.Fatalf("restored: %v %+v", err, restored.Snapshot())
	}
	if model, _ := restored.Granted(); model != 2 {
		t.Errorf("restored granted %d", model)
	}
	for name, change := range map[string]func(*TaskSnapshot){
		"lineage":  func(s *TaskSnapshot) { s.Delegation.Depth = 0 },
		"children": func(s *TaskSnapshot) { s.Children = []string{"Bad"} },
		"granted":  func(s *TaskSnapshot) { s.GrantedModel = -1 },
	} {
		broken := task.Snapshot()
		change(&broken)
		if _, err := RestoreTask(broken); err == nil {
			t.Errorf("restored a bad %s", name)
		}
	}
	if err := task.MissDeadline(t0.Add(30 * time.Second)); err == nil {
		t.Error("missed a deadline that has not come")
	}
	if !task.DeadlinePassed(t0.Add(time.Minute)) {
		t.Error("the deadline has come")
	}
	if err := task.MissDeadline(t0.Add(time.Minute)); err != nil || task.Status() != TaskFailed || task.FailureReason() == "" {
		t.Fatalf("missed: %v %s", err, task.Status())
	}
	if err := task.MissDeadline(t0.Add(time.Hour)); err == nil {
		t.Error("an ended task missed its deadline twice")
	}
}

func TestAWaitingTaskThatMissesItsDeadlineClosesItsWaitAndEndsExpired(t *testing.T) {
	task, _ := NewTask("task-2", "bot-c", "Do it", "k", "trace-1", t0)
	_ = task.Link(lineage())
	_ = task.Start("exec-2", t0)
	if err := task.AwaitApproval(ApprovalWait{CallID: "call-1", Since: t0, Deadline: t0.Add(time.Hour), OnExpire: ExpireDeny}, t0); err != nil {
		t.Fatal(err)
	}
	if err := task.MissDeadline(t0.Add(2 * time.Minute)); err != nil {
		t.Fatal(err)
	}
	if wait, _ := task.Approval(); wait.Outcome != ApprovalExpired || task.Status() != TaskExpired || !strings.HasPrefix(task.FailureReason(), "deadline:") {
		t.Errorf("wait %+v, status %s", wait, task.Status())
	}
}

func TestDelegationTriggerTakesNoOptions(t *testing.T) {
	trigger, err := NewTrigger(TriggerSnapshot{Type: "delegation"})
	if err != nil || trigger.Type() != TriggerDelegation || trigger.Name() != "delegation" {
		t.Fatalf("trigger = %+v %v", trigger, err)
	}
	if _, err := NewTrigger(TriggerSnapshot{Type: "delegation", Objective: "x"}); err == nil {
		t.Error("a delegation trigger with an objective")
	}
	if ref := DelegationTrigger(); ref.Type != TriggerDelegation || ref.Name != "delegation" {
		t.Errorf("ref = %+v", ref)
	}
	for _, kind := range []EventType{EventTaskDelegated, EventDelegationRefused} {
		if parsed, err := ParseEventType(kind.String()); err != nil || parsed != kind || kind.AboutNode() {
			t.Errorf("%s: %v", kind, err)
		}
	}
}
