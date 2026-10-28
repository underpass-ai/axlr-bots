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
			ToolRules: r.ToolRules.Snapshot(), TimeoutSeconds: int(r.Timeout / time.Second), Delegation: fromOffer(r.Delegation),
			Events: fromEventsOffer(r.Events),
		}
		if model, tools := r.Budgets.MaxModelCallsPerTask(), r.Budgets.MaxToolCallsPerTask(); model > 0 || tools > 0 {
			m.Run.Budgets = &BudgetsMessage{MaxModelCalls: model, MaxToolCalls: tools}
		}
	}
	if a.Decide != nil {
		d := a.Decide
		m.Decide = &DecideMessage{
			TaskID: d.TaskID.String(), BotID: d.BotID.String(), CorrelationID: d.CorrelationID.String(), SessionID: d.SessionID, CallID: d.CallID, Decision: d.Decision, Level: d.Level.String(),
			AllowedTools: d.AllowedTools, ToolRules: d.ToolRules.Snapshot(), TimeoutSeconds: int(d.Timeout / time.Second), Delegation: fromOffer(d.Delegation),
			Events: fromEventsOffer(d.Events),
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
			Delegation: toOffer(r.Delegation), Events: toEventsOffer(r.Events),
		}
		if r.Budgets != nil {
			budgets, err := domain.NewBudgets(r.Budgets.MaxModelCalls, r.Budgets.MaxToolCalls, 0, a.Run.Timeout)
			if err != nil {
				return application.Assignment{}, err
			}
			a.Run.Budgets = budgets
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
			Delegation: toOffer(d.Delegation), Events: toEventsOffer(d.Events),
		}
		if err := a.Decide.Validate(); err != nil {
			return application.Assignment{}, err
		}
	default:
		return application.Assignment{}, fmt.Errorf("nodeproto: assignment %s of kind %s carries no %s", m.ExecutionID, kind, kind)
	}
	return a, nil
}

func fromOffer(o *application.DelegationOffer) *DelegationOfferMessage {
	if o == nil {
		return nil
	}
	return &DelegationOfferMessage{Targets: append([]string{}, o.Targets...), Depth: o.Depth, MaxDepth: o.MaxDepth}
}

func toOffer(m *DelegationOfferMessage) *application.DelegationOffer {
	if m == nil || len(m.Targets) == 0 {
		return nil
	}
	return &application.DelegationOffer{Targets: append([]string(nil), m.Targets...), Depth: m.Depth, MaxDepth: m.MaxDepth}
}

func fromEventsOffer(o *application.EventsOffer) *EventsOfferMessage {
	if o == nil {
		return nil
	}
	return &EventsOfferMessage{Names: append([]string{}, o.Names...), MaxSeverity: legacyMaxSeverity, Teammates: append([]string{}, o.Teammates...)}
}

func toEventsOffer(m *EventsOfferMessage) *application.EventsOffer {
	if m == nil {
		return nil
	}
	return &application.EventsOffer{Names: append([]string(nil), m.Names...), Teammates: append([]string(nil), m.Teammates...)}
}

// FromEmitted renders an emit's answer as a message.
func FromEmitted(a application.EmitAnswer) EmittedMessage {
	return EmittedMessage{EventID: a.EventID, Name: a.Name, Severity: a.Severity.String(), Deliveries: a.Deliveries, Skipped: a.Skipped, Deduplicated: a.Deduplicated, Refused: a.Refused}
}

// ToEmitted reads an emit's answer back.
func ToEmitted(m EmittedMessage) application.EmitAnswer {
	return application.EmitAnswer{EventID: m.EventID, Name: m.Name, Severity: domain.Severity(m.Severity), Deliveries: m.Deliveries, Skipped: m.Skipped, Deduplicated: m.Deduplicated, Refused: m.Refused}
}

// FromDelegation renders a delegation answer as a message.
func FromDelegation(a application.DelegationAnswer) DelegationMessage {
	m := DelegationMessage{Refused: a.Refused, WaitUntil: a.WaitUntil}
	if a.Refused != nil {
		return m
	}
	grant := a.Grant
	m.TaskID, m.BotID, m.Status, m.Created, m.Depth, m.Grant = a.TaskID.String(), a.BotID.String(), a.Status.String(), a.Created, a.Depth, &grant
	m.Done, m.Answer, m.FailureReason = a.Done, a.Answer, a.FailureReason
	return m
}

// ToDelegation reads a delegation message back.
func ToDelegation(m DelegationMessage) (application.DelegationAnswer, error) {
	a := application.DelegationAnswer{Refused: m.Refused, WaitUntil: m.WaitUntil}
	if m.Refused != nil {
		return a, nil
	}
	task, err := domain.NewTaskID(m.TaskID)
	if err != nil {
		return application.DelegationAnswer{}, err
	}
	bot, err := domain.NewBotID(m.BotID)
	if err != nil {
		return application.DelegationAnswer{}, err
	}
	status, err := domain.ParseTaskStatus(m.Status)
	if err != nil {
		return application.DelegationAnswer{}, err
	}
	a.TaskID, a.BotID, a.Status, a.Created, a.Depth = task, bot, status, m.Created, m.Depth
	if m.Grant != nil {
		a.Grant = *m.Grant
	}
	a.Done, a.Answer, a.FailureReason = m.Done, m.Answer, m.FailureReason
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
