package domain

import (
	"slices"
	"strconv"
)

// runtimeReadTools are the runtime's tools a run acting alone may keep
// beside its write: they read the workspace. MCP does not say which of
// a server's tools read, so no other tool of the phase's list stays.
var runtimeReadTools = []ToolName{"local_read", "local_list", "local_search"}

// LadderUnitOf is the runbook and namespace of the incident on the
// trigger's ladder, or the reason it has none: a trigger without a
// ladder, a runbook the ladder does not name, an incident of no
// namespace or of several (the unit is one runbook in one namespace), or
// a namespace outside the runbook's bounds.
func LadderUnitOf(trigger Trigger, incident Incident) (LadderUnit, string, bool) {
	ladder, ok := trigger.Ladder()
	if !ok {
		return LadderUnit{}, "", false
	}
	name, ok := RunbookOf(incident.Source(), incident.Title())
	if !ok {
		return LadderUnit{}, "the incident names no runbook", false
	}
	runbook, ok := ladder.Runbook(name)
	if !ok {
		return LadderUnit{}, "runbook " + name.String() + " is not on the ladder", false
	}
	namespaces := incident.Namespaces()
	if len(namespaces) != 1 {
		return LadderUnit{}, "the incident has " + strconv.Itoa(len(namespaces)) + " namespaces: a runbook counts in one", false
	}
	if !runbook.Covers(namespaces[0]) {
		return LadderUnit{}, "namespace " + namespaces[0] + " is outside the bounds of runbook " + name.String(), false
	}
	return LadderUnit{runbook: runbook, namespace: namespaces[0], ladder: ladder}, "", true
}

// DecideLadder is what the trigger's ladder says of the incident's apply
// now (decisions D13 and D14): a grant only for a runbook at the top step
// with its history earned, inside its bounds, on a bot whose level allows
// it, and never for an incident of a production sev1 or one the bot's
// approvals send to two persons. autonomy is the runbook's record, known
// false when it has none yet.
func (b Bot) DecideLadder(trigger Trigger, incident Incident, autonomy RunbookAutonomy, known bool) LadderDecision {
	if _, ok := trigger.Ladder(); !ok {
		return LadderDecision{}
	}
	unit, reason, ok := LadderUnitOf(trigger, incident)
	if !ok {
		return LadderDecision{Laddered: true, Reason: reason}
	}
	card := func(reason string) LadderDecision { return LadderDecision{Laddered: true, Reason: reason} }
	if !known || !autonomy.MayActAlone(unit.Thresholds()) {
		step := AutonomyActWithApproval
		if known {
			step = autonomy.Step()
		}
		return card("runbook " + unit.Runbook().Name().String() + " stands on " + step.String())
	}
	if b.level != AutonomyActWithinPolicy {
		return card("bot level " + b.level.String() + " does not allow act-within-policy")
	}
	tools, listed := trigger.Tools()
	if !listed || !tools.Allows(unit.Runbook().Tool()) || !b.allowedTools.Allows(unit.Runbook().Tool()) {
		return card("tool " + unit.Runbook().Tool().String() + " is not allowed on the apply phase")
	}
	namespaces := incident.Namespaces()
	if incident.Severity() == Sev1 && slices.ContainsFunc(namespaces, b.approvals.TwoPersons().NamesNamespace) {
		return card("a sev1 in production: the card stays")
	}
	if b.approvals.ApprovalsNeeded(namespaces, incident.Severity()) > 1 {
		return card("the bot's approvals send this incident to two persons")
	}
	return LadderDecision{Laddered: true, Reason: "act-within-policy", Grant: &LadderGrant{Runbook: unit.Runbook().Name(), Tool: unit.Runbook().Tool(), Namespace: unit.Namespace()}}
}

// checkLadder refuses a ladder that could not earn the top step safely:
// off the apply phase's trigger, an apply that does not keep its card
// while it earns, a bot whose level forbids the top step, or a write tool
// the phase's list leaves out or that a host tool would be.
func (b Bot) checkLadder(trigger Trigger) error {
	ladder, ok := trigger.Ladder()
	if !ok {
		return nil
	}
	level, leveled := trigger.Level()
	if !leveled || level != AutonomyActWithApproval {
		return invalid("trigger "+trigger.Name(), "a ladder needs level act-with-approval: a runbook earns the top step from the card")
	}
	if b.level != AutonomyActWithinPolicy {
		return invalid("trigger "+trigger.Name(), "a ladder needs the bot at act-within-policy, the most a phase may reach")
	}
	tools, listed := trigger.Tools()
	if !listed {
		return invalid("trigger "+trigger.Name(), "a ladder needs the phase's tools")
	}
	for _, runbook := range ladder.Runbooks() {
		tool := runbook.Tool()
		if !tools.Allows(tool) {
			return invalid("trigger "+trigger.Name(), "ladder tool "+tool.String()+" is not in the phase's tools")
		}
		if slices.Contains(hostWriteTools, tool) {
			return invalid("trigger "+trigger.Name(), "ladder tool "+tool.String()+" is the runtime's own: a ladder bounds an MCP write")
		}
	}
	return nil
}
