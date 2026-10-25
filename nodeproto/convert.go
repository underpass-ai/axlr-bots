package nodeproto

import (
	"fmt"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// FromAssignment renders an assignment as a message.
func FromAssignment(a application.Assignment) AssignmentMessage {
	m := AssignmentMessage{ExecutionID: a.ExecutionID.String(), Kind: a.Kind.String(), LeaseUntil: a.LeaseUntil}
	if a.Run != nil {
		r := a.Run
		m.Run = &RunMessage{
			TaskID: r.TaskID.String(), BotID: r.BotID.String(), TenantID: r.TenantID.String(), CorrelationID: r.CorrelationID.String(),
			Objective: r.PromptText(), Model: r.Model, Level: r.Level.String(), AllowedTools: r.AllowedTools,
			ToolRules: r.ToolRules.Snapshot(), TimeoutSeconds: int(r.Timeout / time.Second),
		}
	}
	if a.Decide != nil {
		d := a.Decide
		m.Decide = &DecideMessage{
			TaskID: d.TaskID.String(), BotID: d.BotID.String(), CorrelationID: d.CorrelationID.String(), SessionID: d.SessionID, CallID: d.CallID, Decision: d.Decision, Level: d.Level.String(),
			AllowedTools: d.AllowedTools, ToolRules: d.ToolRules.Snapshot(), TimeoutSeconds: int(d.Timeout / time.Second),
		}
	}
	return m
}

// ToAssignment reads a message back into an assignment the node's
// executor can run; every value is validated as the domain does.
func ToAssignment(m AssignmentMessage) (application.Assignment, error) {
	executionID, err := domain.NewExecutionID(m.ExecutionID)
	if err != nil {
		return application.Assignment{}, err
	}
	kind, err := domain.ParseExecutionKind(m.Kind)
	if err != nil {
		return application.Assignment{}, err
	}
	a := application.Assignment{ExecutionID: executionID, Kind: kind, LeaseUntil: m.LeaseUntil}
	switch {
	case kind == domain.ExecutionRun && m.Run != nil:
		r := m.Run
		botID, err := domain.NewBotID(r.BotID)
		if err != nil {
			return application.Assignment{}, err
		}
		objective, err := domain.NewObjective(r.Objective)
		if err != nil {
			return application.Assignment{}, err
		}
		level, tools, rules, err := policyOf(r.Level, r.AllowedTools, r.ToolRules)
		if err != nil {
			return application.Assignment{}, err
		}
		a.Run = &application.ExecutionRequest{
			ExecutionID: executionID, TaskID: domain.TaskID(r.TaskID), BotID: botID, TenantID: domain.TenantID(r.TenantID), CorrelationID: domain.CorrelationID(r.CorrelationID),
			Objective: objective, Model: r.Model, Level: level, AllowedTools: tools.Names(), ToolRules: rules, Timeout: time.Duration(r.TimeoutSeconds) * time.Second,
		}
	case kind == domain.ExecutionDecide && m.Decide != nil:
		d := m.Decide
		botID, err := domain.NewBotID(d.BotID)
		if err != nil {
			return application.Assignment{}, err
		}
		level, tools, rules, err := policyOf(d.Level, d.AllowedTools, d.ToolRules)
		if err != nil {
			return application.Assignment{}, err
		}
		a.Decide = &application.DecisionRequest{
			ExecutionID: executionID, TaskID: domain.TaskID(d.TaskID), BotID: botID, CorrelationID: domain.CorrelationID(d.CorrelationID), SessionID: d.SessionID, CallID: d.CallID, Decision: d.Decision,
			Level: level, AllowedTools: tools.Names(), ToolRules: rules, Timeout: time.Duration(d.TimeoutSeconds) * time.Second,
		}
		if err := a.Decide.Validate(); err != nil {
			return application.Assignment{}, err
		}
	default:
		return application.Assignment{}, fmt.Errorf("nodeproto: assignment %s of kind %s carries no %s", m.ExecutionID, kind, kind)
	}
	return a, nil
}

func policyOf(level string, allowed []string, rules domain.ToolRulesSnapshot) (domain.AutonomyLevel, domain.AllowedTools, domain.ToolRules, error) {
	parsed, err := domain.ParseAutonomyLevel(level)
	if err != nil {
		return "", domain.AllowedTools{}, domain.ToolRules{}, err
	}
	tools, err := domain.NewAllowedTools(allowed)
	if err != nil {
		return "", domain.AllowedTools{}, domain.ToolRules{}, err
	}
	toolRules, err := domain.RestoreToolRules(rules, tools)
	if err != nil {
		return "", domain.AllowedTools{}, domain.ToolRules{}, err
	}
	return parsed, tools, toolRules, nil
}

// FromReport renders a report as a message.
func FromReport(r application.ExecutionReport) ReportMessage {
	return ReportMessage{Outcome: r.Outcome.String(), Result: r.Result, Usage: r.Usage, FailureReason: r.FailureReason, TraceReference: r.TracePath, Policy: r.Policy, Correlation: r.Correlation, Receipt: r.Receipt}
}

// ToReport reads a report message back.
func ToReport(m ReportMessage) (application.ExecutionReport, error) {
	outcome, err := domain.ParseExecutionOutcome(m.Outcome)
	if err != nil {
		return application.ExecutionReport{}, err
	}
	if !outcome.IsFinal() {
		return application.ExecutionReport{}, domain.ValidationError{Field: "outcome", Reason: "a report ends the execution: " + m.Outcome + " does not"}
	}
	if err := m.Usage.Validate(); err != nil {
		return application.ExecutionReport{}, err
	}
	return application.ExecutionReport{Outcome: outcome, Result: m.Result, Usage: m.Usage, FailureReason: m.FailureReason, TracePath: m.TraceReference, Policy: m.Policy, Correlation: m.Correlation, Receipt: m.Receipt}, nil
}
