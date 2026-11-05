package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// placedBot registers and enables a bot with the placement.
func placedBot(t *testing.T, h *harness, d BotDefinition, placement domain.PlacementSnapshot) domain.Bot {
	t.Helper()
	ctx := context.Background()
	d.Placement = placement
	bot, err := NewRegisterBot(h.deps).Execute(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	bot, err = NewEnableBot(h.deps).Execute(ctx, bot.ID())
	if err != nil {
		t.Fatal(err)
	}
	return bot
}

// waitOf is what the task waits for, as its answer says it.
func waitOf(t *testing.T, h *harness, id domain.TaskID) string {
	t.Helper()
	task, err := h.tasks.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	waits, err := NewTaskWaits(h.deps).Execute(context.Background(), []domain.Task{task})
	if err != nil {
		t.Fatal(err)
	}
	return waits[id]
}

// picked reports whether the single-process worker would run a task.
func picked(t *testing.T, h *harness) bool {
	t.Helper()
	_, found, err := NewRunNextPending(h.deps).Pick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return found
}

// A bot placed on a named node waits for it, never running in the
// control plane's process (QA L10): while the node is not registered,
// then Offline, the task stays Pending and says why; the node back
// Online takes it on its next poll.
func TestAPlacedBotWaitsForItsNamedNode(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := placedBot(t, h, sampleDefinition(), domain.PlacementSnapshot{Nodes: []string{"web-1"}})
	task := submit(t, h, bot, "k-placed").Task
	if picked(t, h) {
		t.Fatal("the control plane would run a placed bot's task in process")
	}
	if got := waitOf(t, h, task.ID()); got != "waiting for node web-1, which is not registered" {
		t.Errorf("wait = %q", got)
	}
	onlineNode(t, h, "web-1")
	if got := waitOf(t, h, task.ID()); got != "" {
		t.Errorf("a task its Online node may run waits: %q", got)
	}
	h.clock.now = t0.Add(2 * time.Minute)
	if _, err := NewMarkOfflineNodes(h.deps).Execute(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if picked(t, h) {
		t.Fatal("the control plane would run the task of a bot whose node is Offline")
	}
	if got := waitOf(t, h, task.ID()); got != "waiting for node web-1, which is Offline" {
		t.Errorf("wait = %q", got)
	}
	if _, ran, err := NewRunNextPending(h.deps).Execute(ctx); err != nil || ran || len(h.runtime.requests) != 0 {
		t.Fatalf("ran in process: %v %v", ran, err)
	}
	// The node comes back: its poll takes the task, which waits no more.
	if _, err := NewHeartbeatNode(h.deps).Execute(ctx, "web-1"); err != nil {
		t.Fatal(err)
	}
	assignment, found, err := NewNextAssignment(h.deps).Execute(ctx, "web-1")
	if err != nil || !found || assignment.Run == nil || assignment.Run.TaskID != task.ID() {
		t.Fatalf("assignment = %+v %v %v", assignment, found, err)
	}
	if got := waitOf(t, h, task.ID()); got != "" {
		t.Errorf("a Running task waits: %q", got)
	}
}

// A bot placed by labels waits for a node with them: none registered,
// then one Offline.
func TestAPlacedBotWaitsForANodeWithItsLabels(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := placedBot(t, h, sampleDefinition(), domain.PlacementSnapshot{Selector: map[string]string{"zone": "lab"}})
	task := submit(t, h, bot, "k-labelled").Task
	onlineNode(t, h, "web-1")
	if picked(t, h) {
		t.Fatal("the control plane would run the task of a bot no node matches")
	}
	if _, found, _ := NewNextAssignment(h.deps).Execute(ctx, "web-1"); found {
		t.Fatal("a node without the labels took the task")
	}
	if got := waitOf(t, h, task.ID()); got != "waiting for a node labelled zone=lab: no node has these labels" {
		t.Errorf("wait = %q", got)
	}
	lab := registration("lab-1")
	lab.Labels = map[string]string{"zone": "lab"}
	if _, err := NewRegisterNode(h.deps).Execute(ctx, lab); err != nil {
		t.Fatal(err)
	}
	h.clock.now = t0.Add(2 * time.Minute)
	if _, err := NewHeartbeatNode(h.deps).Execute(ctx, "web-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := NewMarkOfflineNodes(h.deps).Execute(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if picked(t, h) {
		t.Fatal("the control plane would run the task of a bot whose node is Offline")
	}
	if got := waitOf(t, h, task.ID()); got != "waiting for a node labelled zone=lab: lab-1 is Offline" {
		t.Errorf("wait = %q", got)
	}
}

// A named node Online without the bot's runtime is waited for as one
// that is away, and the wait names the runtime it lacks.
func TestAPlacedBotWaitsForItsNodeToHaveItsRuntime(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := placedBot(t, h, jevDefinition(), domain.PlacementSnapshot{Nodes: []string{"plain-1"}})
	task := submit(t, h, bot, "k-jev").Task
	onlineNode(t, h, "plain-1")
	if _, found, _ := NewNextAssignment(h.deps).Execute(ctx, "plain-1"); found {
		t.Fatal("a node without the runtime took a Jev bot's task")
	}
	if picked(t, h) {
		t.Fatal("the control plane would run a placed Jev bot's task in process")
	}
	if got := waitOf(t, h, task.ID()); got != "waiting for node plain-1, which lacks the jev runtime" {
		t.Errorf("wait = %q", got)
	}
	// The node registers again with jev: it takes the task.
	withJev := registration("plain-1")
	withJev.Capabilities.Runtimes = []string{"jev"}
	if _, err := NewRegisterNode(h.deps).Execute(ctx, withJev); err != nil {
		t.Fatal(err)
	}
	if assignment, found, err := NewNextAssignment(h.deps).Execute(ctx, "plain-1"); err != nil || !found || assignment.Run.TaskID != task.ID() {
		t.Fatalf("assignment = %+v %v %v", assignment, found, err)
	}
}

// A bot with no placement still runs in process while no node may run
// it, and waits for no node.
func TestABotWithNoPlacementStillRunsInProcess(t *testing.T) {
	h := newHarness()
	bot := enabledBot(t, h)
	task := submit(t, h, bot, "k-any").Task
	if got := waitOf(t, h, task.ID()); got != "" {
		t.Errorf("an any-node task waits: %q", got)
	}
	result, ran, err := NewRunNextPending(h.deps).Execute(context.Background())
	if err != nil || !ran || result.Task.ID() != task.ID() {
		t.Fatalf("run next: %v %v", err, ran)
	}
}

// The wait names every node the placement admits and what keeps each.
func TestTheWaitNamesEveryAdmittedNode(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	registered := func(id string, status domain.NodeStatus, labels map[string]string) domain.Node {
		snapshot := domain.NodeSnapshot{ID: id, HostName: id, Labels: labels, Status: status.String(), RegisteredAt: t0, LastSeenAt: t0}
		if status == domain.NodeRevoked {
			revoked := t0
			snapshot.RevokedAt = &revoked
		}
		node, err := domain.RestoreNode(snapshot)
		if err != nil {
			t.Fatal(err)
		}
		return node
	}
	nodes := []domain.Node{
		registered("web-1", domain.NodeOffline, nil),
		registered("web-3", domain.NodeRegistered, map[string]string{"zone": "lab"}),
		registered("web-4", domain.NodeRevoked, map[string]string{"zone": "lab"}),
		registered("web-5", domain.NodeOnline, map[string]string{"zone": "eu"}),
	}
	cases := []struct {
		placement domain.PlacementSnapshot
		want      string
	}{
		{domain.PlacementSnapshot{Nodes: []string{"web-1", "web-2"}}, "waiting for node web-1, web-2: web-1 is Offline, web-2 is not registered"},
		{domain.PlacementSnapshot{Nodes: []string{"web-3"}}, "waiting for node web-3, which is Registered but not yet Online"},
		{domain.PlacementSnapshot{Selector: map[string]string{"zone": "lab"}}, "waiting for a node labelled zone=lab: web-3 is Registered but not yet Online, web-4 is Revoked"},
		{domain.PlacementSnapshot{Nodes: []string{"web-1"}, Selector: map[string]string{"zone": "mars"}}, "waiting for node web-1 or a node labelled zone=mars: web-1 is Offline, no node has these labels"},
	}
	for i, c := range cases {
		d := sampleDefinition()
		d.ID = "bot-" + string(rune('a'+i))
		bot := placedBot(t, h, d, c.placement)
		if got, waits := placementWait(bot, nodes); !waits || got != c.want {
			t.Errorf("%+v: wait = %q %v, want %q", c.placement, got, waits, c.want)
		}
	}
	// A paused bot waits for itself, not for a node.
	paused := placedBot(t, h, sampleDefinition(), domain.PlacementSnapshot{Nodes: []string{"web-1"}})
	paused, _ = NewPauseBot(h.deps).Execute(ctx, paused.ID())
	if got, waits := placementWait(paused, nodes); waits {
		t.Errorf("a paused bot waits: %q", got)
	}
}

// The waits read each bot and the nodes once, answer nothing for a task
// whose bot is gone, and stop at a store's error.
func TestTaskWaitsReadsOnceAndStopsAtAnError(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := placedBot(t, h, sampleDefinition(), domain.PlacementSnapshot{Nodes: []string{"web-1"}})
	first := submit(t, h, bot, "k-1").Task
	second := submit(t, h, bot, "k-2").Task
	gone, _ := domain.NewTask("task-gone", "bot-gone", "Do it", "k-gone", "trace-1", t0)
	waits, err := NewTaskWaits(h.deps).Execute(ctx, []domain.Task{first, second, gone})
	if err != nil || len(waits) != 2 || waits[first.ID()] != waits[second.ID()] || !strings.HasPrefix(waits[first.ID()], "waiting for node web-1") {
		t.Fatalf("waits = %v %v", waits, err)
	}
	if waits, err := NewTaskWaits(h.deps).Execute(ctx, nil); err != nil || len(waits) != 0 {
		t.Errorf("no task: %v %v", waits, err)
	}
	h.nodes.listFail = errBoom
	if _, err := NewTaskWaits(h.deps).Execute(ctx, []domain.Task{first}); !errors.Is(err, errBoom) {
		t.Errorf("nodes: %v", err)
	}
	h.nodes.listFail = nil
	failing := &failingBots{memoryBots: h.bots, err: errBoom}
	h.deps.Bots = failing
	if _, err := NewTaskWaits(h.deps).Execute(ctx, []domain.Task{first}); !errors.Is(err, errBoom) {
		t.Errorf("bots: %v", err)
	}
}

// failingBots answers every read with an error.
type failingBots struct {
	*memoryBots
	err error
}

func (f *failingBots) Get(context.Context, domain.BotID) (domain.Bot, error) {
	return domain.Bot{}, f.err
}

// A placed child no node took fails at its grant's deadline with what it
// waited for, in its reason and in its end event.
func TestAPlacedChildPastItsDeadlineSaysWhatItWaitedFor(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	onlineNode(t, h, "web-1")
	a := delegationBot(t, h, "bot-a", &domain.DelegationSnapshot{To: []string{"bot-b"}, BudgetShare: 1}, BudgetsDefinition{TaskTimeoutSeconds: 120})
	b := delegationBot(t, h, "bot-b", nil, BudgetsDefinition{TaskTimeoutSeconds: 900})
	placement := domain.PlacementSnapshot{Nodes: []string{"lab-1"}}
	if _, err := NewUpdateBot(h.deps).Execute(ctx, b.ID(), BotPatch{Placement: &placement}); err != nil {
		t.Fatal(err)
	}
	parent := runOnNode(t, h, a, "web-1", "ask-1")
	answer := delegate(t, h, "web-1", parent.ExecutionID, "bot-b", "Quick")
	if picked(t, h) {
		t.Fatal("the control plane would run a placed child in process")
	}
	// A node store's error stops the miss.
	pending, _ := h.tasks.Get(ctx, answer.TaskID)
	b, _ = h.bots.Get(ctx, b.ID())
	h.nodes.listFail = errBoom
	if err := newDeadlineKeeper(h.deps).miss(ctx, b, &pending, nil); !errors.Is(err, errBoom) {
		t.Errorf("miss with a failing node store: %v", err)
	}
	h.nodes.listFail = nil
	h.clock.now = answer.Grant.Deadline.Add(time.Second)
	if result, err := NewReconcileTasks(h.deps).Execute(ctx, 0); err != nil || !containsTask(result.Failed, answer.TaskID) {
		t.Fatalf("reconcile: %+v %v", result, err)
	}
	child, _ := h.tasks.Get(ctx, answer.TaskID)
	if child.Status() != domain.TaskFailed || !strings.HasSuffix(child.FailureReason(), "passed before the task ended; it was waiting for node lab-1, which is not registered") {
		t.Errorf("child = %s %q", child.Status(), child.FailureReason())
	}
	var told bool
	for _, event := range h.log.events {
		if event.TaskID == answer.TaskID && event.Type == domain.EventTaskFailed && event.Payload["waiting"] == "waiting for node lab-1, which is not registered" {
			told = true
		}
	}
	if !told {
		t.Error("the failed event does not say what the child waited for")
	}
}
