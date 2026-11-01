package application

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

const hookSecret = "0123456789abcdef-hook"

// governBot sets the bot's approvals, as a PATCH would.
func governBot(t *testing.T, h *harness, bot domain.Bot, approvals domain.ApprovalsSnapshot) domain.Bot {
	t.Helper()
	updated, err := NewUpdateBot(h.deps).Execute(context.Background(), bot.ID(), BotPatch{Approvals: &approvals})
	if err != nil {
		t.Fatal(err)
	}
	h.log.events, h.log.audits = nil, nil
	return updated
}

// waitingWith runs a task of a bot with the approvals until it waits.
func waitingWith(t *testing.T, h *harness, approvals domain.ApprovalsSnapshot) (domain.Bot, domain.Task) {
	t.Helper()
	bot := governBot(t, h, enabledBot(t, h), approvals)
	task := submit(t, h, bot, "alert-1").Task
	h.runtime.report = pendingReport("sess-1")
	res, err := NewRunTask(h.deps).Execute(context.Background(), task.ID())
	if err != nil || res.Task.Status() != domain.TaskWaitingApproval {
		t.Fatalf("run: %v %v", err, res.Task.Status())
	}
	h.log.events, h.log.audits = nil, nil
	return bot, res.Task
}

func TestAWaitCarriesItsDeadline(t *testing.T) {
	h := newHarness()
	bot := enabledBot(t, h)
	task := submit(t, h, bot, "k").Task
	h.runtime.report = pendingReport("sess-1")
	h.clock.now = t0.Add(time.Minute)
	res, err := NewRunTask(h.deps).Execute(context.Background(), task.ID())
	if err != nil {
		t.Fatal(err)
	}
	wait, ok := res.Task.Approval()
	if !ok || !wait.Open() || wait.CallID != "c1" || wait.Tool != "local_exec" || !wait.Deadline.Equal(h.clock.now.Add(time.Hour)) || wait.OnExpire != domain.ExpireDeny {
		t.Fatalf("wait = %+v", wait)
	}
	last := h.log.events[len(h.log.events)-1]
	if last.Type != domain.EventTaskWaitingApproval || last.Payload["deadline"] != wait.Deadline || last.Payload["callId"] != "c1" || last.Payload["onExpire"] != "deny" {
		t.Errorf("event = %+v", last)
	}
	if len(h.outbox.deliveries) != 0 {
		t.Error("a bot with no target notified someone")
	}
}

func TestExpiryDeniesTheCallInProcess(t *testing.T) {
	h := newHarness()
	_, task := waitingWith(t, h, domain.ApprovalsSnapshot{TTL: "2m"})
	runner := NewSerialRunner(h.deps)
	ctx := context.Background()
	h.clock.now = t0.Add(2 * time.Minute)
	if done, err := runner.ExpireApprovals(ctx); err != nil || len(done) != 0 {
		t.Fatalf("expired at the deadline itself: %v %v", done, err)
	}
	h.decider.report = ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "I did not write it.", SessionID: "sess-1"}}
	h.clock.now = t0.Add(3 * time.Minute)
	done, err := runner.ExpireApprovals(ctx)
	if err != nil || len(done) != 1 || done[0].Action != domain.ExpireDeny || done[0].Execution == nil {
		t.Fatalf("expire: %v %+v", err, done)
	}
	if len(h.decider.requests) != 1 || h.decider.requests[0].Decision != DecisionDeny || h.decider.requests[0].CallID != "c1" {
		t.Errorf("the denial was not handed to the session: %+v", h.decider.requests)
	}
	after, _ := h.tasks.Get(ctx, task.ID())
	wait, _ := after.Approval()
	if after.Status() != domain.TaskCompleted || wait.Outcome != domain.ApprovalExpired || wait.ClosedAt == nil {
		t.Errorf("task %s wait %+v", after.Status(), wait)
	}
	if got := h.log.eventTypes(); strings.Join(got, ",") != "bot.approval.expired,bot.task.decided,bot.task.completed" {
		t.Errorf("events = %v", got)
	}
	expired := h.log.events[0]
	if expired.Payload["action"] != "deny" || expired.Payload["callId"] != "c1" || expired.ExecutionID != task.CurrentExecutionID() {
		t.Errorf("expired event = %+v", expired)
	}
	if h.log.events[1].Payload["reason"] != "approval expired" || h.log.events[1].Payload["decision"] != "deny" {
		t.Errorf("decided event = %+v", h.log.events[1].Payload)
	}
	// A decision now authorises nothing, whatever it says.
	for _, decision := range []string{DecisionApprove, DecisionDeny} {
		_, err := runner.Decide(ctx, task.ID(), decision, "")
		var expiredErr domain.ApprovalExpiredError
		if !errors.As(err, &expiredErr) || expiredErr.CallID != "c1" || !expiredErr.Deadline.Equal(t0.Add(2*time.Minute)) {
			t.Errorf("late %s: %v", decision, err)
		}
	}
	if len(h.decider.requests) != 1 {
		t.Error("a late decision reached the runtime")
	}
	if done, err := runner.ExpireApprovals(ctx); err != nil || len(done) != 0 {
		t.Errorf("expired again: %v %v", done, err)
	}
}

func TestALateDecisionIsRefusedBeforeTheExpiryRuns(t *testing.T) {
	h := newHarness()
	_, task := waitingWith(t, h, domain.ApprovalsSnapshot{TTL: "2m"})
	h.clock.now = t0.Add(2*time.Minute + time.Second)
	_, err := NewApproveTask(h.deps).Execute(context.Background(), task.ID(), DecisionApprove, "c1")
	if !domain.IsApprovalExpired(err) {
		t.Fatalf("late approve: %v", err)
	}
	if len(h.decider.requests) != 0 || len(h.log.events) != 0 {
		t.Error("the late approve ran or published")
	}
	if last := h.log.audits[len(h.log.audits)-1]; last.Action != "task.decide" || last.Outcome != domain.AuditDenied || !strings.Contains(last.Details["reason"], "approval expired") {
		t.Errorf("audit = %+v", last)
	}
	after, _ := h.tasks.Get(context.Background(), task.ID())
	if after.Status() != domain.TaskWaitingApproval {
		t.Error("the refusal changed the task")
	}
}

func TestADecisionNamesThePendingCall(t *testing.T) {
	h := newHarness()
	_, task := waitingWith(t, h, domain.ApprovalsSnapshot{})
	_, err := NewApproveTask(h.deps).Execute(context.Background(), task.ID(), DecisionApprove, "c2")
	var state domain.StateError
	if !errors.As(err, &state) || !strings.Contains(err.Error(), "waiting on call c1") {
		t.Fatalf("another call: %v", err)
	}
	res, err := NewApproveTask(h.deps).Execute(context.Background(), task.ID(), DecisionApprove, "c1")
	if err != nil || res.Task.Status() != domain.TaskCompleted {
		t.Fatalf("the pending call: %v", err)
	}
	if wait, _ := res.Task.Approval(); wait.Outcome != domain.ApprovalApproved {
		t.Errorf("wait = %+v", wait)
	}
	// The task is over: an approve link for the call it waited on is a
	// state refusal, not an expiry.
	if _, err := NewApproveTask(h.deps).Execute(context.Background(), task.ID(), DecisionApprove, "c1"); domain.IsApprovalExpired(err) || !errors.As(err, &state) {
		t.Errorf("after the decision: %v", err)
	}
}

func TestExpiryQueuesTheDenialForTheNode(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := governBot(t, h, enabledBot(t, h), domain.ApprovalsSnapshot{TTL: "5m"})
	node := onlineNode(t, h, "web-1")
	task := submit(t, h, bot, "k").Task
	first, _, _ := NewNextAssignment(h.deps).Execute(ctx, node.ID())
	pending := ExecutionReport{Outcome: domain.ExecutionDecisionRequired, Result: domain.Result{SessionID: "sess-1", PendingDecision: &domain.PendingDecision{Tool: "local_write", CallID: "c1", SessionID: "sess-1"}}}
	if _, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), first.ExecutionID, pending); err != nil {
		t.Fatal(err)
	}
	h.clock.now = t0.Add(6 * time.Minute)
	done, err := NewSerialRunner(h.deps).ExpireApprovals(ctx)
	if err != nil || len(done) != 1 || done[0].Execution.NodeID() != node.ID() || done[0].Execution.Decision() != DecisionDeny || done[0].Task.Status() != domain.TaskRunning {
		t.Fatalf("expire: %v %+v", err, done)
	}
	if len(h.decider.requests) != 0 {
		t.Error("the control plane ran the node's denial")
	}
	assignment, found, err := NewNextAssignment(h.deps).Execute(ctx, node.ID())
	if err != nil || !found || assignment.Decide == nil || assignment.Decide.Decision != DecisionDeny || assignment.Decide.CallID != "c1" {
		t.Fatalf("assignment: %v %+v", err, assignment)
	}
	if _, err := NewApproveTask(h.deps).Execute(ctx, task.ID(), DecisionApprove, ""); !domain.IsApprovalExpired(err) {
		t.Errorf("late approve while the denial runs: %v", err)
	}
}

// Nobody approved a bot's call, the expiry handed the session a denial,
// and the node's denial run failed. The task is Expired, apart from a
// real failure, and the overview and the task list count it apart.
func TestAnExpiredApprovalWhoseDenialFailsEndsExpiredNotFailed(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := governBot(t, h, enabledBot(t, h), domain.ApprovalsSnapshot{TTL: "5m"})
	node := onlineNode(t, h, "worker-0")
	pending := ExecutionReport{Outcome: domain.ExecutionDecisionRequired, Result: domain.Result{SessionID: "sess-1", PendingDecision: &domain.PendingDecision{Tool: "axlr_call_tool", CallID: "c1", SessionID: "sess-1"}}}
	reason := "exit code 1: axlr-tui: open /tmp/axlr-state/axlr/sessions/sess-1.json: no such file or directory"
	failed := ExecutionReport{Outcome: domain.ExecutionFailed, FailureReason: reason}
	waitOn := func(key string) domain.TaskID {
		task := submit(t, h, bot, key).Task
		run, _, err := NewNextAssignment(h.deps).Execute(ctx, node.ID())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), run.ExecutionID, pending); err != nil {
			t.Fatal(err)
		}
		return task.ID()
	}
	expired := waitOn("watch-1")
	// A person approves the other one, and its run fails: a real failure.
	approved := waitOn("watch-2")
	if _, err := NewApproveTask(h.deps).Execute(ctx, approved, DecisionApprove, ""); err != nil {
		t.Fatal(err)
	}
	run, _, _ := NewNextAssignment(h.deps).Execute(ctx, node.ID())
	if _, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), run.ExecutionID, failed); err != nil {
		t.Fatal(err)
	}
	h.clock.now = t0.Add(6 * time.Minute)
	if done, err := NewSerialRunner(h.deps).ExpireApprovals(ctx); err != nil || len(done) != 1 || done[0].Action != domain.ExpireDeny {
		t.Fatalf("expire: %v %+v", err, done)
	}
	denial, _, _ := NewNextAssignment(h.deps).Execute(ctx, node.ID())
	if denial.Decide == nil || denial.Decide.TaskID != expired || denial.Decide.Decision != DecisionDeny {
		t.Fatalf("denial = %+v", denial)
	}
	h.log.events = nil
	if _, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), denial.ExecutionID, failed); err != nil {
		t.Fatal(err)
	}
	after, _ := h.tasks.Get(ctx, expired)
	if after.Status() != domain.TaskExpired || after.FailureReason() != reason {
		t.Fatalf("expired task = %s %q", after.Status(), after.FailureReason())
	}
	if other, _ := h.tasks.Get(ctx, approved); other.Status() != domain.TaskFailed {
		t.Errorf("approved task = %s", other.Status())
	}
	if !h.log.has("bot.task.expired", expired) || h.log.has("bot.task.failed", expired) {
		t.Errorf("events = %v", h.log.eventTypes())
	}
	overview, err := NewGetOverview(h.deps).Execute(ctx, time.Time{})
	if err != nil || overview.TasksSince["Expired"] != 1 || overview.TasksSince["Failed"] != 1 || overview.Tasks["Expired"] != 1 {
		t.Errorf("overview = %+v %v", overview, err)
	}
	for status, want := range map[string]domain.TaskID{"Expired": expired, "Failed": approved} {
		listed, err := NewListTasks(h.deps).Execute(ctx, TaskQuery{Status: status})
		if err != nil || len(listed) != 1 || listed[0].ID() != want {
			t.Errorf("list %s = %v %v", status, ids(listed), err)
		}
	}
}

// A denial whose node never reports is reconciled Expired too.
func TestALostDenialRunIsReconciledExpired(t *testing.T) {
	h := newHarness()
	ctx := context.Background()
	bot := governBot(t, h, enabledBot(t, h), domain.ApprovalsSnapshot{TTL: "5m"})
	node := onlineNode(t, h, "web-1")
	task := submit(t, h, bot, "k").Task
	first, _, _ := NewNextAssignment(h.deps).Execute(ctx, node.ID())
	pending := ExecutionReport{Outcome: domain.ExecutionDecisionRequired, Result: domain.Result{SessionID: "sess-1", PendingDecision: &domain.PendingDecision{Tool: "local_write", CallID: "c1", SessionID: "sess-1"}}}
	if _, err := NewReportExecution(h.deps).Execute(ctx, node.ID(), first.ExecutionID, pending); err != nil {
		t.Fatal(err)
	}
	h.clock.now = t0.Add(6 * time.Minute)
	if _, err := NewSerialRunner(h.deps).ExpireApprovals(ctx); err != nil {
		t.Fatal(err)
	}
	if denial, found, err := NewNextAssignment(h.deps).Execute(ctx, node.ID()); err != nil || !found || denial.Decide == nil {
		t.Fatalf("denial = %+v %v", denial, err)
	}
	h.clock.now = t0.Add(3 * time.Hour)
	result, err := NewReconcileTasks(h.deps).Execute(ctx, time.Hour)
	if err != nil || !containsTask(result.Failed, task.ID()) {
		t.Fatalf("reconcile = %+v %v", result, err)
	}
	after, _ := h.tasks.Get(ctx, task.ID())
	if after.Status() != domain.TaskExpired || !h.log.has("bot.task.expired", task.ID()) {
		t.Errorf("task = %s, events %v", after.Status(), h.log.eventTypes())
	}
}

func TestExpiryEndsTheTaskExpiredWhenTheBotSaysCancel(t *testing.T) {
	h := newHarness()
	_, task := waitingWith(t, h, domain.ApprovalsSnapshot{TTL: "1m", OnExpire: "cancel"})
	ctx := context.Background()
	h.clock.now = t0.Add(time.Hour)
	done, err := NewSerialRunner(h.deps).ExpireApprovals(ctx)
	if err != nil || len(done) != 1 || done[0].Action != domain.ExpireCancel || done[0].Task.Status() != domain.TaskExpired {
		t.Fatalf("expire: %v %+v", err, done)
	}
	if got := h.log.eventTypes(); strings.Join(got, ",") != "bot.approval.expired,bot.task.expired" {
		t.Errorf("events = %v", got)
	}
	ended := h.log.events[len(h.log.events)-1].Payload
	if ended["action"] != "cancel" || ended["reason"] != "approval expired: nobody decided the call c1 (local_exec) by 2026-10-23T15:01:00Z" {
		t.Errorf("expired event = %+v", ended)
	}
	if len(h.decider.requests) != 0 {
		t.Error("an expired task's session was asked to decide")
	}
	if _, err := NewApproveTask(h.deps).Execute(ctx, task.ID(), DecisionApprove, ""); !domain.IsApprovalExpired(err) {
		t.Errorf("late approve: %v", err)
	}
	audit := h.log.audits[0]
	if audit.Action != "approval.expire" || audit.Details["action"] != "cancel" {
		t.Errorf("audit = %+v", audit)
	}
}

func TestAPausedBotsExpiredTaskEndsExpired(t *testing.T) {
	h := newHarness()
	bot, task := waitingWith(t, h, domain.ApprovalsSnapshot{TTL: "1m"})
	ctx := context.Background()
	if _, err := NewPauseBot(h.deps).Execute(ctx, bot.ID()); err != nil {
		t.Fatal(err)
	}
	h.log.events = nil
	h.clock.now = t0.Add(time.Hour)
	done, err := NewSerialRunner(h.deps).ExpireApprovals(ctx)
	if err != nil || len(done) != 1 || done[0].Action != domain.ExpireCancel {
		t.Fatalf("expire: %v %+v", err, done)
	}
	if h.log.events[0].Payload["reason"] != "bot is Paused" || h.log.events[0].Payload["onExpire"] != "deny" {
		t.Errorf("expired event = %+v", h.log.events[0].Payload)
	}
	after, _ := h.tasks.Get(ctx, task.ID())
	if after.Status() != domain.TaskExpired {
		t.Errorf("task = %s", after.Status())
	}
}

func TestARecordedExpiryWhoseDenialFailedIsCarriedNextPass(t *testing.T) {
	h := newHarness()
	_, task := waitingWith(t, h, domain.ApprovalsSnapshot{TTL: "1m"})
	ctx := context.Background()
	h.clock.now = t0.Add(time.Hour)
	h.execs.fail = errBoom
	if _, err := NewSerialRunner(h.deps).ExpireApprovals(ctx); !errors.Is(err, errBoom) {
		t.Fatalf("expire with a failing store: %v", err)
	}
	h.execs.fail = nil
	stuck, _ := h.tasks.Get(ctx, task.ID())
	if wait, _ := stuck.Approval(); stuck.Status() != domain.TaskWaitingApproval || wait.Outcome != domain.ApprovalExpired {
		t.Fatalf("task %s wait %+v", stuck.Status(), wait)
	}
	h.log.events = nil
	done, err := NewSerialRunner(h.deps).ExpireApprovals(ctx)
	if err != nil || len(done) != 1 || done[0].Task.Status() != domain.TaskCompleted {
		t.Fatalf("second pass: %v %+v", err, done)
	}
	if got := h.log.eventTypes(); strings.Join(got, ",") != "bot.task.decided,bot.task.completed" {
		t.Errorf("the expiry was published twice: %v", got)
	}
}

func TestAWaitFromBeforeDeadlinesGetsOne(t *testing.T) {
	h := newHarness()
	bot, task := waitingWith(t, h, domain.ApprovalsSnapshot{TTL: "10m"})
	ctx := context.Background()
	snapshot := task.Snapshot()
	snapshot.Approval = nil
	legacy, err := domain.RestoreTask(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	_ = h.tasks.Save(ctx, legacy)
	h.clock.now = t0.Add(5 * time.Minute)
	if _, err := NewApproveTask(h.deps).Execute(ctx, task.ID(), DecisionApprove, "c9"); domain.IsApprovalExpired(err) {
		t.Fatalf("refused as expired within the bot's ttl: %v", err)
	}
	due, err := NewExpireApprovals(h.deps).Due(ctx)
	if err != nil || len(due) != 0 {
		t.Fatalf("due: %v %v", due, err)
	}
	recorded, _ := h.tasks.Get(ctx, task.ID())
	wait, ok := recorded.Approval()
	if !ok || !wait.Deadline.Equal(legacy.UpdatedAt().Add(10*time.Minute)) || wait.CallID != "c1" || wait.OnExpire != bot.Approvals().OnExpire() {
		t.Fatalf("recorded wait = %+v", wait)
	}
	_ = h.tasks.Save(ctx, legacy)
	h.clock.now = t0.Add(11 * time.Minute)
	if _, err := NewApproveTask(h.deps).Execute(ctx, task.ID(), DecisionApprove, ""); !domain.IsApprovalExpired(err) {
		t.Errorf("a legacy wait past the bot's ttl: %v", err)
	}
	if due, err := NewExpireApprovals(h.deps).Due(ctx); err != nil || len(due) != 1 {
		t.Errorf("due: %v %v", due, err)
	}
}

func TestExpiryPortErrors(t *testing.T) {
	h := newHarness()
	_, task := waitingWith(t, h, domain.ApprovalsSnapshot{TTL: "1m"})
	ctx := context.Background()
	h.clock.now = t0.Add(time.Hour)
	h.tasks.listFail = errBoom
	if _, err := NewSerialRunner(h.deps).ExpireApprovals(ctx); !errors.Is(err, errBoom) {
		t.Errorf("list: %v", err)
	}
	h.tasks.listFail = nil
	uc := NewExpireApprovals(h.deps)
	if _, err := uc.Expire(ctx, "task-missing"); !domain.IsNotFound(err) {
		t.Errorf("missing task: %v", err)
	}
	h.tasks.fail = errBoom
	if _, err := uc.Expire(ctx, task.ID()); !errors.Is(err, errBoom) {
		t.Errorf("save: %v", err)
	}
	h.tasks.fail = nil
	bots := h.bots.bots
	h.bots.bots = nil
	if _, err := uc.Expire(ctx, task.ID()); !domain.IsNotFound(err) {
		t.Errorf("bot gone: %v", err)
	}
	h.bots.bots = bots
	h.clock.now = t0
	if expired, err := uc.Expire(ctx, task.ID()); err != nil || expired.Action != "" {
		t.Errorf("not due yet: %v %+v", err, expired)
	}
}

func TestAWaitQueuesANotificationPerTarget(t *testing.T) {
	h := newHarness()
	h.deps.OperatorURL = "https://cp.example.internal:7879/"
	approvals := domain.ApprovalsSnapshot{TTL: "2m", Notify: []domain.NotifyTargetSnapshot{
		{Type: "webhook", Name: "ops", URL: "https://hooks.example.com/axlr", Secret: hookSecret},
		{Type: "command", Name: "desk", Command: []string{"/usr/local/bin/notify"}},
	}}
	bot, task := waitingWith(t, h, approvals)
	if len(h.outbox.deliveries) != 2 {
		t.Fatalf("deliveries = %d", len(h.outbox.deliveries))
	}
	pending, _ := h.outbox.ListPending(context.Background())
	for _, d := range pending {
		var n ApprovalNotification
		if err := json.Unmarshal(d.Payload(), &n); err != nil {
			t.Fatal(err)
		}
		if n.Schema != ApprovalNotificationSchema || n.DeliveryID != d.ID().String() || n.TaskID != task.ID().String() || n.BotID != bot.ID().String() ||
			n.Call.Tool != "local_exec" || n.Call.CallID != "c1" || n.Call.SessionID != "sess-1" || !n.Deadline.Equal(t0.Add(2*time.Minute)) || n.OnExpire != "deny" ||
			n.CorrelationID != "trace-abc" || n.Correlation.TraceID != "trace-abc" || n.Correlation.ExecutionID != task.CurrentExecutionID().String() {
			t.Errorf("notification = %+v", n)
		}
		if n.Actions.Approve.URL != "https://cp.example.internal:7879/v1/tasks/"+task.ID().String()+"/approve?call=c1" || n.Actions.Deny.Path != "/v1/tasks/"+task.ID().String()+"/deny?call=c1" || n.Actions.Task.Method != "GET" {
			t.Errorf("actions = %+v", n.Actions)
		}
		if d.CallID() != "c1" || d.ExecutionID() != task.CurrentExecutionID() {
			t.Errorf("delivery = %+v", d.Snapshot())
		}
	}
	// Queueing the same wait again changes nothing: the ids are derived.
	execution, _ := h.execs.Get(context.Background(), task.CurrentExecutionID())
	wait, _ := task.Approval()
	notifier := approvalNotifier{deps: h.deps, rec: recorder{deps: h.deps}}
	if err := notifier.queue(context.Background(), bot, task, execution, wait); err != nil || len(h.outbox.deliveries) != 2 || len(h.log.audits) != 0 {
		t.Errorf("queued twice: %v %d", err, len(h.outbox.deliveries))
	}
	ids := map[domain.DeliveryID]bool{}
	for id := range h.outbox.deliveries {
		ids[id] = true
	}
	h.outbox.deliveries = nil
	if err := notifier.queue(context.Background(), bot, task, execution, wait); err != nil || len(h.outbox.deliveries) != 2 {
		t.Fatalf("queue: %v", err)
	}
	for id := range h.outbox.deliveries {
		if !ids[id] {
			t.Errorf("delivery %s has a new id", id)
		}
	}
	if len(h.log.audits) != 2 || h.log.audits[0].Action != "approval.notify" || h.log.audits[0].Details["status"] != "queued" {
		t.Errorf("audits = %+v", h.log.audits)
	}
}

func TestBigArgumentsAreLeftOutOfTheNotification(t *testing.T) {
	h := newHarness()
	bot := governBot(t, h, enabledBot(t, h), domain.ApprovalsSnapshot{Notify: []domain.NotifyTargetSnapshot{{Type: "command", Command: []string{"/bin/true"}}}})
	task := submit(t, h, bot, "k").Task
	report := pendingReport("sess-1")
	report.Result.PendingDecision.Arguments = json.RawMessage(`{"content":"` + strings.Repeat("x", MaxNotifiedArguments) + `"}`)
	h.runtime.report = report
	if _, err := NewRunTask(h.deps).Execute(context.Background(), task.ID()); err != nil {
		t.Fatal(err)
	}
	pending, _ := h.outbox.ListPending(context.Background())
	var n ApprovalNotification
	_ = json.Unmarshal(pending[0].Payload(), &n)
	if !n.Call.ArgumentsOmitted || n.Call.Arguments != nil || n.Actions.Approve.URL != "" {
		t.Errorf("call = %+v actions %+v", n.Call, n.Actions)
	}
}

func TestDeliveriesRetryWithBackoffAndGiveUp(t *testing.T) {
	h := newHarness()
	waitingWith(t, h, domain.ApprovalsSnapshot{Notify: []domain.NotifyTargetSnapshot{{Type: "webhook", Name: "ops", URL: "https://hooks.example.com/axlr", Secret: hookSecret}}})
	ctx := context.Background()
	h.notify.failures = MaxDeliveryAttempts
	uc := NewDeliverNotifications(h.deps)
	waits := []time.Duration{10 * time.Second, 30 * time.Second, time.Minute, 5 * time.Minute, 15 * time.Minute}
	for i, wait := range waits {
		outcomes, err := uc.Execute(ctx)
		if err != nil || len(outcomes) != 1 || outcomes[0].Delivery.Status() != domain.DeliveryPending || outcomes[0].Reason != "HTTP 503" {
			t.Fatalf("attempt %d: %v %+v", i+1, err, outcomes)
		}
		if next := outcomes[0].Delivery.NextAttemptAt(); !next.Equal(h.clock.now.Add(wait)) {
			t.Errorf("attempt %d: next at %s, want +%s", i+1, next, wait)
		}
		if again, _ := uc.Execute(ctx); len(again) != 0 {
			t.Errorf("attempt %d was retried before its backoff", i+1)
		}
		h.clock.now = h.clock.now.Add(wait)
	}
	outcomes, err := uc.Execute(ctx)
	if err != nil || len(outcomes) != 1 || outcomes[0].Delivery.Status() != domain.DeliveryFailed || outcomes[0].Delivery.Attempts() != MaxDeliveryAttempts {
		t.Fatalf("last attempt: %v %+v", err, outcomes)
	}
	if again, _ := uc.Execute(ctx); len(again) != 0 || len(h.notify.sent) != MaxDeliveryAttempts {
		t.Errorf("a failed delivery was tried again: %d sends", len(h.notify.sent))
	}
	var failed, gaveUp int
	for _, a := range h.log.audits {
		if a.Action == "approval.notify" && a.Outcome == domain.AuditFailed {
			failed++
			if a.Details["status"] == "failed" {
				gaveUp++
			}
		}
	}
	if failed != MaxDeliveryAttempts || gaveUp != 1 {
		t.Errorf("audits: %d failed, %d gave up", failed, gaveUp)
	}
	if ids := map[string]bool{}; len(h.notify.sent) > 0 {
		for _, d := range h.notify.sent {
			ids[d.ID().String()] = true
		}
		if len(ids) != 1 || h.notify.targets[0].URL() != "https://hooks.example.com/axlr" {
			t.Errorf("every attempt carries the same delivery id: %v", ids)
		}
	}
}

func TestADeliveryIsSkippedOnceItsWaitEnded(t *testing.T) {
	h := newHarness()
	_, task := waitingWith(t, h, domain.ApprovalsSnapshot{Notify: []domain.NotifyTargetSnapshot{{Type: "command", Command: []string{"/bin/true"}}}})
	ctx := context.Background()
	if _, err := NewApproveTask(h.deps).Execute(ctx, task.ID(), DecisionApprove, ""); err != nil {
		t.Fatal(err)
	}
	outcomes, err := NewDeliverNotifications(h.deps).Execute(ctx)
	if err != nil || len(outcomes) != 1 || outcomes[0].Delivery.Status() != domain.DeliverySkipped || !strings.Contains(outcomes[0].Reason, "Completed") {
		t.Fatalf("outcomes: %v %+v", err, outcomes)
	}
	if len(h.notify.sent) != 0 {
		t.Error("a decided wait was announced")
	}
}

func TestADeliveryIsSkippedWhenItsTargetIsGone(t *testing.T) {
	h := newHarness()
	bot, _ := waitingWith(t, h, domain.ApprovalsSnapshot{Notify: []domain.NotifyTargetSnapshot{{Type: "command", Name: "desk", Command: []string{"/bin/true"}}}})
	governBot(t, h, bot, domain.ApprovalsSnapshot{})
	outcomes, err := NewDeliverNotifications(h.deps).Execute(context.Background())
	if err != nil || len(outcomes) != 1 || outcomes[0].Delivery.Status() != domain.DeliverySkipped || !strings.Contains(outcomes[0].Reason, "desk") {
		t.Fatalf("outcomes: %v %+v", err, outcomes)
	}
}

func TestDeliveriesSucceedAndPortErrors(t *testing.T) {
	h := newHarness()
	waitingWith(t, h, domain.ApprovalsSnapshot{Notify: []domain.NotifyTargetSnapshot{{Type: "command", Command: []string{"/bin/true"}}}})
	ctx := context.Background()
	uc := NewDeliverNotifications(h.deps)
	h.outbox.listFail = errBoom
	if _, err := uc.Execute(ctx); !errors.Is(err, errBoom) {
		t.Errorf("list: %v", err)
	}
	h.outbox.listFail = nil
	h.tasks.listFail = nil
	bots := h.bots.bots
	h.bots.bots = nil
	if outcomes, err := uc.Execute(ctx); err != nil || outcomes[0].Delivery.Status() != domain.DeliverySkipped {
		t.Errorf("bot gone: %v %+v", err, outcomes)
	}
	h.bots.bots = bots
	h2 := newHarness()
	waitingWith(t, h2, domain.ApprovalsSnapshot{Notify: []domain.NotifyTargetSnapshot{{Type: "command", Command: []string{"/bin/true"}}}})
	h2.outbox.fail = errBoom
	if _, err := NewDeliverNotifications(h2.deps).Execute(ctx); !errors.Is(err, errBoom) {
		t.Errorf("save: %v", err)
	}
	h2.outbox.fail = nil
	outcomes, err := NewDeliverNotifications(h2.deps).Execute(ctx)
	if err != nil || len(outcomes) != 1 || outcomes[0].Delivery.Status() != domain.DeliveryDelivered {
		t.Fatalf("delivered: %v %+v", err, outcomes)
	}
	h3 := newHarness()
	h3.outbox.getFail = errBoom
	bot := governBot(t, h3, enabledBot(t, h3), domain.ApprovalsSnapshot{Notify: []domain.NotifyTargetSnapshot{{Type: "command", Command: []string{"/bin/true"}}}})
	task := submit(t, h3, bot, "k").Task
	h3.runtime.report = pendingReport("sess-1")
	if _, err := NewRunTask(h3.deps).Execute(ctx, task.ID()); !errors.Is(err, errBoom) {
		t.Errorf("queue: %v", err)
	}
	h4 := newHarness()
	waitingWith(t, h4, domain.ApprovalsSnapshot{Notify: []domain.NotifyTargetSnapshot{{Type: "command", Command: []string{"/bin/true"}}}})
	h4.tasks.tasks = map[domain.TaskID]domain.Task{}
	if outcomes, err := NewDeliverNotifications(h4.deps).Execute(ctx); err != nil || outcomes[0].Reason != "the task is gone" {
		t.Errorf("task gone: %v %+v", err, outcomes)
	}
}

func TestOneBrokenWaitDoesNotHoldUpTheOthers(t *testing.T) {
	h := newHarness()
	_, task := waitingWith(t, h, domain.ApprovalsSnapshot{TTL: "1m"})
	ctx := context.Background()
	orphan, _ := domain.NewTask("task-orphan", "ghost", "o", "k-orphan", "", t0)
	_ = orphan.Start("exec-orphan", t0)
	_ = orphan.AwaitApproval(testWait(t0), t0)
	snapshot := orphan.Snapshot()
	snapshot.Approval = nil
	orphan, _ = domain.RestoreTask(snapshot)
	_ = h.tasks.Save(ctx, orphan)
	h.clock.now = t0.Add(time.Hour)
	done, err := NewSerialRunner(h.deps).ExpireApprovals(ctx)
	if !domain.IsNotFound(err) || !strings.Contains(err.Error(), "task-orphan") {
		t.Errorf("error = %v", err)
	}
	if len(done) != 1 || done[0].TaskID != task.ID() {
		t.Errorf("done = %+v", done)
	}
}

func TestAnUploadCutShortIsRefused(t *testing.T) {
	artifact := domain.Artifact{Name: "a.txt", Size: 10, SHA256: strings.Repeat("a", 64)}
	reader := newVerifiedReader(shortReader{}, artifact)
	if _, err := reader.Read(make([]byte, 16)); err == nil || !strings.Contains(err.Error(), "ended after 3 of the 10 bytes") {
		t.Errorf("err = %v", err)
	}
}

type shortReader struct{}

func (shortReader) Read(p []byte) (int, error) { return copy(p, "abc"), io.ErrUnexpectedEOF }
