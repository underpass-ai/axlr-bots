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
// ladder, a group known to be mixed, an incident of no namespace or of
// several (the unit is one runbook in one namespace), a runbook the
// ladder does not name, a namespace outside its bounds, or a title that
// is not exactly "<runbook> in <namespace>".
func LadderUnitOf(trigger Trigger, incident Incident) (LadderUnit, string, bool) {
	ladder, ok := trigger.Ladder()
	if !ok {
		return LadderUnit{}, "", false
	}
	if incident.AlertGroup() == AlertGroupMixed {
		return LadderUnit{}, "the alert group is mixed: no one runbook speaks for it", false
	}
	name, ok := RunbookOf(incident.Source(), incident.Title())
	if !ok {
		return LadderUnit{}, "the incident names no runbook", false
	}
	namespaces := incident.Namespaces()
	if len(namespaces) != 1 {
		return LadderUnit{}, "the incident has " + strconv.Itoa(len(namespaces)) + " namespaces: a runbook counts in one", false
	}
	runbook, ok := ladder.Runbook(name)
	if !ok {
		return LadderUnit{}, "runbook " + name.String() + " is not on the ladder", false
	}
	if !runbook.Covers(namespaces[0]) {
		return LadderUnit{}, "namespace " + namespaces[0] + " is outside the bounds of runbook " + name.String(), false
	}
	if !runbook.Titles(incident.Title(), namespaces[0]) {
		return LadderUnit{}, "runbook " + name.String() + " is not on the ladder: the title is not <runbook> in <namespace>", false
	}
	return LadderUnit{runbook: runbook, namespace: namespaces[0], ladder: ladder}, "", true
}

// DecideLadder is what the trigger's ladder says of the incident's apply
// now (decisions D13 and D14, review of #143): a grant only for a runbook
// at the top step with its history earned with this tool and no other
// apply left unknown, of a group known to be one alert, on a bot whose
// level allows it, and never for a sev1 or an incident the bot's
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
	runbook := unit.Runbook()
	pinned := LadderGrant{Runbook: runbook.Name(), Tool: runbook.Tool(), Namespace: unit.Namespace()}
	card := func(reason string) LadderDecision {
		return LadderDecision{Laddered: true, Reason: reason, Unit: &pinned}
	}
	if !known || !autonomy.MayActAlone(unit.Thresholds()) {
		step := AutonomyActWithApproval
		if known {
			step = autonomy.Step()
		}
		return card("runbook " + runbook.Name().String() + " stands on " + step.String())
	}
	if autonomy.Tool() != runbook.Tool() {
		return card("the history of runbook " + runbook.Name().String() + " was earned with " + autonomy.Tool().String())
	}
	if autonomy.HoldsOpen(incident.ID(), incident.Cycle()) {
		return card("an earlier apply of runbook " + runbook.Name().String() + " has no recorded end")
	}
	if incident.AlertGroup() != AlertGroupUniform {
		return card("the alert group is not known to be one alert")
	}
	if b.level != AutonomyActWithinPolicy {
		return card("bot level " + b.level.String() + " does not allow act-within-policy")
	}
	tools, listed := trigger.Tools()
	if !listed || !tools.Allows(runbook.Tool()) || !b.allowedTools.Allows(runbook.Tool()) {
		return card("tool " + runbook.Tool().String() + " is not allowed on the apply phase")
	}
	if incident.Severity() == Sev1 {
		return card("a sev1 never acts alone")
	}
	if b.approvals.ApprovalsNeeded(incident.Namespaces(), incident.Severity()) > 1 {
		return card("the bot's approvals send this incident to two persons")
	}
	granted := pinned
	granted.Alone = true
	return LadderDecision{Laddered: true, Reason: "act-within-policy", Unit: &pinned, Grant: &granted}
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
	if len(b.approvals.Approvers()) == 0 {
		return invalid("trigger "+trigger.Name(), "a ladder needs approvals.approvers: its signals are the acts of named people")
	}
	for _, approver := range b.approvals.Approvers() {
		if IsMachineActor(approver) {
			return invalid("trigger "+trigger.Name(), "approver "+approver+" is no person: a ladder counts people")
		}
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
