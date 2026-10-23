package axlr

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/underpass-ai/underpass-bots/application"
	"github.com/underpass-ai/underpass-bots/domain"
)

// turnParser folds the JSON lines of one run into a report. The result
// line is the source of truth when present; without it (an axlr-tui
// older than the contract) the exit code and the completed, failed and
// decision.required lines decide.
type turnParser struct {
	sessionID string
	sessions  []string
	answer    strings.Builder
	calls     []domain.ToolCall
	index     map[string]int
	ended     string
	failure   string
	pending   *domain.PendingDecision
	result    *resultObject
}

func newTurnParser() *turnParser {
	return &turnParser{index: map[string]int{}}
}

// feed reads one line; lines that are not JSON events are ignored here
// and kept only in the trace.
func (p *turnParser) feed(line []byte) {
	var event eventLine
	if err := json.Unmarshal(line, &event); err != nil {
		return
	}
	switch event.Type {
	case "session", "session.continued":
		p.session(event.Session)
	case "text":
		p.answer.WriteString(event.Text)
	case "tool.requested":
		p.call(event, "pending")
	case "tool.completed":
		p.call(event, "completed")
	case "decision.required":
		p.ended = event.Type
		p.session(event.Session)
		p.pending = &domain.PendingDecision{Tool: event.Tool, CallID: event.CallID, SessionID: p.sessionID}
	case "completed":
		p.ended = event.Type
		p.session(event.Session)
	case "failed":
		p.ended = event.Type
		p.failure = event.Error
		p.session(event.Session)
	case "result":
		// TODO(runtime-contract): the result object, axlr.execution-result/v1,
		// is the contract's result; keep this parse in step with AXLR's
		// dto.ExecutionResult when the schema moves to v2.
		if event.Result != nil && event.Result.Schema == resultSchema {
			p.result = event.Result
		}
	}
}

func (p *turnParser) session(id string) {
	if id == "" {
		return
	}
	p.sessionID = id
	for _, known := range p.sessions {
		if known == id {
			return
		}
	}
	p.sessions = append(p.sessions, id)
}

func (p *turnParser) call(event eventLine, status string) {
	if i, ok := p.index[event.CallID]; ok && event.CallID != "" {
		p.calls[i].Status = status
		if event.Decision != "" {
			p.calls[i].Decision = event.Decision
		}
		return
	}
	p.index[event.CallID] = len(p.calls)
	p.calls = append(p.calls, domain.ToolCall{Name: event.Tool, CallID: event.CallID, Decision: event.Decision, Status: status})
}

// interrupted reports a run the control plane stopped (timeout,
// cancellation) with whatever it had seen.
func (p *turnParser) interrupted(reason string) application.ExecutionReport {
	return application.ExecutionReport{
		Outcome:       domain.ExecutionFailed,
		Result:        p.partial(),
		Usage:         domain.Usage{ToolCalls: len(p.calls)},
		FailureReason: reason,
	}
}

// report turns the run into a report once the process has exited.
func (p *turnParser) report(exitCode int, stderrLine string) application.ExecutionReport {
	if p.result != nil {
		return p.fromResult(p.result)
	}
	return p.fromEvents(exitCode, stderrLine)
}

func (p *turnParser) fromResult(r *resultObject) application.ExecutionReport {
	result := domain.Result{Answer: r.Answer, SessionID: r.SessionID, Sessions: r.Sessions}
	if result.SessionID == "" {
		result.SessionID = p.sessionID
	}
	if len(result.Sessions) == 0 {
		result.Sessions = p.sessions
	}
	for _, c := range r.ToolCalls {
		result.ToolCalls = append(result.ToolCalls, domain.ToolCall{Name: c.Tool, CallID: c.CallID, Decision: c.Decision, Status: c.Status})
	}
	for _, a := range r.Artifacts {
		result.Artifacts = append(result.Artifacts, domain.Artifact{Name: a.Name, MediaType: a.MediaType, Reference: a.Reference})
	}
	usage := domain.Usage{
		Requests:         r.Usage.Requests,
		PromptTokens:     r.Usage.PromptTokens,
		CachedTokens:     r.Usage.CachedTokens,
		CompletionTokens: r.Usage.CompletionTokens,
		ReasoningTokens:  r.Usage.ReasoningTokens,
		ToolCalls:        len(r.ToolCalls),
		CostUSD:          r.Usage.CostUSD,
	}
	report := application.ExecutionReport{Result: result, Usage: usage}
	switch r.Status {
	case "completed":
		report.Outcome = domain.ExecutionCompleted
	case "needs_decision":
		if r.Pending == nil {
			report.Outcome = domain.ExecutionFailed
			report.FailureReason = "runtime result says needs_decision but names no pending call"
			break
		}
		report.Result.PendingDecision = &domain.PendingDecision{Tool: r.Pending.Tool, CallID: r.Pending.CallID, SessionID: result.SessionID}
		report.Outcome = domain.ExecutionDecisionRequired
	case "failed":
		report.Outcome = domain.ExecutionFailed
		report.FailureReason = orElse(r.Error, "runtime reported failed")
	case "interrupted":
		report.Outcome = domain.ExecutionFailed
		report.FailureReason = "interrupted: " + orElse(r.Error, "the turn stopped before its end")
	default:
		report.Outcome = domain.ExecutionFailed
		report.FailureReason = fmt.Sprintf("runtime result has unknown status %q", r.Status)
	}
	return report
}

func (p *turnParser) fromEvents(exitCode int, stderrLine string) application.ExecutionReport {
	report := application.ExecutionReport{Result: p.partial(), Usage: domain.Usage{ToolCalls: len(p.calls)}}
	switch exitCode {
	case exitCompleted:
		report.Outcome = domain.ExecutionCompleted
	case exitNeedsDecision:
		if p.pending == nil {
			report.Outcome = domain.ExecutionFailed
			report.FailureReason = "runtime exited 3 (decision required) but reported no pending call"
			break
		}
		report.Result.PendingDecision = p.pending
		report.Outcome = domain.ExecutionDecisionRequired
	default:
		report.Outcome = domain.ExecutionFailed
		switch {
		case p.failure != "":
			report.FailureReason = p.failure
		case stderrLine != "":
			report.FailureReason = fmt.Sprintf("exit code %d: %s", exitCode, stderrLine)
		default:
			report.FailureReason = fmt.Sprintf("runtime exited with code %d", exitCode)
		}
	}
	return report
}

func (p *turnParser) partial() domain.Result {
	calls := make([]domain.ToolCall, len(p.calls))
	copy(calls, p.calls)
	return domain.Result{Answer: p.answer.String(), SessionID: p.sessionID, Sessions: p.sessions, ToolCalls: calls}
}

func orElse(value, fallback string) string {
	if value != "" {
		return value
	}
	return fallback
}
