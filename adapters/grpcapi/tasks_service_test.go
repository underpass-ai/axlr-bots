package grpcapi

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/durationpb"

	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func TestTasksServiceRunsATaskAndReadsItBack(t *testing.T) {
	f := newFixture(t, "completed")
	ctx := context.Background()
	tasks := operatorv1.NewTasksServiceClient(f.authed)
	registerAndEnable(t, f, "bot-a", "observe")

	submitted, err := tasks.SubmitTask(ctx, &operatorv1.SubmitTaskRequest{BotId: "bot-a", Objective: "say hi", IdempotencyKey: "k-1"})
	if err != nil || !submitted.GetCreated() || submitted.GetTask().GetStatus() != "Pending" || submitted.GetTask().GetTrigger().GetType() != "manual" {
		t.Fatalf("submit: %v %v", submitted, err)
	}
	id := submitted.GetTask().GetId()
	if again, err := tasks.SubmitTask(ctx, &operatorv1.SubmitTaskRequest{BotId: "bot-a", Objective: "say hi", IdempotencyKey: "k-1"}); err != nil || again.GetCreated() {
		t.Errorf("same key: %v %v", again, err)
	}
	if _, err := tasks.SubmitTask(ctx, &operatorv1.SubmitTaskRequest{BotId: "bot-a", Objective: "", IdempotencyKey: "k-9"}); err == nil {
		t.Error("an empty objective was taken")
	} else if k, c := kind(t, err); k != "validation" || c != "InvalidArgument" {
		t.Errorf("empty objective: %s %s", k, c)
	}

	run, err := tasks.RunTask(ctx, &operatorv1.RunTaskRequest{TaskId: id})
	if err != nil || run.GetTask().GetStatus() != "Completed" || run.GetExecution().GetOutcome() != "Completed" || run.GetExecution().GetResult().GetAnswer() == "" {
		t.Fatalf("run: %v %v", run, err)
	}
	if run.GetExecution().GetUsage() == nil || run.GetExecution().GetStartedAt() == nil || run.GetExecution().GetEndedAt() == nil {
		t.Errorf("execution: %v", run.GetExecution())
	}
	if got, err := tasks.GetTask(ctx, &operatorv1.GetTaskRequest{TaskId: id}); err != nil || got.GetTask().GetStatus() != "Completed" || len(got.GetTask().GetExecutionIds()) != 1 {
		t.Fatalf("get: %v %v", got, err)
	}
	_, err = tasks.GetTask(ctx, &operatorv1.GetTaskRequest{TaskId: "task-nope"})
	if k, c := kind(t, err); k != "not_found" || c != "NotFound" {
		t.Errorf("get unknown: %s %s", k, c)
	}

	list, err := tasks.ListTasks(ctx, &operatorv1.ListTasksRequest{BotId: "bot-a", Status: "Completed"})
	if err != nil || len(list.GetTasks()) != 1 {
		t.Fatalf("list: %v %v", list, err)
	}
	future := time.Now().Add(time.Hour)
	if none, err := tasks.ListTasks(ctx, &operatorv1.ListTasksRequest{Since: timestamp(future)}); err != nil || len(none.GetTasks()) != 0 {
		t.Errorf("since the future: %v %v", none, err)
	}
	if _, err := tasks.ListTasks(ctx, &operatorv1.ListTasksRequest{Limit: -3}); err == nil {
		t.Error("a negative limit was taken")
	}

	executions, err := tasks.ListTaskExecutions(ctx, &operatorv1.ListTaskExecutionsRequest{TaskId: id})
	if err != nil || len(executions.GetExecutions()) != 1 {
		t.Fatalf("executions: %v %v", executions, err)
	}
	report, err := tasks.GetTaskReport(ctx, &operatorv1.GetTaskReportRequest{TaskId: id})
	if err != nil || !strings.HasPrefix(report.GetContent(), "# Report") || report.GetTaskId() != id {
		t.Fatalf("report: %v %v", report, err)
	}

	events, err := tasks.ListTaskEvents(ctx, &operatorv1.ListTaskEventsRequest{TaskId: id})
	if err != nil || len(events.GetEvents()) < 2 || events.GetEvents()[0].GetTaskId() != id {
		t.Fatalf("events: %v %v", events, err)
	}
	first := events.GetEvents()[0].GetEventId()
	if rest, err := tasks.ListTaskEvents(ctx, &operatorv1.ListTaskEventsRequest{TaskId: id, After: first}); err != nil || len(rest.GetEvents()) != len(events.GetEvents())-1 {
		t.Errorf("after the first: %v %v", rest, err)
	}
	if _, err := tasks.ListTaskEvents(ctx, &operatorv1.ListTaskEventsRequest{TaskId: id, Limit: -1}); err == nil {
		t.Error("a negative limit was taken")
	}

	exec := operatorv1.NewExecutionsServiceClient(f.authed)
	got, err := exec.GetExecution(ctx, &operatorv1.GetExecutionRequest{ExecutionId: run.GetExecution().GetId()})
	if err != nil || got.GetExecution().GetTaskId() != id {
		t.Fatalf("execution: %v %v", got, err)
	}
	if _, err := exec.GetExecution(ctx, &operatorv1.GetExecutionRequest{ExecutionId: "exec-nope"}); err == nil {
		t.Error("an unknown execution")
	}
}

func TestTasksServiceCancelsAndTalksToATask(t *testing.T) {
	f := newFixture(t, "completed")
	ctx := context.Background()
	tasks := operatorv1.NewTasksServiceClient(f.authed)
	registerAndEnable(t, f, "bot-a", "observe")
	submitted, err := tasks.SubmitTask(ctx, &operatorv1.SubmitTaskRequest{BotId: "bot-a", Objective: "wait", IdempotencyKey: "k-1", Priority: "urgent"})
	if err != nil || submitted.GetTask().GetPriority() != "urgent" {
		t.Fatalf("submit: %v %v", submitted, err)
	}
	id := submitted.GetTask().GetId()
	if _, err := tasks.SendTaskMessage(ctx, &operatorv1.SendTaskMessageRequest{TaskId: id, Text: "hello"}); err == nil {
		t.Error("a message to a task that does not run")
	} else if k, c := kind(t, err); k != "state" || c != "FailedPrecondition" {
		t.Errorf("message: %s %s", k, c)
	}
	cancelled, err := tasks.CancelTask(ctx, &operatorv1.CancelTaskRequest{TaskId: id})
	if err != nil || !cancelled.GetChanged() || cancelled.GetTask().GetStatus() != "Cancelled" {
		t.Fatalf("cancel: %v %v", cancelled, err)
	}
	if again, err := tasks.CancelTask(ctx, &operatorv1.CancelTaskRequest{TaskId: id}); err != nil || again.GetChanged() {
		t.Errorf("cancel twice: %v %v", again, err)
	}
	if _, err := tasks.RunTask(ctx, &operatorv1.RunTaskRequest{TaskId: id}); err == nil {
		t.Error("ran a cancelled task")
	}
}

func TestTasksServiceDecidesWithAnActor(t *testing.T) {
	f := newFixture(t, "approval-flow")
	ctx := context.Background()
	tasks := operatorv1.NewTasksServiceClient(f.authed)
	bot := definition("bot-b", "act-with-approval")
	bot.Approvals = &operatorv1.Approvals{Approvers: []string{"alice", "oncall"}}
	bots := operatorv1.NewBotsServiceClient(f.authed)
	if _, err := bots.RegisterBot(ctx, &operatorv1.RegisterBotRequest{Definition: bot}); err != nil {
		t.Fatal(err)
	}
	if _, err := bots.EnableBot(ctx, &operatorv1.EnableBotRequest{BotId: "bot-b"}); err != nil {
		t.Fatal(err)
	}
	waiting := func(key string) string {
		submitted, err := tasks.SubmitTask(ctx, &operatorv1.SubmitTaskRequest{BotId: "bot-b", Objective: "Restart demo-broken.service", IdempotencyKey: key})
		if err != nil {
			t.Fatal(err)
		}
		run, err := tasks.RunTask(ctx, &operatorv1.RunTaskRequest{TaskId: submitted.GetTask().GetId()})
		if err != nil || run.GetTask().GetStatus() != "WaitingApproval" || run.GetTask().GetApproval().GetCallId() != "c1" {
			t.Fatalf("run: %v %v", run, err)
		}
		return submitted.GetTask().GetId()
	}
	as := func(actor string) context.Context { return metadata.AppendToOutgoingContext(ctx, ActorMetadata, actor) }
	id := waiting("k-1")

	_, err := tasks.ApproveTask(ctx, &operatorv1.ApproveTaskRequest{TaskId: id})
	if k, c := kind(t, err); k != "forbidden" || c != "PermissionDenied" {
		t.Errorf("approve with no actor: %s %s", k, c)
	}
	_, err = tasks.DenyTask(as("mallory"), &operatorv1.DenyTaskRequest{TaskId: id})
	if k, _ := kind(t, err); k != "forbidden" {
		t.Errorf("deny as mallory: %v", err)
	}
	_, err = tasks.DecideTask(ctx, &operatorv1.DecideTaskRequest{TaskId: id, Decision: "approve", DecidedBy: "mallory"})
	if k, _ := kind(t, err); k != "forbidden" {
		t.Errorf("decide as mallory: %v", err)
	}
	_, err = tasks.DecideTask(as("mallory"), &operatorv1.DecideTaskRequest{TaskId: id, Decision: "approve", DecidedBy: "alice"})
	if k, c := kind(t, err); k != "validation" || c != "InvalidArgument" {
		t.Errorf("two actors: %s %s", k, c)
	}
	_, err = tasks.ApproveTask(as("alice"), &operatorv1.ApproveTaskRequest{TaskId: id, CallId: "c9"})
	if k, c := kind(t, err); k != "state" || c != "FailedPrecondition" {
		t.Errorf("another call: %s %s", k, c)
	}
	if task, _ := tasks.GetTask(ctx, &operatorv1.GetTaskRequest{TaskId: id}); task.GetTask().GetStatus() != "WaitingApproval" {
		t.Fatalf("a refusal changed the task: %v", task)
	}
	approved, err := tasks.ApproveTask(as("alice"), &operatorv1.ApproveTaskRequest{TaskId: id, CallId: "c1"})
	if err != nil || approved.GetTask().GetApproval().GetOutcome() != domain.ApprovalApproved || approved.GetExecution() == nil {
		t.Fatalf("approve as alice: %v %v", approved, err)
	}
	denied, err := tasks.DenyTask(as("oncall"), &operatorv1.DenyTaskRequest{TaskId: waiting("k-2")})
	if err != nil || denied.GetTask().GetApproval().GetOutcome() != domain.ApprovalDenied {
		t.Fatalf("deny as oncall: %v %v", denied, err)
	}
	decided, err := tasks.DecideTask(as("oncall"), &operatorv1.DecideTaskRequest{TaskId: waiting("k-3"), Decision: "deny"})
	if err != nil || decided.GetTask().GetApproval().GetOutcome() != domain.ApprovalDenied {
		t.Fatalf("decide with the metadata alone: %v %v", decided, err)
	}
	same, err := tasks.DecideTask(as("alice"), &operatorv1.DecideTaskRequest{TaskId: waiting("k-4"), Decision: "approve", DecidedBy: "alice"})
	if err != nil || same.GetTask() == nil {
		t.Errorf("decided_by equal to the metadata: %v %v", same, err)
	}
}

func TestTasksServiceRefusesALateDecision(t *testing.T) {
	clock := &testClock{now: time.Now()}
	f := newFixtureWith(t, "approval-flow", func(d *application.Dependencies) { d.Clock = clock })
	ctx := context.Background()
	bot := definition("bot-b", "act-with-approval")
	bot.Approvals = &operatorv1.Approvals{Ttl: durationpb.New(time.Minute)}
	bots := operatorv1.NewBotsServiceClient(f.authed)
	if _, err := bots.RegisterBot(ctx, &operatorv1.RegisterBotRequest{Definition: bot}); err != nil {
		t.Fatal(err)
	}
	if _, err := bots.EnableBot(ctx, &operatorv1.EnableBotRequest{BotId: "bot-b"}); err != nil {
		t.Fatal(err)
	}
	tasks := operatorv1.NewTasksServiceClient(f.authed)
	submitted, _ := tasks.SubmitTask(ctx, &operatorv1.SubmitTaskRequest{BotId: "bot-b", Objective: "Restart demo-broken.service", IdempotencyKey: "k"})
	if _, err := tasks.RunTask(ctx, &operatorv1.RunTaskRequest{TaskId: submitted.GetTask().GetId()}); err != nil {
		t.Fatal(err)
	}
	clock.advance(time.Minute + time.Second)
	_, err := tasks.ApproveTask(ctx, &operatorv1.ApproveTaskRequest{TaskId: submitted.GetTask().GetId(), CallId: "c1"})
	if k, c := kind(t, err); k != "approval_expired" || c != "FailedPrecondition" {
		t.Errorf("late approval: %s %s", k, c)
	}
}
