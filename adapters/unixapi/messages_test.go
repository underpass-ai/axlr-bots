package unixapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// remoteKind is the kind of the error the daemon answered, or "".
func remoteKind(err error) string {
	var remote application.RemoteError
	if errors.As(err, &remote) {
		return remote.Kind
	}
	if err == nil {
		return ""
	}
	return application.ClassifyError(err).Kind
}

// compact is the JSON without the whitespace it was stored with.
func compact(raw []byte) string {
	var out bytes.Buffer
	if err := json.Compact(&out, raw); err != nil {
		return string(raw)
	}
	return out.String()
}

func TestOperatorMessagesReachTheNodeRunningTheTask(t *testing.T) {
	f := serveFleet(t)
	ctx := context.Background()
	if _, err := f.local.RegisterBot(ctx, definition("bot-a", "observe")); err != nil {
		t.Fatal(err)
	}
	if _, err := f.local.EnableBot(ctx, "bot-a"); err != nil {
		t.Fatal(err)
	}
	submitted, err := f.local.SubmitTask(ctx, application.SubmitRequest{BotID: "bot-a", Objective: "List the files in your workspace", IdempotencyKey: "k-1"})
	if err != nil {
		t.Fatal(err)
	}
	taskID := submitted.Task.ID
	// No node took the task yet: it has no run to read a message.
	if task, _ := f.local.GetTask(ctx, taskID); task.Status != string(domain.TaskPending) {
		t.Fatalf("task = %+v", task)
	}
	if _, err := f.local.SendTaskMessage(ctx, taskID, application.MessageRequest{Text: "check row F"}); remoteKind(err) != application.KindState {
		t.Errorf("pending task: %v", err)
	}
	_, web := f.enroll(t, f.token(t, "web-1", nil))
	if _, err := web.Register(ctx, application.NodeRegistration{HostName: "web-1.example"}); err != nil {
		t.Fatal(err)
	}
	assignment, found, err := web.NextAssignment(ctx, "web-1", 0)
	if err != nil || !found || assignment.Run == nil || assignment.Run.TaskID != domain.TaskID(taskID) {
		t.Fatalf("assignment: %v %v %+v", err, found, assignment)
	}
	_, db := f.enroll(t, f.token(t, "db-1", nil))
	if _, err := db.Register(ctx, application.NodeRegistration{HostName: "db-1.example"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.local.SendTaskMessage(ctx, taskID, application.MessageRequest{Text: "  "}); remoteKind(err) != application.KindValidation {
		t.Errorf("empty text: %v", err)
	}
	if _, err := f.local.SendTaskMessage(ctx, "task-missing", application.MessageRequest{Text: "hello"}); !domain.IsNotFound(err) {
		t.Errorf("unknown task: %v", err)
	}
	sent, err := f.local.SendTaskMessage(ctx, taskID, application.MessageRequest{Text: "check row F"})
	if err != nil || sent.ExecutionID != assignment.ExecutionID || sent.TaskID != domain.TaskID(taskID) || sent.CreatedAt.IsZero() {
		t.Fatalf("send: %v %+v", err, sent)
	}
	// The node reads the message once: after it, nothing is left.
	messages, err := web.ExecutionMessages(ctx, "web-1", assignment.ExecutionID, "")
	if err != nil || len(messages) != 1 || messages[0].Text != "check row F" || messages[0].ID == "" {
		t.Fatalf("messages: %v %+v", err, messages)
	}
	if after, err := web.ExecutionMessages(ctx, "web-1", assignment.ExecutionID, messages[0].ID); err != nil || len(after) != 0 {
		t.Errorf("after the cursor: %v %+v", err, after)
	}
	// Another node reads neither under the first one's name nor its own.
	if _, err := db.ExecutionMessages(ctx, "web-1", assignment.ExecutionID, ""); !domain.IsForbidden(err) {
		t.Errorf("another node's path: %v", err)
	}
	if _, err := db.ExecutionMessages(ctx, "db-1", assignment.ExecutionID, ""); remoteKind(err) != application.KindState {
		t.Errorf("another node's execution: %v", err)
	}
	report := application.ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "3 files", SessionID: "sess-1"}, Usage: domain.Usage{Requests: 1}}
	if ack, err := web.Report(ctx, "web-1", assignment.ExecutionID, report); err != nil || !ack.Accepted {
		t.Fatalf("report: %v %+v", err, ack)
	}
	// An ended task takes no message, and its run has none to read.
	if _, err := f.local.SendTaskMessage(ctx, taskID, application.MessageRequest{Text: "too late"}); remoteKind(err) != application.KindState {
		t.Errorf("ended task: %v", err)
	}
	if ended, err := web.ExecutionMessages(ctx, "web-1", assignment.ExecutionID, ""); err != nil || len(ended) != 0 {
		t.Errorf("ended execution: %v %+v", err, ended)
	}
}
