package axlr

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// correlationObject is the contract's correlation, as the file carries
// it: {"correlation": {bot_id, task_id, execution_id, trace_id}}.
type correlationObject struct {
	Correlation correlationIDs `json:"correlation"`
}

type correlationIDs struct {
	BotID       string `json:"bot_id"`
	TaskID      string `json:"task_id,omitempty"`
	ExecutionID string `json:"execution_id"`
	TraceID     string `json:"trace_id,omitempty"`
}

// budgetObject is the contract's budget per execution:
// {"budget": {seconds, requests, tool_calls}}; a zero limit is left out.
type budgetObject struct {
	Budget budgetLimits `json:"budget"`
}

type budgetLimits struct {
	Seconds   int `json:"seconds,omitempty"`
	Requests  int `json:"requests,omitempty"`
	ToolCalls int `json:"tool_calls,omitempty"`
}

// executionBundle is what --execution takes when the binary has that
// flag in place of the separate ones: the policy, the budget and the
// correlation of one execution together.
type executionBundle struct {
	Policy      policyObject   `json:"policy"`
	Budget      budgetLimits   `json:"budget"`
	Correlation correlationIDs `json:"correlation"`
}

// preparedCorrelation is what the run passes and records.
type preparedCorrelation struct {
	args   []string
	record domain.CorrelationRecord
}

// prepareCorrelation writes the execution's correlation file
// (<state>/correlation/<execution>.json, kept as evidence whatever the
// binary takes) and decides the flags: --correlation and --budget when
// the binary has them; else the --execution bundle, which then carries
// the policy too and replaces --policy; else nothing, and the record
// says the ids were not carried.
func (e *Executor) prepareCorrelation(ctx context.Context, execution domain.ExecutionID, task domain.TaskID, bot domain.BotID, trace domain.CorrelationID, budgets domain.Budgets, timeout time.Duration, policy *preparedPolicy) (preparedCorrelation, error) {
	ids := correlationIDs{BotID: bot.String(), TaskID: task.String(), ExecutionID: execution.String(), TraceID: trace.String()}
	path, err := e.writeJSON("correlation", execution, correlationObject{Correlation: ids})
	if err != nil {
		return preparedCorrelation{}, fmt.Errorf("axlr: correlation file: %w", err)
	}
	prepared := preparedCorrelation{record: domain.CorrelationRecord{BotID: ids.BotID, TaskID: ids.TaskID, ExecutionID: ids.ExecutionID, TraceID: ids.TraceID, Path: path}}
	limits := budgetLimits{Seconds: int(timeout / time.Second), Requests: budgets.MaxModelCallsPerTask(), ToolCalls: budgets.MaxToolCallsPerTask()}
	f := e.detectFeatures(ctx)
	switch {
	case f.correlation:
		prepared.args = append(prepared.args, "--correlation", path)
		prepared.record.Carried = true
		if f.budget {
			budgetPath, err := e.writeJSON("budgets", execution, budgetObject{Budget: limits})
			if err != nil {
				return preparedCorrelation{}, fmt.Errorf("axlr: budget file: %w", err)
			}
			prepared.args = append(prepared.args, "--budget", budgetPath)
		}
	case f.execution:
		bundlePath, err := e.writeJSON("bundles", execution, executionBundle{Policy: policy.object, Budget: limits, Correlation: ids})
		if err != nil {
			return preparedCorrelation{}, fmt.Errorf("axlr: execution bundle: %w", err)
		}
		prepared.args = append(prepared.args, "--execution", bundlePath)
		prepared.record.Carried = true
		policy.args = nil
	}
	return prepared, nil
}

// writeJSON writes one object under <state>/<dir>/<execution>.json.
func (e *Executor) writeJSON(dir string, execution domain.ExecutionID, object any) (string, error) {
	folder := filepath.Join(e.StateDir, dir)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(object, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(folder, execution.String()+".json")
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

// correlate adds the correlation to a report.
func correlate(report *application.ExecutionReport, prepared preparedCorrelation) {
	record := prepared.record
	report.Correlation = &record
}
