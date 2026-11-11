package jev

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// fixedJudge is a Jev that always chooses the same option.
type fixedJudge struct{ choice string }

func (j fixedJudge) Choose(_ context.Context, q Question) (Verdict, error) {
	return Verdict{Choice: j.choice, Probability: 0.83, Confidence: 0.71, InputTokens: 90, Took: time.Millisecond}, q.Validate()
}

// nodeTools is the node's side of the reserved server in a test: a round
// of questions, then done; it keeps the decisions it was handed.
type nodeTools struct {
	left      int
	decisions []map[string]any
	refuse    bool
}

func (n *nodeTools) call(_ context.Context, tool string, arguments map[string]any) (application.NodeToolAnswer, error) {
	switch domain.ToolName(tool) {
	case domain.WatchNextTool:
		if n.left == 0 {
			return application.NodeToolAnswer{Object: map[string]any{"done": true}}, nil
		}
		n.left--
		return application.NodeToolAnswer{Object: map[string]any{"ask": map[string]any{
			"state": "Question task-" + itoa(n.left) + " from qa-jefe to team dev, asked 6m0s ago; status Pending.", "question": "Should team qa keep waiting for this answer or abandon the question?", "options": []any{"wait", "abandon"},
		}}}, nil
	case domain.WatchDecideTool:
		if n.refuse {
			return application.NodeToolAnswer{Refused: "question task-1 is younger than 5m0s"}, nil
		}
		n.decisions = append(n.decisions, arguments)
		return application.NodeToolAnswer{Object: map[string]any{"status": "Cancelled", "abandoned": true}}, nil
	}
	return application.NodeToolAnswer{}, errors.New("unknown tool " + tool)
}

func watcherRequest(t *testing.T, tools *nodeTools) application.ExecutionRequest {
	t.Helper()
	runtime, err := domain.NewRuntime(&domain.RuntimeSnapshot{Kind: "jev", Jev: &domain.JevProgramSnapshot{
		Server: domain.WatchServer, Start: domain.WatchNextTool.String(), Wait: domain.WatchNextTool.String(),
		Act: domain.JevActSnapshot{Tool: domain.WatchDecideTool.String(), Choice: domain.WatchChoice, Verdict: domain.WatchVerdict},
	}})
	if err != nil {
		t.Fatal(err)
	}
	budgets, _ := domain.NewBudgets(16, 32, 0, time.Minute)
	request := application.ExecutionRequest{
		ExecutionID: "exec-w", TaskID: "task-w", BotID: "qa-vigia", TenantID: "default", CorrelationID: "trace-w",
		Level: domain.AutonomyActWithinPolicy, Runtime: runtime, Budgets: budgets, Timeout: 30 * time.Second,
	}
	if tools != nil {
		request.OnNodeTool = tools.call
	}
	return request
}

// A watcher's program runs over the node's own tools, never an mcp.json:
// each question goes to Jev, each choice and verdict back to the node, and
// the receipt and the trace are those of any Jev run.
func TestAWatcherRunsOverTheNodesOwnTools(t *testing.T) {
	tools := &nodeTools{left: 2}
	dir := t.TempDir()
	executor := NewExecutor(fixedJudge{choice: "abandon"}, MCPConfig{Path: filepath.Join(dir, "absent.json")}, dir)
	report, err := executor.Execute(context.Background(), watcherRequest(t, tools))
	if err != nil || report.Outcome != domain.ExecutionCompleted {
		t.Fatalf("report = %+v %v", report, err)
	}
	if report.Result.Answer != "Jev chose: abandon (0.83); abandon (0.83)" || len(tools.decisions) != 2 {
		t.Errorf("answer %q decisions %+v", report.Result.Answer, tools.decisions)
	}
	verdict, _ := tools.decisions[0][domain.WatchVerdict].(map[string]any)
	if tools.decisions[0][domain.WatchChoice] != "abandon" || verdict["probability"] != 0.83 || verdict["confidence"] != 0.71 {
		t.Errorf("decision = %+v", tools.decisions[0])
	}
	if !strings.Contains(string(report.Receipt), `"decisions":[{`) || report.Usage.Requests != 2 {
		t.Errorf("receipt %s usage %+v", report.Receipt, report.Usage)
	}
	trace, err := os.ReadFile(report.TracePath)
	if err != nil || !strings.Contains(string(trace), `"server":"node:axlr-bots"`) {
		t.Errorf("trace: %v %s", err, trace)
	}
}

// A refusal of the node is the act's tool error: the run goes on to the
// next question and ends after three refusals in a row.
func TestARefusedDecisionIsTheToolsError(t *testing.T) {
	tools := &nodeTools{left: 5, refuse: true}
	dir := t.TempDir()
	executor := NewExecutor(fixedJudge{choice: "wait"}, MCPConfig{}, dir)
	executor.IdleBackoff = time.Millisecond
	report, err := executor.Execute(context.Background(), watcherRequest(t, tools))
	if err != nil || report.Outcome != domain.ExecutionFailed || !strings.Contains(report.FailureReason, "refused 3 times in a row") || !strings.Contains(report.FailureReason, "younger than 5m0s") {
		t.Errorf("report = %+v %v", report, err)
	}
}

// A node that serves no axlr-bots tools cannot run a watcher; an
// unknown tool fails the run.
func TestAWatcherNeedsTheNodesTools(t *testing.T) {
	dir := t.TempDir()
	executor := NewExecutor(fixedJudge{choice: "wait"}, MCPConfig{}, dir)
	if _, err := executor.Execute(context.Background(), watcherRequest(t, nil)); err == nil || !strings.Contains(err.Error(), "serves no axlr-bots tools") {
		t.Errorf("without the node's tools: %v", err)
	}
	client := nodeToolClient{call: (&nodeTools{}).call}
	if _, err := client.callTool(context.Background(), "other", nil); err == nil || isToolError(err) {
		t.Errorf("an unknown tool: %v", err)
	}
	if result, err := client.callTool(context.Background(), domain.WatchNextTool.String(), nil); err != nil || result.Object["done"] != true || result.Text != `{"done":true}` {
		t.Errorf("done = %+v %v", result, err)
	}
	broken := nodeToolClient{call: func(context.Context, string, map[string]any) (application.NodeToolAnswer, error) {
		return application.NodeToolAnswer{Object: map[string]any{"x": func() {}}}, nil
	}}
	if _, err := broken.callTool(context.Background(), "x", nil); err == nil {
		t.Error("an object that is no JSON was taken")
	}
}
