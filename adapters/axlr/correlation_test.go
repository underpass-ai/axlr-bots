package axlr

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func correlatedRequest() application.ExecutionRequest {
	r := request(time.Minute)
	r.TaskID, r.CorrelationID = "task-7", "trace-abc"
	budgets, _ := domain.NewBudgets(20, 60, 0, 0)
	r.Budgets = budgets
	return r
}

func TestCorrelationIsWrittenAndNotCarriedByAnOlderBinary(t *testing.T) {
	e, argsFile := fake(t, "result-completed")
	report, err := e.Execute(context.Background(), correlatedRequest())
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(readArgs(t, argsFile), " ")
	if strings.Contains(args, "--correlation") || strings.Contains(args, "--execution") || strings.Contains(args, "--budget") {
		t.Errorf("flags the binary lacks were passed: %s", args)
	}
	path := filepath.Join(e.StateDir, "correlation", "exec-1.json")
	if report.Correlation == nil || report.Correlation.Carried || report.Correlation.Path != path || report.Correlation.TraceID != "trace-abc" || report.Correlation.TaskID != "task-7" || report.Correlation.ExecutionID != "exec-1" || report.Correlation.BotID != "sii" {
		t.Errorf("correlation = %+v", report.Correlation)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var object correlationObject
	if err := json.Unmarshal(raw, &object); err != nil || object.Correlation.TraceID != "trace-abc" || object.Correlation.BotID != "sii" || object.Correlation.ExecutionID != "exec-1" {
		t.Errorf("file = %s (%v)", raw, err)
	}
	if e.SupportsCorrelation(context.Background()) {
		t.Error("an older binary supports the correlation")
	}
}

func TestCorrelationAndBudgetArePassedWhenTheBinaryTakesThem(t *testing.T) {
	t.Setenv("FAKE_AXLR_CORRELATION", "1")
	e, argsFile := fake(t, "result-completed")
	if !e.SupportsCorrelation(context.Background()) {
		t.Fatal("the binary should support the correlation")
	}
	report, err := e.Execute(context.Background(), correlatedRequest())
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(readArgs(t, argsFile), " ")
	correlationPath := filepath.Join(e.StateDir, "correlation", "exec-1.json")
	budgetPath := filepath.Join(e.StateDir, "budgets", "exec-1.json")
	if !strings.Contains(args, "--correlation "+correlationPath+" ") || !strings.Contains(args, "--budget "+budgetPath+" ") || !strings.Contains(args, "--policy ") {
		t.Errorf("args = %s", args)
	}
	if report.Correlation == nil || !report.Correlation.Carried {
		t.Errorf("correlation = %+v", report.Correlation)
	}
	raw, _ := os.ReadFile(budgetPath)
	var budget budgetObject
	if err := json.Unmarshal(raw, &budget); err != nil || budget.Budget.Seconds != 60 || budget.Budget.Requests != 20 || budget.Budget.ToolCalls != 60 {
		t.Errorf("budget file = %s (%v)", raw, err)
	}
	// A decision carries the ids too.
	decided, err := e.Decide(context.Background(), application.DecisionRequest{ExecutionID: "exec-2", TaskID: "task-7", BotID: "sii", CorrelationID: "trace-abc", SessionID: "sess-1", Decision: "approve", Level: domain.AutonomyObserve})
	if err != nil || decided.Correlation == nil || !decided.Correlation.Carried || decided.Correlation.ExecutionID != "exec-2" {
		t.Errorf("decide: %v %+v", err, decided.Correlation)
	}
	if args := strings.Join(readArgs(t, argsFile), " "); !strings.Contains(args, "--correlation "+filepath.Join(e.StateDir, "correlation", "exec-2.json")) {
		t.Errorf("decide args = %s", args)
	}
}

func TestExecutionBundleReplacesTheSeparateFlags(t *testing.T) {
	t.Setenv("FAKE_AXLR_EXECUTION", "1")
	e, argsFile := fake(t, "result-completed")
	report, err := e.Execute(context.Background(), correlatedRequest())
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(readArgs(t, argsFile), " ")
	bundlePath := filepath.Join(e.StateDir, "bundles", "exec-1.json")
	if !strings.Contains(args, "--execution "+bundlePath+" ") || strings.Contains(args, "--policy") || strings.Contains(args, "--correlation") {
		t.Errorf("args = %s", args)
	}
	if report.Correlation == nil || !report.Correlation.Carried || report.Policy == nil || !report.Policy.Enforced {
		t.Errorf("report = %+v / %+v", report.Correlation, report.Policy)
	}
	raw, _ := os.ReadFile(bundlePath)
	var bundle executionBundle
	if err := json.Unmarshal(raw, &bundle); err != nil || bundle.Correlation.TraceID != "trace-abc" || bundle.Policy.Level == "" || bundle.Budget.Seconds != 60 {
		t.Errorf("bundle = %s (%v)", raw, err)
	}
}

func TestCorrelationFileErrors(t *testing.T) {
	e, _ := fake(t, "result-completed")
	if err := os.MkdirAll(e.StateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.StateDir, "correlation"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Execute(context.Background(), correlatedRequest()); err == nil || !strings.Contains(err.Error(), "correlation file") {
		t.Errorf("blocked correlation dir: %v", err)
	}
	t.Setenv("FAKE_AXLR_CORRELATION", "1")
	e2, _ := fake(t, "result-completed")
	_ = os.MkdirAll(e2.StateDir, 0o755)
	_ = os.WriteFile(filepath.Join(e2.StateDir, "budgets"), []byte("x"), 0o644)
	if _, err := e2.Execute(context.Background(), correlatedRequest()); err == nil || !strings.Contains(err.Error(), "budget file") {
		t.Errorf("blocked budget dir: %v", err)
	}
	t.Setenv("FAKE_AXLR_CORRELATION", "")
	t.Setenv("FAKE_AXLR_EXECUTION", "1")
	e3, _ := fake(t, "result-completed")
	_ = os.MkdirAll(e3.StateDir, 0o755)
	_ = os.WriteFile(filepath.Join(e3.StateDir, "bundles"), []byte("x"), 0o644)
	if _, err := e3.Execute(context.Background(), correlatedRequest()); err == nil || !strings.Contains(err.Error(), "execution bundle") {
		t.Errorf("blocked bundle dir: %v", err)
	}
}

func TestBudgetExceededEndsTheRunFailedWithTheLimit(t *testing.T) {
	e, _ := fake(t, "result-budget-exceeded")
	report, err := e.Execute(context.Background(), correlatedRequest())
	if err != nil {
		t.Fatal(err)
	}
	if report.Outcome != domain.ExecutionFailed || report.FailureReason != "budget exceeded: budget: tool_calls 1 reached" || report.Result.PendingDecision == nil || report.Result.PendingDecision.CallID != "c2" || report.Usage.Requests != 2 {
		t.Errorf("report = %+v", report)
	}
}
