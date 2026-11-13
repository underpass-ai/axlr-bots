package grpcclient

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	operatorv1 "github.com/underpass-ai/axlr-bots/api/gen/axlrbots/operator/v1"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func TestTaskSnapshotMapsEveryField(t *testing.T) {
	h := newHarness(t)
	h.fake.answer(method("TasksService", "GetTask"), &operatorv1.GetTaskResponse{Task: fullTask("t-1")})
	task, err := h.client.GetTask(t.Context(), "t-1")
	if err != nil {
		t.Fatal(err)
	}
	if task.ID != "t-1" || task.BotID != "bot-a" || task.Status != "WaitingApproval" || task.IdempotencyKey != "k" || task.CorrelationID != "corr" ||
		task.Trigger != (domain.TriggerRefSnapshot{Type: "timer", Name: "clock"}) || string(task.Input) != `{"a":1}` || task.ExecutionIDs[0] != "e-1" ||
		task.FailureReason != "boom" || task.Priority != "urgent" || task.Waiting != "no node" || task.Children[0] != "t-2" || task.GrantedModel != 4 || task.GrantedTools != 5 ||
		task.Skills[0].Name != "sk" || !task.CreatedAt.Equal(t0) || !task.UpdatedAt.Equal(t1) {
		t.Errorf("task: %+v", task)
	}
	wait := task.Approval
	if wait == nil || wait.CallID != "c1" || wait.Tool != "local_exec" || !wait.Since.Equal(t0) || !wait.Deadline.Equal(t1) || wait.OnExpire != domain.ExpireDeny || wait.Outcome != "approved" || wait.ClosedAt == nil {
		t.Errorf("approval: %+v", wait)
	}
	lineage := task.Delegation
	if lineage == nil || lineage.ParentTaskID != "t-0" || lineage.ParentExecutionID != "e-0" || lineage.ParentBotID != "bot-p" || lineage.Depth != 1 || lineage.MaxDepth != 3 ||
		lineage.Chain[0] != "bot-p" || lineage.EventID != "ev" || lineage.Grant.MaxModelCalls != 2 || lineage.Grant.MaxToolCalls != 3 || !lineage.Grant.Deadline.Equal(t1) {
		t.Errorf("lineage: %+v", lineage)
	}
	h.fake.answer(method("TasksService", "GetTask"), &operatorv1.GetTaskResponse{Task: &operatorv1.Task{Id: "t-2"}})
	bare, _ := h.client.GetTask(t.Context(), "t-2")
	raw, _ := json.Marshal(bare)
	if string(raw) == "" || bare.Approval != nil || bare.Delegation != nil || bare.Input != nil || bare.ExecutionIDs == nil {
		t.Errorf("a bare task: %s", raw)
	}
}

func TestExecutionSnapshotMapsEveryField(t *testing.T) {
	h := newHarness(t)
	h.fake.answer(method("ExecutionsService", "GetExecution"), &operatorv1.GetExecutionResponse{Execution: fullExecution("e-1")})
	e, err := h.client.GetExecution(t.Context(), "e-1")
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != "e-1" || e.TaskID != "t-1" || e.BotID != "bot-a" || e.Outcome != "Completed" || e.FailureReason != "none" || e.TracePath != "/trace" || e.Kind != "run" ||
		e.Decision != "approve" || e.NodeID != "n1" || e.EndedAt == nil || e.LeaseUntil == nil || string(e.Receipt) != `{"r":1}` {
		t.Errorf("execution: %+v", e)
	}
	if e.Usage != (domain.Usage{Requests: 3, PromptTokens: 100, CachedTokens: 10, CompletionTokens: 20, ReasoningTokens: 5, ToolCalls: 2, CostUSD: 0.25, CostUnknown: true}) {
		t.Errorf("usage: %+v", e.Usage)
	}
	r := e.Result
	if r.Answer != "done" || r.SessionID != "s1" || len(r.Sessions) != 2 || !r.NotStarted || r.Artifacts[0].Name != "r.md" || !r.Artifacts[0].Stored || r.Artifacts[0].CallID != "c1" ||
		r.ToolCalls[0] != (domain.ToolCall{Name: "local_exec", CallID: "c1", Decision: "approve", Status: "ok", Preview: true}) || r.PendingDecision.CallID != "c2" || string(r.PendingDecision.Arguments) != `{"x":1}` {
		t.Errorf("result: %+v", r)
	}
	if e.Policy == nil || e.Policy.Level != "observe" || !e.Policy.Enforced || e.Correlation == nil || e.Correlation.TraceID != "tr" || !e.Correlation.Carried {
		t.Errorf("policy and correlation: %+v %+v", e.Policy, e.Correlation)
	}
	h.fake.answer(method("ExecutionsService", "GetExecution"), &operatorv1.GetExecutionResponse{Execution: &operatorv1.Execution{Id: "e-2"}})
	bare, _ := h.client.GetExecution(t.Context(), "e-2")
	if bare.EndedAt != nil || bare.Policy != nil || bare.Correlation != nil || bare.Receipt != nil || bare.LeaseUntil != nil {
		t.Errorf("a bare execution has no optional parts: %+v", bare)
	}
}

func TestTaskCommandsSendWhatTheCommandAsked(t *testing.T) {
	h := newHarness(t)
	task := fullTask("t-1")
	h.fake.answer(method("TasksService", "SubmitTask"), &operatorv1.SubmitTaskResponse{Created: true, Task: task})
	h.fake.answer(method("TasksService", "RunTask"), &operatorv1.RunTaskResponse{Task: task, Execution: fullExecution("e-1")})
	h.fake.answer(method("TasksService", "CancelTask"), &operatorv1.CancelTaskResponse{Task: task, Changed: true})
	h.fake.answer(method("TasksService", "SendTaskMessage"), &operatorv1.SendTaskMessageResponse{TaskId: "t-1", ExecutionId: "e-1", CreatedAt: ts(t1)})
	h.fake.answer(method("TasksService", "ListTasks"), &operatorv1.ListTasksResponse{Tasks: []*operatorv1.Task{task}})
	h.fake.answer(method("TasksService", "ListTaskEvents"), &operatorv1.ListTaskEventsResponse{Events: []*operatorv1.Event{fullEvent("ev", "bot.task.submitted")}})
	h.fake.answer(method("TasksService", "ListTaskExecutions"), &operatorv1.ListTaskExecutionsResponse{Executions: []*operatorv1.Execution{fullExecution("e-1")}})
	h.fake.answer(method("TasksService", "GetTaskReport"), &operatorv1.GetTaskReportResponse{TaskId: "t-1", Reference: "/r", Content: "pong"})
	ctx := t.Context()
	submitted, err := h.client.SubmitTask(ctx, unixapi.SubmitRequest{BotID: "bot-a", Objective: "o", IdempotencyKey: "k1", CorrelationID: "c", Priority: "urgent"})
	if err != nil || !submitted.Created || submitted.Task.ID != "t-1" {
		t.Fatalf("submit: %+v %v", submitted, err)
	}
	if r := h.fake.request(method("TasksService", "SubmitTask")).(*operatorv1.SubmitTaskRequest); r.GetBotId() != "bot-a" || r.GetObjective() != "o" || r.GetIdempotencyKey() != "k1" || r.GetCorrelationId() != "c" || r.GetPriority() != "urgent" {
		t.Errorf("submit request: %v", r)
	}
	if run, err := h.client.RunTask(ctx, "t-1"); err != nil || run.Execution.ID != "e-1" {
		t.Errorf("run: %+v %v", run, err)
	}
	if cancel, err := h.client.CancelTask(ctx, "t-1"); err != nil || !cancel.Changed || cancel.Execution != nil {
		t.Errorf("cancel: %+v %v", cancel, err)
	}
	h.fake.answer(method("TasksService", "CancelTask"), &operatorv1.CancelTaskResponse{Task: task, Execution: fullExecution("e-1")})
	if cancel, _ := h.client.CancelTask(ctx, "t-1"); cancel.Execution == nil || cancel.Execution.ID != "e-1" {
		t.Errorf("cancel with execution: %+v", cancel)
	}
	sent, err := h.client.SendTaskMessage(ctx, "t-1", unixapi.MessageRequest{Text: "hi"})
	if err != nil || sent.TaskID != "t-1" || sent.ExecutionID != "e-1" || !sent.CreatedAt.Equal(t1) {
		t.Errorf("message: %+v %v", sent, err)
	}
	if r := h.fake.request(method("TasksService", "SendTaskMessage")).(*operatorv1.SendTaskMessageRequest); r.GetText() != "hi" || r.GetTaskId() != "t-1" {
		t.Errorf("message request: %v", r)
	}
	since := t0
	if tasks, err := h.client.ListTasks(ctx, application.TaskQuery{BotID: "bot-a", Status: "Pending", Since: since, Limit: 7}); err != nil || len(tasks) != 1 {
		t.Errorf("list: %v %v", tasks, err)
	}
	if r := h.fake.request(method("TasksService", "ListTasks")).(*operatorv1.ListTasksRequest); r.GetBotId() != "bot-a" || r.GetStatus() != "Pending" || !r.GetSince().AsTime().Equal(since) || r.GetLimit() != 7 {
		t.Errorf("list request: %v", r)
	}
	if _, err := h.client.ListTasks(ctx, application.TaskQuery{}); err != nil {
		t.Fatal(err)
	}
	if r := h.fake.request(method("TasksService", "ListTasks")).(*operatorv1.ListTasksRequest); r.GetSince() != nil {
		t.Errorf("no since is no bound: %v", r)
	}
	if events, err := h.client.ListTaskEvents(ctx, application.TaskEventsQuery{ID: "t-1", After: "ev0", Limit: 3}); err != nil || len(events) != 1 {
		t.Errorf("events: %v %v", events, err)
	}
	if r := h.fake.request(method("TasksService", "ListTaskEvents")).(*operatorv1.ListTaskEventsRequest); r.GetTaskId() != "t-1" || r.GetAfter() != "ev0" || r.GetLimit() != 3 {
		t.Errorf("events request: %v", r)
	}
	if executions, err := h.client.ListExecutions(ctx, "t-1"); err != nil || len(executions) != 1 {
		t.Errorf("executions: %v %v", executions, err)
	}
	if report, err := h.client.GetReport(ctx, "t-1"); err != nil || report.Content != "pong" || report.Reference != "/r" || report.TaskID != "t-1" {
		t.Errorf("report: %+v %v", report, err)
	}
}

func TestTheActorIsSentWithTheDecisionAndAgreesWithIt(t *testing.T) {
	h := newHarness(t)
	h.fake.answer(method("TasksService", "DecideTask"), &operatorv1.DecideTaskResponse{Task: fullTask("t-1"), Execution: fullExecution("e-1")})
	run, err := h.client.DecideTask(t.Context(), "t-1", unixapi.DecideRequest{Decision: "approve", CallID: "c1", DecidedBy: "cli:ana"})
	if err != nil || run.Execution.ID != "e-1" {
		t.Fatalf("decide: %+v %v", run, err)
	}
	request := h.fake.request(method("TasksService", "DecideTask")).(*operatorv1.DecideTaskRequest)
	if request.GetDecision() != "approve" || request.GetCallId() != "c1" || request.GetDecidedBy() != "cli:ana" {
		t.Errorf("request: %v", request)
	}
	headers := h.fake.headers(method("TasksService", "DecideTask"))
	if got := headers.Get(ActorKey); len(got) != 1 || got[0] != "cli:ana" {
		t.Errorf("actor metadata: %v", headers)
	}
	if got := headers.Get("authorization"); len(got) != 1 || got[0] != "Bearer "+testToken {
		t.Errorf("authorization metadata: %v", headers)
	}
	if _, err := h.client.DecideTask(t.Context(), "t-1", unixapi.DecideRequest{Decision: "deny"}); err != nil {
		t.Fatal(err)
	}
	if got := h.fake.headers(method("TasksService", "DecideTask")).Get(ActorKey); len(got) != 0 {
		t.Errorf("no actor, no metadata: %v", got)
	}
}

type recordingFirer struct{ got unixapi.FireRequest }

func (r *recordingFirer) FireTrigger(_ context.Context, request unixapi.FireRequest) (unixapi.FireResponse, error) {
	r.got = request
	return unixapi.FireResponse{Created: true}, nil
}

func TestFiringATriggerNeedsTheHTTPRouteAndSaysSo(t *testing.T) {
	h := newHarness(t)
	_, err := h.client.FireTrigger(t.Context(), unixapi.FireRequest{BotID: "b", Trigger: "hook"})
	var v domain.ValidationError
	if !asValidation(err, &v) || v.Field != "trigger" {
		t.Fatalf("without a firer: %v", err)
	}
	firer := &recordingFirer{}
	fired, err := h.client.WithTriggerFirer(firer).FireTrigger(t.Context(), unixapi.FireRequest{BotID: "b", Trigger: "hook", DeliveryID: "d"})
	if err != nil || !fired.Created || firer.got.DeliveryID != "d" {
		t.Errorf("with a firer: %+v %v %+v", fired, err, firer.got)
	}
}

func TestNodesEnrollmentAndRevocation(t *testing.T) {
	h := newHarness(t)
	h.fake.answer(method("NodesService", "ListNodes"), &operatorv1.ListNodesResponse{Nodes: []*operatorv1.Node{fullNode()}})
	h.fake.answer(method("NodesService", "GetNode"), &operatorv1.GetNodeResponse{Node: fullNode()})
	h.fake.answer(method("NodesService", "CreateEnrollment"), &operatorv1.CreateEnrollmentResponse{Token: "tok", Id: "en-1", NodeId: "n1", Labels: map[string]string{"a": "1"}, ExpiresAt: ts(t1)})
	h.fake.answer(method("NodesService", "RevokeNode"), &operatorv1.RevokeNodeResponse{Node: fullNode(), FailedTasks: []string{"t-1"}})
	nodes, err := h.client.ListNodes(t.Context())
	if err != nil || len(nodes) != 1 {
		t.Fatal(nodes, err)
	}
	n := nodes[0]
	if n.ID != "n1" || n.HostName != "host" || n.Labels["a"] != "1" || n.OperatorLabels["a"] != "1" || n.ReportedLabels["b"] != "2" || n.Status != "Online" ||
		n.CertificateSerial != "01" || n.CertificateNotAfter == nil || n.RevokedAt == nil || !n.RegisteredAt.Equal(t0) || !n.LastSeenAt.Equal(t1) {
		t.Errorf("node: %+v", n)
	}
	c := n.Capabilities
	if c.AXLRVersion != "0.20" || c.NodeVersion != "v" || c.NodeRevision != "r" || !c.PolicySupported || !c.CorrelationSupported || !c.Dedicated || c.Runtimes[0] != "axlr" || c.Providers[0] != "p" || !c.SkillsSupported {
		t.Errorf("capabilities: %+v", c)
	}
	if node, err := h.client.GetNode(t.Context(), "n1"); err != nil || node.ID != "n1" {
		t.Errorf("get: %+v %v", node, err)
	}
	token, err := h.client.CreateEnrollmentToken(t.Context(), unixapi.EnrollTokenRequest{NodeID: "n1", Labels: map[string]string{"a": "1"}, TTL: "1h"})
	if err != nil || token.Token != "tok" || token.ID != "en-1" || token.NodeID != "n1" || !token.ExpiresAt.Equal(t1) {
		t.Errorf("token: %+v %v", token, err)
	}
	if r := h.fake.request(method("NodesService", "CreateEnrollment")).(*operatorv1.CreateEnrollmentRequest); r.GetNodeId() != "n1" || r.GetTtl().AsDuration() != time.Hour || r.GetLabels()["a"] != "1" {
		t.Errorf("enrollment request: %v", r)
	}
	if _, err := h.client.CreateEnrollmentToken(t.Context(), unixapi.EnrollTokenRequest{}); err != nil {
		t.Fatal(err)
	}
	if r := h.fake.request(method("NodesService", "CreateEnrollment")).(*operatorv1.CreateEnrollmentRequest); r.GetTtl() != nil {
		t.Errorf("no ttl is the default: %v", r)
	}
	revoked, err := h.client.RevokeNode(t.Context(), "n1")
	if err != nil || revoked.Node.ID != "n1" || revoked.FailedTasks[0] != "t-1" {
		t.Errorf("revoke: %+v %v", revoked, err)
	}
}

func TestOverviewAndGroups(t *testing.T) {
	h := newHarness(t)
	h.fake.answer(method("OverviewService", "GetOverview"), &operatorv1.GetOverviewResponse{
		Since: ts(t0), Nodes: map[string]int32{"Online": 1}, Bots: map[string]int32{"Enabled": 2}, Tasks: map[string]int32{"Pending": 3},
		TasksSince: map[string]int32{"Completed": 4}, ExecutionsSince: 5, CostUsdSince: 1.5, CostUnknownSince: 2,
	})
	overview, err := h.client.GetOverview(t.Context(), t0)
	if err != nil || !overview.Since.Equal(t0) || overview.Nodes["Online"] != 1 || overview.Bots["Enabled"] != 2 || overview.Tasks["Pending"] != 3 || overview.TasksSince["Completed"] != 4 ||
		overview.ExecutionsSince != 5 || overview.CostUSDSince != 1.5 || overview.CostUnknownSince != 2 {
		t.Errorf("overview: %+v %v", overview, err)
	}
	if r := h.fake.request(method("OverviewService", "GetOverview")).(*operatorv1.GetOverviewRequest); !r.GetSince().AsTime().Equal(t0) {
		t.Errorf("since: %v", r)
	}
	h.fake.answer(method("OverviewService", "GetOverview"), &operatorv1.GetOverviewResponse{})
	if bare, _ := h.client.GetOverview(t.Context(), time.Time{}); bare.Nodes == nil || bare.TasksSince == nil {
		t.Errorf("no counts are {}: %+v", bare)
	}
	if r := h.fake.request(method("OverviewService", "GetOverview")).(*operatorv1.GetOverviewRequest); r.GetSince() != nil {
		t.Errorf("zero since is unset: %v", r)
	}

	group := &operatorv1.Group{TenantId: "default", Name: "team", Members: []*operatorv1.GroupMember{{
		BotId: "bot-a", Name: "A", Status: "Enabled", Purpose: "p", Tools: []string{"t"}, Where: "anywhere", Placement: &operatorv1.Placement{Nodes: []string{"n"}}, Groups: []string{"team"}, TakesDelegation: true,
	}}}
	h.fake.answer(method("GroupsService", "ListGroups"), &operatorv1.ListGroupsResponse{Groups: []*operatorv1.Group{group}})
	h.fake.answer(method("GroupsService", "GetGroup"), &operatorv1.GetGroupResponse{Group: group})
	h.fake.answer(method("GroupsService", "ListGroupTopics"), &operatorv1.ListGroupTopicsResponse{Topics: []*operatorv1.GroupTopic{{
		Name: "stop", Publishers: []string{"bot-a"}, DeclaredPublishers: []string{"bot-b"}, LastAt: ts(t1), Count: 2, NobodyListens: true,
		Listeners: []*operatorv1.TopicListener{{BotId: "bot-b", Topic: "stop", Mode: "task", Status: "active"}},
	}}})
	h.fake.answer(method("GroupsService", "ListGroupEvents"), &operatorv1.ListGroupEventsResponse{Events: []*operatorv1.Event{fullEvent("g1", "bot.group.event")}})
	h.fake.answer(method("GroupsService", "PublishGroupEvent"), &operatorv1.PublishGroupEventResponse{
		EventId: "evt-1", Name: "stop.all", Severity: "critical", Created: true,
		Deliveries: []*operatorv1.EventDelivery{{BotId: "bot-a", Mode: "task", TaskId: "t-1", MessageId: "m", Trigger: "event", Created: true, Urgent: true, Degraded: "intake_rate", CoalescedInto: "t-0"}},
		Skipped:    []*operatorv1.EventSkip{{BotId: "bot-c", Reason: "not_member", Message: "no"}},
	})
	groups, err := h.client.ListGroups(t.Context())
	if err != nil || len(groups) != 1 || groups[0].Members[0].BotID != "bot-a" || !groups[0].Members[0].TakesDelegation || groups[0].Members[0].Placement.Nodes[0] != "n" || groups[0].Members[0].Where != "anywhere" {
		t.Errorf("groups: %+v %v", groups, err)
	}
	if got, err := h.client.GetGroup(t.Context(), "team", "default"); err != nil || got.Name != "team" || got.TenantID != "default" {
		t.Errorf("group: %+v %v", got, err)
	}
	if r := h.fake.request(method("GroupsService", "GetGroup")).(*operatorv1.GetGroupRequest); r.GetName() != "team" || r.GetTenant() != "default" {
		t.Errorf("group request: %v", r)
	}
	topics, err := h.client.ListGroupTopics(t.Context(), "team", "")
	if err != nil || len(topics) != 1 || topics[0].Count != 2 || !topics[0].NobodyListens || topics[0].LastAt == nil || topics[0].Declared[0] != "bot-b" || topics[0].Listeners[0].Mode != "task" {
		t.Errorf("topics: %+v %v", topics, err)
	}
	events, err := h.client.ListGroupEvents(t.Context(), application.GroupEventsQuery{Name: "team", Tenant: "default", After: "g0", Limit: 4})
	if err != nil || len(events) != 1 {
		t.Fatal(events, err)
	}
	if r := h.fake.request(method("GroupsService", "ListGroupEvents")).(*operatorv1.ListGroupEventsRequest); r.GetName() != "team" || r.GetAfter() != "g0" || r.GetLimit() != 4 || r.GetTenant() != "default" {
		t.Errorf("group events request: %v", r)
	}
	answer, err := h.client.PublishGroupEvent(t.Context(), application.PublishGroupEventCommand{Group: "team", Tenant: "default", Name: "stop.all", Message: "stop", Data: map[string]any{"why": "x"}, To: []string{"bot-a"}, IdempotencyKey: "k"})
	if err != nil || answer.EventID != "evt-1" || answer.Severity != "critical" || !answer.Created || answer.Deliveries[0].CoalescedInto != "t-0" || !answer.Deliveries[0].Urgent || answer.Skipped[0].Reason != "not_member" {
		t.Errorf("publish: %+v %v", answer, err)
	}
	sent := h.fake.request(method("GroupsService", "PublishGroupEvent")).(*operatorv1.PublishGroupEventRequest)
	if sent.GetName() != "team" || sent.GetEventName() != "stop.all" || sent.GetData().AsMap()["why"] != "x" || sent.GetTo()[0] != "bot-a" || sent.GetIdempotencyKey() != "k" {
		t.Errorf("publish request: %v", sent)
	}
	if _, err := h.client.PublishGroupEvent(t.Context(), application.PublishGroupEventCommand{Group: "team", Data: map[string]any{"bad": make(chan int)}}); !isValidation(err, "data") {
		t.Errorf("data that is no JSON object: %v", err)
	}
}
