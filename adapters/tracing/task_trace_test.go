package tracing

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func attr(attributes []attribute.KeyValue, key string) (attribute.Value, bool) {
	for _, kv := range attributes {
		if string(kv.Key) == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

func requireAttr(t *testing.T, attributes []attribute.KeyValue, key string, want any) {
	t.Helper()
	got, found := attr(attributes, key)
	if !found {
		t.Fatalf("attribute %s missing in %v", key, attributes)
	}
	if got.AsInterface() != want {
		t.Fatalf("attribute %s = %v, want %v", key, got.AsInterface(), want)
	}
}

// approvalTask is a task that ran, stopped on a call for a person for an
// hour, was approved and completed under a second execution.
func approvalTask(t *testing.T, h *harness, id, correlation string) domain.Task {
	task := h.newTask(id, correlation)
	first := h.execution(id+"-x1", task, t0.Add(time.Second))
	ok(t, task.Start(first.ID(), t0.Add(time.Second)))
	end1 := t0.Add(10 * time.Second)
	ok(t, first.AssignTo("node-1"))
	ok(t, first.RequireDecision(domain.Result{PendingDecision: &domain.PendingDecision{CallID: "c2", Tool: "write"}}, domain.Usage{Requests: 2, PromptTokens: 100, CompletionTokens: 20, CostUSD: 0.5}, "", end1))
	ok(t, task.AwaitApproval(domain.ApprovalWait{CallID: "c2", Tool: "write", Since: end1, Deadline: end1.Add(24 * time.Hour), OnExpire: domain.ExpireDeny}, end1))
	resumedAt := end1.Add(time.Hour)
	second := h.execution(id+"-x2", task, resumedAt)
	ok(t, task.Resume(second.ID(), domain.ApprovalApproved, resumedAt))
	ok(t, second.Complete(domain.Result{Answer: "done"}, domain.Usage{Requests: 1, CostUSD: 0.25, ToolCalls: 1}, "", resumedAt.Add(100*time.Second)))
	ok(t, task.Complete(resumedAt.Add(100*time.Second)))
	h.source.tasks[task.ID()] = task
	h.source.executions[first.ID()] = first
	h.source.executions[second.ID()] = second
	h.source.events[task.ID()] = []domain.Event{
		progress(task.ID(), first.ID(),
			application.ProgressItem{Kind: application.ProgressToolStarted, At: t0.Add(2 * time.Second), Tool: "read", CallID: "c1", Summary: "a file"},
			application.ProgressItem{Kind: application.ProgressToolEnded, At: t0.Add(4 * time.Second), Tool: "read", CallID: "c1", Status: "ok"},
			application.ProgressItem{Kind: application.ProgressToolStarted, At: t0.Add(5 * time.Second), Tool: "write", CallID: "c2"}),
		progress(task.ID(), second.ID(),
			application.ProgressItem{Kind: application.ProgressToolStarted, At: resumedAt.Add(time.Second), Tool: "write", CallID: "c2"},
			application.ProgressItem{Kind: application.ProgressToolEnded, At: resumedAt.Add(3 * time.Second), Tool: "write", CallID: "c2", Status: "failed", Error: "disk full"}),
		{Type: domain.EventTaskCompleted, TaskID: task.ID()},
	}
	return task
}

func TestTraceHierarchyTimestampsAndDerivedTraceID(t *testing.T) {
	h := newHarness(t)
	task := approvalTask(t, h, "task-1", "corr-123")
	built, err := h.trace.Build(context.Background(), task.ID())
	if err != nil || !built {
		t.Fatalf("Build = %v, %v", built, err)
	}
	spans := h.exporter.GetSpans()
	if len(spans) != 7 {
		t.Fatalf("got %d spans, want 7 (task, 2 executions, 3 tools, approval): %v", len(spans), names(spans))
	}
	wantTrace := traceIDFromCorrelation("corr-123")
	byID := map[trace.SpanID]string{}
	for _, span := range spans {
		if span.SpanContext.TraceID() != wantTrace {
			t.Fatalf("span %s in trace %s, want %s", span.Name, span.SpanContext.TraceID(), wantTrace)
		}
		byID[span.SpanContext.SpanID()] = span.Name
		if got, _ := attr(span.Resource.Attributes(), "service.name"); got.AsString() != ServiceName {
			t.Fatalf("service.name = %v", got)
		}
	}
	root := find(t, spans, "axlr.task")
	if root.Parent.IsValid() {
		t.Fatalf("root has a parent: %v", root.Parent)
	}
	if !root.StartTime.Equal(t0) || !root.EndTime.Equal(task.UpdatedAt()) {
		t.Fatalf("root %v..%v", root.StartTime, root.EndTime)
	}
	requireAttr(t, root.Attributes, "axlr.correlation_id", "corr-123")
	requireAttr(t, root.Attributes, "axlr.bot", "bot-a")
	requireAttr(t, root.Attributes, "axlr.task", "task-1")
	requireAttr(t, root.Attributes, "outcome", "Completed")

	first := findByAttr(t, spans, "axlr.execution", "axlr.execution", "task-1-x1")
	second := findByAttr(t, spans, "axlr.execution", "axlr.execution", "task-1-x2")
	for _, execution := range []tracetestSpan{first, second} {
		if execution.Parent.SpanID() != root.SpanContext.SpanID() {
			t.Fatalf("execution %v parent %v, want the task", execution.SpanContext.SpanID(), execution.Parent.SpanID())
		}
	}
	if !first.StartTime.Equal(t0.Add(time.Second)) || !first.EndTime.Equal(t0.Add(10*time.Second)) {
		t.Fatalf("first execution %v..%v", first.StartTime, first.EndTime)
	}
	requireAttr(t, first.Attributes, "axlr.node", "node-1")
	requireAttr(t, first.Attributes, "outcome", "DecisionRequired")
	requireAttr(t, first.Attributes, "axlr.tokens.prompt", int64(100))
	requireAttr(t, first.Attributes, "axlr.tokens.completion", int64(20))
	requireAttr(t, first.Attributes, "axlr.cost_usd", 0.5)
	requireAttr(t, second.Attributes, "axlr.cost_usd", 0.25)

	tool := find(t, spans, "axlr.tool read")
	if tool.Parent.SpanID() != first.SpanContext.SpanID() || !tool.StartTime.Equal(t0.Add(2*time.Second)) || !tool.EndTime.Equal(t0.Add(4*time.Second)) {
		t.Fatalf("read tool %v %v..%v", tool.Parent.SpanID(), tool.StartTime, tool.EndTime)
	}
	requireAttr(t, tool.Attributes, "outcome", "ok")
	// The write that was waiting for approval never ended in the first
	// execution: it ends with its execution.
	var open, failed *tracetestSpan
	for i := range spans {
		if spans[i].Name == "axlr.tool write" {
			if v, found := attr(spans[i].Attributes, "axlr.tool_unfinished"); found && v.AsBool() {
				open = &spans[i]
			} else {
				failed = &spans[i]
			}
		}
	}
	if open == nil || failed == nil {
		t.Fatalf("write tool spans: open %v failed %v", open, failed)
	}
	if !open.EndTime.Equal(first.EndTime) || open.Parent.SpanID() != first.SpanContext.SpanID() {
		t.Fatalf("unfinished write %v..%v", open.StartTime, open.EndTime)
	}
	if failed.Status.Code != codes.Error || failed.Status.Description != "disk full" || failed.Parent.SpanID() != second.SpanContext.SpanID() {
		t.Fatalf("failed write %+v", failed.Status)
	}

	wait := find(t, spans, "approval.wait")
	if wait.Parent.SpanID() != root.SpanContext.SpanID() || !wait.StartTime.Equal(t0.Add(10*time.Second)) || !wait.EndTime.Equal(t0.Add(10*time.Second+time.Hour)) {
		t.Fatalf("approval %v %v..%v", wait.Parent.SpanID(), wait.StartTime, wait.EndTime)
	}
	requireAttr(t, wait.Attributes, "outcome", "approved")
	for _, span := range spans {
		if span.Parent.IsValid() {
			if _, found := byID[span.Parent.SpanID()]; !found {
				t.Fatalf("span %s has an unknown parent", span.Name)
			}
		}
	}
}

func TestBuildIsRepeatableWithTheSameIDs(t *testing.T) {
	h := newHarness(t)
	task := approvalTask(t, h, "task-1", "corr")
	_, err := h.trace.Build(context.Background(), task.ID())
	ok(t, err)
	first := map[string]trace.SpanID{}
	for _, span := range h.exporter.GetSpans() {
		first[span.Name+span.StartTime.String()] = span.SpanContext.SpanID()
	}
	h.exporter.Reset()
	_, err = h.trace.Build(context.Background(), task.ID())
	ok(t, err)
	for _, span := range h.exporter.GetSpans() {
		if first[span.Name+span.StartTime.String()] != span.SpanContext.SpanID() {
			t.Fatalf("span %s changed its id between builds", span.Name)
		}
	}
}

func TestTraceIDWithoutCorrelationComesFromTheRootTask(t *testing.T) {
	h := newHarness(t)
	task := approvalTask(t, h, "task-9", "")
	_, err := h.trace.Build(context.Background(), task.ID())
	ok(t, err)
	root := find(t, h.exporter.GetSpans(), "axlr.task")
	if root.SpanContext.TraceID() != traceIDFromRoot("task-9") {
		t.Fatalf("trace id %s", root.SpanContext.TraceID())
	}
	if _, found := attr(root.Attributes, "axlr.correlation_id"); found {
		t.Fatal("no correlation id, no attribute")
	}
}

// child makes a delegated, failed task whose parent is parentID.
func child(t *testing.T, h *harness, id, parentID, correlation string) domain.Task {
	task := h.newTask(id, correlation)
	lineage := domain.TaskLineage{ParentTaskID: parentID, ParentExecutionID: parentID + "-x1", ParentBotID: "bot-a", Depth: 1, MaxDepth: 3, Chain: []string{"bot-a"}, Grant: domain.DelegationGrant{Deadline: t0.Add(time.Hour)}}
	ok(t, task.Link(lineage))
	run := h.execution(id+"-x1", task, t0.Add(20*time.Second))
	ok(t, task.Start(run.ID(), t0.Add(20*time.Second)))
	ok(t, run.Fail("boom", domain.Result{}, domain.Usage{}, "", t0.Add(30*time.Second)))
	ok(t, task.Fail("boom", t0.Add(30*time.Second)))
	h.source.tasks[task.ID()] = task
	h.source.executions[run.ID()] = run
	return task
}

func TestDelegatedTaskHangsUnderItsExistingParent(t *testing.T) {
	h := newHarness(t)
	parent := approvalTask(t, h, "task-1", "corr-123")
	kid := child(t, h, "task-2", parent.ID().String(), "other-text")
	built, err := h.trace.Build(context.Background(), kid.ID())
	if err != nil || !built {
		t.Fatalf("Build = %v, %v", built, err)
	}
	root := find(t, h.exporter.GetSpans(), "axlr.task")
	if root.SpanContext.TraceID() != traceIDFromCorrelation("corr-123") {
		t.Fatalf("a child lives in its parent's trace, got %s", root.SpanContext.TraceID())
	}
	if root.Parent.SpanID() != spanIDOf("execution", "task-1-x1") {
		t.Fatalf("parent span %s, want the parent's execution", root.Parent.SpanID())
	}
	requireAttr(t, root.Attributes, "axlr.correlation_id", "other-text")
	requireAttr(t, root.Attributes, "axlr.parent_task", "task-1")
	if root.Status.Code != codes.Error || root.Status.Description != "boom" {
		t.Fatalf("status %+v", root.Status)
	}
	if len(root.Links) != 0 {
		t.Fatal("a child with a parent is not linked")
	}
}

func TestDelegatedTaskWithoutItsParentIsLinked(t *testing.T) {
	h := newHarness(t)
	kid := child(t, h, "task-2", "task-gone", "corr-7")
	_, err := h.trace.Build(context.Background(), kid.ID())
	ok(t, err)
	root := find(t, h.exporter.GetSpans(), "axlr.task")
	if root.Parent.IsValid() {
		t.Fatal("no parent to hang from")
	}
	if len(root.Links) != 1 || root.Links[0].SpanContext.SpanID() != spanIDOf("execution", "task-gone-x1") || root.Links[0].SpanContext.TraceID() != traceIDFromCorrelation("corr-7") {
		t.Fatalf("links %+v", root.Links)
	}
}

func TestDelegatedChainWithoutCorrelationSharesTheTopTask(t *testing.T) {
	h := newHarness(t)
	top := approvalTask(t, h, "task-1", "")
	mid := child(t, h, "task-2", top.ID().String(), "")
	leaf := child(t, h, "task-3", mid.ID().String(), "")
	_, err := h.trace.Build(context.Background(), leaf.ID())
	ok(t, err)
	root := find(t, h.exporter.GetSpans(), "axlr.task")
	if root.SpanContext.TraceID() != traceIDFromRoot("task-1") {
		t.Fatalf("trace %s", root.SpanContext.TraceID())
	}
	h.exporter.Reset()
	// With the middle task gone the chain's top is the highest still stored.
	delete(h.source.tasks, mid.ID())
	_, err = h.trace.Build(context.Background(), leaf.ID())
	ok(t, err)
	root = find(t, h.exporter.GetSpans(), "axlr.task")
	if root.SpanContext.TraceID() != traceIDFromRoot("task-2") || len(root.Links) != 1 {
		t.Fatalf("trace %s links %v", root.SpanContext.TraceID(), root.Links)
	}
}

func TestTaskNotOverBuildsNothing(t *testing.T) {
	h := newHarness(t)
	task := h.newTask("task-1", "c")
	h.source.tasks[task.ID()] = task
	if built, err := h.trace.Build(context.Background(), task.ID()); built || err != nil {
		t.Fatalf("pending task: %v %v", built, err)
	}
	if built, err := h.trace.Build(context.Background(), "nope"); built || err != nil {
		t.Fatalf("missing task: %v %v", built, err)
	}
	h.source.failTask = errors.New("disk")
	if _, err := h.trace.Build(context.Background(), task.ID()); err == nil {
		t.Fatal("a read error is returned")
	}
	if len(h.exporter.GetSpans()) != 0 {
		t.Fatal("no span for a task that is not over")
	}
}

func TestCancelledTaskWithAnExecutionThatNeverEndedEndsWithTheTask(t *testing.T) {
	h := newHarness(t)
	task := h.newTask("task-1", "c")
	run := h.execution("task-1-x1", task, t0.Add(time.Second))
	ok(t, task.Start(run.ID(), t0.Add(time.Second)))
	ok(t, task.Cancel(t0.Add(time.Minute)))
	h.source.tasks[task.ID()] = task
	h.source.executions[run.ID()] = run
	_, err := h.trace.Build(context.Background(), task.ID())
	ok(t, err)
	execution := find(t, h.exporter.GetSpans(), "axlr.execution")
	if !execution.EndTime.Equal(t0.Add(time.Minute)) {
		t.Fatalf("end %v", execution.EndTime)
	}
	root := find(t, h.exporter.GetSpans(), "axlr.task")
	requireAttr(t, root.Attributes, "outcome", "Cancelled")
	if root.Status.Code == codes.Error {
		t.Fatal("a cancel is not an error")
	}
}

func TestMissingExecutionIsRecordedOnTheTaskSpan(t *testing.T) {
	h := newHarness(t)
	task := h.newTask("task-1", "c")
	ok(t, task.Start("task-1-x1", t0))
	ok(t, task.Complete(t0.Add(time.Second)))
	h.source.tasks[task.ID()] = task
	_, err := h.trace.Build(context.Background(), task.ID())
	ok(t, err)
	root := find(t, h.exporter.GetSpans(), "axlr.task")
	if len(root.Events) != 1 {
		t.Fatalf("events %v", root.Events)
	}
}

func TestApprovalThatExpiredIsAnErrorSpan(t *testing.T) {
	h := newHarness(t)
	task := h.newTask("task-1", "c")
	run := h.execution("task-1-x1", task, t0)
	ok(t, task.Start(run.ID(), t0))
	ok(t, run.RequireDecision(domain.Result{PendingDecision: &domain.PendingDecision{CallID: "c", Tool: "rm"}}, domain.Usage{}, "", t0.Add(time.Second)))
	ok(t, task.AwaitApproval(domain.ApprovalWait{CallID: "c", Tool: "rm", Since: t0.Add(time.Second), Deadline: t0.Add(time.Hour)}, t0.Add(time.Second)))
	ok(t, task.ExpireApproval(t0.Add(2*time.Hour)))
	ok(t, task.EndExpired(t0.Add(2*time.Hour)))
	h.source.tasks[task.ID()] = task
	h.source.executions[run.ID()] = run
	_, err := h.trace.Build(context.Background(), task.ID())
	ok(t, err)
	spans := h.exporter.GetSpans()
	wait := find(t, spans, "approval.wait")
	if wait.Status.Code != codes.Error || !wait.EndTime.Equal(t0.Add(2*time.Hour)) {
		t.Fatalf("wait %+v %v", wait.Status, wait.EndTime)
	}
	if find(t, spans, "axlr.task").Status.Code != codes.Error {
		t.Fatal("an expired task is an error")
	}
}

func TestCancelledApprovalEndsAtTheCancel(t *testing.T) {
	h := newHarness(t)
	task := h.newTask("task-1", "c")
	run := h.execution("task-1-x1", task, t0)
	ok(t, task.Start(run.ID(), t0))
	ok(t, task.AwaitApproval(domain.ApprovalWait{CallID: "c", Since: t0, Deadline: t0.Add(time.Hour)}, t0))
	ok(t, task.Cancel(t0.Add(time.Minute)))
	h.source.tasks[task.ID()] = task
	h.source.executions[run.ID()] = run
	_, err := h.trace.Build(context.Background(), task.ID())
	ok(t, err)
	wait := find(t, h.exporter.GetSpans(), "approval.wait")
	requireAttr(t, wait.Attributes, "outcome", "cancelled")
	if !wait.EndTime.Equal(t0.Add(time.Minute)) {
		t.Fatalf("end %v", wait.EndTime)
	}
}

func TestSpanNeverEndsBeforeItStarts(t *testing.T) {
	if got := atLeast(t0, t0.Add(time.Second)); !got.Equal(t0.Add(time.Second)) {
		t.Fatal(got)
	}
}

type tracetestSpan = spanStub

func names(spans []spanStub) []string {
	var out []string
	for _, s := range spans {
		out = append(out, s.Name)
	}
	return out
}

func find(t *testing.T, spans []spanStub, name string) spanStub {
	t.Helper()
	for _, span := range spans {
		if span.Name == name {
			return span
		}
	}
	t.Fatalf("no span %s in %v", name, names(spans))
	return spanStub{}
}

func findByAttr(t *testing.T, spans []spanStub, name, key, value string) spanStub {
	t.Helper()
	for _, span := range spans {
		if v, found := attr(span.Attributes, key); span.Name == name && found && v.AsString() == value {
			return span
		}
	}
	t.Fatalf("no span %s with %s=%s", name, key, value)
	return spanStub{}
}
