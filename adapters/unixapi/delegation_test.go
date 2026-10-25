package unixapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// delegatingPair registers bot-a, which may ask bot-b, and bot-b, and
// returns the assignment of a task of bot-a to the node.
func delegatingPair(t *testing.T, operator, node *Client, nodeID string) application.Assignment {
	t.Helper()
	ctx := context.Background()
	a := definition("bot-a", "observe")
	a.ID, a.Delegation = "bot-a", &domain.DelegationSnapshot{To: []string{"bot-b"}, MaxDepth: 1}
	a.Budgets.TaskTimeoutSeconds = 300
	b := definition("bot-b", "observe")
	b.ID, b.Triggers = "bot-b", []domain.TriggerSnapshot{{Type: "delegation"}}
	for _, d := range []application.BotDefinition{a, b} {
		if _, err := operator.RegisterBot(ctx, d); err != nil {
			t.Fatal(err)
		}
		if _, err := operator.EnableBot(ctx, d.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := node.Register(ctx, application.NodeRegistration{ID: nodeID, HostName: nodeID, Capabilities: domain.NodeCapabilities{PolicySupported: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := operator.SubmitTask(ctx, SubmitRequest{BotID: "bot-a", Objective: "Ask bot-b", IdempotencyKey: "k-1"}); err != nil {
		t.Fatal(err)
	}
	assignment, found, err := node.NextAssignment(ctx, domain.NodeID(nodeID), time.Second)
	if err != nil || !found {
		t.Fatalf("assignment: %v %v", found, err)
	}
	return assignment
}

func TestDelegationOverTheSocket(t *testing.T) {
	client, _, _ := serve(t, "completed")
	ctx := context.Background()
	assignment := delegatingPair(t, client, client, "web-1")
	if assignment.Run.Delegation == nil || assignment.Run.Delegation.Targets[0] != "bot-b" || assignment.Run.Delegation.MaxDepth != 1 {
		t.Fatalf("the assignment offers no delegation: %+v", assignment.Run)
	}
	answer, err := client.Delegate(ctx, "web-1", assignment.ExecutionID, application.DelegateCommand{Bot: "bot-b", Objective: "Say hello", Wait: true})
	if err != nil || !answer.Created || answer.Depth != 1 || answer.Status != domain.TaskPending || answer.Grant.Deadline.IsZero() || answer.WaitUntil.IsZero() {
		t.Fatalf("delegate: %v %+v %+v", err, answer, answer.Refused)
	}
	refused, err := client.Delegate(ctx, "web-1", assignment.ExecutionID, application.DelegateCommand{Bot: "bot-c", Objective: "Say hello"})
	if err != nil || refused.Refused == nil || refused.Refused.Reason != domain.RefusedNotAllowed {
		t.Errorf("refused: %v %+v", err, refused)
	}
	started := time.Now()
	waited, err := client.AwaitDelegation(ctx, "web-1", assignment.ExecutionID, answer.TaskID, 300*time.Millisecond)
	if err != nil || waited.Done || waited.Status != domain.TaskPending || time.Since(started) < 250*time.Millisecond {
		t.Errorf("await: %v %+v", err, waited)
	}
	child, err := client.GetTask(ctx, answer.TaskID.String())
	if err != nil || child.Delegation == nil || child.Delegation.ParentTaskID != assignment.Run.TaskID.String() {
		t.Errorf("GET /v1/tasks/{child}: %v %+v", err, child.Delegation)
	}
	parent, err := client.GetTask(ctx, assignment.Run.TaskID.String())
	if err != nil || len(parent.Children) != 1 || parent.Children[0] != answer.TaskID.String() {
		t.Errorf("GET /v1/tasks/{parent}: %v %v", err, parent.Children)
	}
	// The child ends: the wait answers at once with its answer.
	next, found, err := client.NextAssignment(ctx, "web-1", time.Second)
	if err != nil || !found || next.Run.TaskID != answer.TaskID {
		t.Fatalf("child assignment: %v %v", found, err)
	}
	if _, err := client.Report(ctx, "web-1", next.ExecutionID, application.ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "hello"}}); err != nil {
		t.Fatal(err)
	}
	done, err := client.AwaitDelegation(ctx, "web-1", assignment.ExecutionID, answer.TaskID, 10*time.Second)
	if err != nil || !done.Done || done.Answer != "hello" {
		t.Errorf("await after the end: %v %+v", err, done)
	}
	// Errors: another node, a bad id, a bad wait, a bad body.
	if _, err := client.Delegate(ctx, "web-2", assignment.ExecutionID, application.DelegateCommand{Bot: "bot-b", Objective: "x"}); err == nil {
		t.Error("another node delegated")
	}
	if _, err := client.AwaitDelegation(ctx, "web-1", assignment.ExecutionID, "Bad Id", 0); err == nil {
		t.Error("a bad child id was read")
	}
	for _, path := range []string{
		"/v1/nodes/web-1/executions/" + assignment.ExecutionID.String() + "/delegations/" + answer.TaskID.String() + "?wait=soon",
		"/v1/nodes/Bad/executions/" + assignment.ExecutionID.String() + "/delegations/" + answer.TaskID.String(),
		"/v1/nodes/web-1/executions/Bad/delegations/" + answer.TaskID.String(),
		"/v1/nodes/web-1/executions/exec-404/delegations/" + answer.TaskID.String(),
	} {
		if err := client.call(ctx, http.MethodGet, path, nil, &map[string]any{}); err == nil {
			t.Errorf("GET %s answered", path)
		}
	}
	if err := client.call(ctx, http.MethodPost, "/v1/nodes/web-1/executions/"+assignment.ExecutionID.String()+"/delegations", map[string]any{"bot": "bot-b", "unknown": 1}, &map[string]any{}); err == nil {
		t.Error("an unknown field was taken")
	}
	if err := client.call(ctx, http.MethodPost, "/v1/nodes/Bad/executions/"+assignment.ExecutionID.String()+"/delegations", map[string]any{"bot": "bot-b"}, &map[string]any{}); err == nil {
		t.Error("a bad node id was taken")
	}
}

func TestDelegationOverTLSIsTheCertificatesNode(t *testing.T) {
	f := serveFleet(t)
	ctx := context.Background()
	_, web1 := f.enroll(t, f.token(t, "web-1", nil))
	_, web2 := f.enroll(t, f.token(t, "web-2", nil))
	assignment := delegatingPair(t, f.operator, web1, "web-1")
	answer, err := web1.Delegate(ctx, "web-1", assignment.ExecutionID, application.DelegateCommand{Bot: "bot-b", Objective: "Say hello"})
	if err != nil || !answer.Created {
		t.Fatalf("over TLS: %v %+v", err, answer)
	}
	if _, err := web2.Register(ctx, application.NodeRegistration{ID: "web-2", HostName: "web-2"}); err != nil {
		t.Fatal(err)
	}
	// web-2's certificate cannot speak for web-1, nor for its execution.
	if _, err := web2.Delegate(ctx, "web-1", assignment.ExecutionID, application.DelegateCommand{Bot: "bot-b", Objective: "x"}); !domain.IsForbidden(err) {
		t.Errorf("web-2 as web-1: %v", err)
	}
	if _, err := web2.AwaitDelegation(ctx, "web-2", assignment.ExecutionID, answer.TaskID, 0); err == nil {
		t.Error("web-2 read web-1's delegation")
	}
}
