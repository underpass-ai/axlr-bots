package nodeagent

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// stateTransport answers one question's state.
type stateTransport struct {
	fakeTransport
	view  application.QuestionView
	err   error
	asked domain.TaskID
}

func (s *stateTransport) QuestionState(_ context.Context, _ domain.NodeID, _ domain.ExecutionID, question domain.TaskID) (application.QuestionView, error) {
	s.asked = question
	return s.view, s.err
}

// The model reads where its question stands, in words; a bad id or a
// question the control plane will not show is the tool's error.
func TestTheModelReadsWhereItsQuestionStands(t *testing.T) {
	at := time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)
	transport := &stateTransport{view: application.QuestionView{Question: "task-9", Team: "dev", AskedBy: "qa-jefe", Responder: "dev-a", Status: domain.TaskPending, AskedAt: at, Deadline: at.Add(2 * time.Hour), Now: at.Add(42 * time.Minute), Waiting: "its responder is busy with 1 other tasks"}}
	reader := questionReader{transport: transport, node: "qa-node", execution: "exec-1", log: log.New(&syncBuffer{}, "", 0)}
	result := reader.call(context.Background(), json.RawMessage(`{"question":"task-9"}`))
	text := result.Content[0].Text
	if result.IsError || transport.asked != "task-9" || !strings.Contains(text, `"status":"Pending"`) || !strings.Contains(text, "busy with 1 other tasks") || !strings.Contains(text, "asked 42m0s ago") {
		t.Errorf("result = %+v", result)
	}
	for _, raw := range []string{`{}`, `{"question":"Bad Id"}`, `nope`} {
		if bad := reader.call(context.Background(), json.RawMessage(raw)); !bad.IsError {
			t.Errorf("%s: %+v", raw, bad)
		}
	}
	transport.err = errors.New("question task-9 not found")
	if missing := reader.call(context.Background(), json.RawMessage(`{"question":"task-9"}`)); !missing.IsError || !strings.Contains(missing.Content[0].Text, "not found") {
		t.Errorf("missing = %+v", missing)
	}
	tool := questionStatusTool()
	if tool.Name != application.QuestionStatusToolName || !strings.Contains(tool.Description, "only reads") {
		t.Errorf("tool = %+v", tool)
	}
}
