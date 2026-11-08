package domain

import (
	"slices"
	"strconv"
	"time"
)

// RunbookAutonomy is how much one runbook in one namespace has earned
// (axlr-sre design §8.3): the step it stands on, its counters and the
// history of the signals behind them, independent of any incident. Every
// signal is idempotent per incident, cycle and signal, so a cycle never
// counts twice and each reopened cycle of one incident counts as its own
// (decision D13). A promotion reads what was earned since the last
// lowering: a total at the threshold would otherwise give the step back
// on the next signal.
type RunbookAutonomy struct {
	id        AutonomyID
	tenant    TenantID
	bot       BotID
	runbook   RunbookName
	namespace string
	tool      ToolName
	step      AutonomyLevel
	start     AutonomyLevel
	totals    AutonomyCounters
	earned    AutonomyCounters
	history   []AutonomyEntry
	open      []AutonomyOutstanding
	createdAt time.Time
	updatedAt time.Time
}

// NewRunbookAutonomy starts a runbook at the step its apply trigger
// declares, with nothing earned.
func NewRunbookAutonomy(tenant TenantID, bot BotID, runbook RunbookName, namespace string, start AutonomyLevel, now time.Time) (RunbookAutonomy, error) {
	if tenant == "" || bot == "" || runbook == "" {
		return RunbookAutonomy{}, invalid("runbook autonomy", "needs a tenant, a bot and a runbook")
	}
	if _, err := validateIdentifier("runbook autonomy namespace", namespace); err != nil {
		return RunbookAutonomy{}, err
	}
	if _, err := ParseAutonomyLevel(start.String()); err != nil || start == "" {
		return RunbookAutonomy{}, invalid("runbook autonomy start", "must be a level")
	}
	if now.IsZero() {
		return RunbookAutonomy{}, invalid("created at", "must not be zero")
	}
	return RunbookAutonomy{id: AutonomyIDOf(tenant, bot, runbook, namespace), tenant: tenant, bot: bot, runbook: runbook, namespace: namespace, step: start, start: start, createdAt: now, updatedAt: now}, nil
}

// ID identifies the record.
func (a RunbookAutonomy) ID() AutonomyID { return a.id }

// TenantID, BotID, Runbook and Namespace name the unit.
func (a RunbookAutonomy) TenantID() TenantID   { return a.tenant }
func (a RunbookAutonomy) BotID() BotID         { return a.bot }
func (a RunbookAutonomy) Runbook() RunbookName { return a.runbook }
func (a RunbookAutonomy) Namespace() string    { return a.namespace }

// Tool is the write this record was earned with; empty until it is bound.
func (a RunbookAutonomy) Tool() ToolName { return a.tool }

// BindTool ties the record to its write. A record earned with another
// tool starts over (decision of the review of #143): the history stays,
// the step goes back to where it began and nothing counts. True when it
// changed the record.
func (a *RunbookAutonomy) BindTool(tool ToolName, now time.Time) bool {
	switch a.tool {
	case tool:
		return false
	case "":
		a.tool = tool
		return true
	}
	entry := AutonomyEntry{Signal: SignalToolChanged.String(), At: now, From: a.step.String(), To: a.start.String(), Reason: a.tool.String() + " to " + tool.String()}
	before := a.earned
	entry.EarnedBefore = &before
	a.tool, a.step, a.totals, a.earned = tool, a.start, AutonomyCounters{}, AutonomyCounters{}
	a.open = nil
	a.remember(entry, now)
	return true
}

// Begin records an apply about to write, before it does: outstanding
// until its green or its failure is recorded. Idempotent.
func (a *RunbookAutonomy) Begin(incident IncidentID, cycle int, now time.Time) {
	if !slices.ContainsFunc(a.open, func(o AutonomyOutstanding) bool { return o.IncidentID == incident.String() && o.Cycle == cycle }) {
		a.open = append(a.open, AutonomyOutstanding{IncidentID: incident.String(), Cycle: cycle, At: now})
		a.updatedAt = now
	}
}

// Abandon drops an outstanding apply that wrote nothing: refused before
// it ran, taken back to the card, or failed before any tool call. True
// when it was outstanding.
func (a *RunbookAutonomy) Abandon(incident IncidentID, cycle int, now time.Time) bool {
	before := len(a.open)
	a.open = slices.DeleteFunc(a.open, func(o AutonomyOutstanding) bool { return o.IncidentID == incident.String() && o.Cycle == cycle })
	if len(a.open) == before {
		return false
	}
	a.updatedAt = now
	return true
}

// HoldsOpen reports an apply other than this incident's cycle whose end
// is not recorded: while one is, the runbook gets no grant.
func (a RunbookAutonomy) HoldsOpen(incident IncidentID, cycle int) bool {
	return slices.ContainsFunc(a.open, func(o AutonomyOutstanding) bool { return o.IncidentID != incident.String() || o.Cycle != cycle })
}

// Outstanding lists the applies whose end is not recorded, copied.
func (a RunbookAutonomy) Outstanding() []AutonomyOutstanding {
	return append([]AutonomyOutstanding(nil), a.open...)
}

// Lower takes the runbook one step down by a person's hand, and ends
// whatever was outstanding: the person looked. Audited by the caller.
func (a *RunbookAutonomy) Lower(actor, reason string, now time.Time) error {
	if !isPerson(actor) {
		return invalid("actor", "a runbook is lowered by a named person")
	}
	before, from := a.earned, a.step
	a.step, a.earned = a.step.lowered(SignalLoweredByPerson.lowers()), AutonomyCounters{}
	a.open = nil
	a.remember(AutonomyEntry{Signal: SignalLoweredByPerson.String(), Actor: actor, At: now, From: from.String(), To: a.step.String(), EarnedBefore: &before, Reason: autonomyReason(reason)}, now)
	return nil
}

// Clear ends every outstanding apply by a person's hand, lowering and
// zeroing nothing. It answers how many it ended; none records nothing.
func (a *RunbookAutonomy) Clear(actor, reason string, now time.Time) (int, error) {
	if !isPerson(actor) {
		return 0, invalid("actor", "outstanding applies are cleared by a named person")
	}
	cleared := len(a.open)
	if cleared == 0 {
		return 0, nil
	}
	a.open = nil
	a.remember(AutonomyEntry{Signal: SignalOutstandingCleared.String(), Actor: actor, At: now, Reason: autonomyReason(reason)}, now)
	return cleared, nil
}

// autonomyReason bounds and cleans what a person wrote, as an incident's
// reason is; nothing usable is no reason.
func autonomyReason(raw string) string {
	if IncidentTextIsEmpty(raw) {
		return ""
	}
	return IncidentText(raw, MaxIncidentReasonLength)
}

// Step is where the runbook stands now.
func (a RunbookAutonomy) Step() AutonomyLevel { return a.step }

// Totals are every signal counted since the runbook began; Earned those
// since its last lowering.
func (a RunbookAutonomy) Totals() AutonomyCounters { return a.totals }
func (a RunbookAutonomy) Earned() AutonomyCounters { return a.earned }

// History lists every signal, oldest first, copied.
func (a RunbookAutonomy) History() []AutonomyEntry { return append([]AutonomyEntry(nil), a.history...) }

// MayActAlone reports a runbook at the top step with all three
// thresholds met by what it earned since its last lowering: only a
// history of a person's acts gets there.
func (a RunbookAutonomy) MayActAlone(thresholds AutonomyThresholds) bool {
	return a.tool != "" && a.step == AutonomyActWithinPolicy && a.earned.meets(thresholds)
}

// Seen reports a signal recorded already for the incident's cycle.
func (a RunbookAutonomy) Seen(incident IncidentID, cycle int, signal AutonomySignal) bool {
	return slices.ContainsFunc(a.history, func(entry AutonomyEntry) bool {
		return entry.IncidentID == incident.String() && entry.Cycle == cycle && entry.Signal == signal.String()
	})
}

// DiagnosisMark is a person's verdict on the cycle's diagnosis, and
// false when nobody marked it.
func (a RunbookAutonomy) DiagnosisMark(incident IncidentID, cycle int) (AutonomySignal, bool) {
	for _, signal := range []AutonomySignal{SignalDiagnosisCorrect, SignalDiagnosisWrong} {
		if a.Seen(incident, cycle, signal) {
			return signal, true
		}
	}
	return "", false
}

// Record takes one signal, under the thresholds the bot's ladder has
// now: the first time for its incident, cycle and signal it is kept in
// the history, and counted or lowers the step as it says. A green verify
// is recorded for any cycle, and counts only for one a person approved,
// under that person's name; a counted signal needs a named actor who is
// no machine. A denial lowers whoever took it. A green or a lowering ends
// that apply's outstanding entry. A second, different mark of the same
// diagnosis is refused; an identical one changes nothing.
func (a *RunbookAutonomy) Record(signal AutonomySignal, incident IncidentID, cycle int, actor string, thresholds AutonomyThresholds, now time.Time) (AutonomyChange, error) {
	unchanged := AutonomyChange{From: a.step, To: a.step}
	if _, err := ParseAutonomySignal(signal.String()); err != nil {
		return unchanged, err
	}
	if incident == "" || cycle < 1 {
		return unchanged, invalid("autonomy signal", "needs an incident and its cycle")
	}
	if a.Seen(incident, cycle, signal) {
		return unchanged, nil
	}
	if mark, marked := a.DiagnosisMark(incident, cycle); marked && signal.isDiagnosisMark() {
		return unchanged, StateError{Subject: "the diagnosis of incident " + incident.String() + " cycle " + strconv.Itoa(cycle), Current: "marked " + mark.String(), Attempted: "mark it " + signal.String()}
	}
	if signal.needsPerson() && !isPerson(actor) {
		return unchanged, nil
	}
	approved := true
	if signal == SignalApplyVerified {
		var approver string
		if approver, approved = a.approverOf(incident, cycle); approved {
			actor = approver
		}
	}
	if signal.settles() {
		a.Abandon(incident, cycle, now)
	}
	entry := AutonomyEntry{Signal: signal.String(), IncidentID: incident.String(), Cycle: cycle, Actor: actor, At: now}
	if down := signal.lowers(); down > 0 {
		before := a.earned
		from := a.step
		a.step = a.step.lowered(down)
		a.earned = AutonomyCounters{}
		entry.From, entry.To, entry.EarnedBefore = from.String(), a.step.String(), &before
	} else if approved && a.totals.add(signal) {
		a.earned.add(signal)
		from := a.step
		a.climb(thresholds)
		if a.step != from {
			entry.From, entry.To = from.String(), a.step.String()
		}
	}
	a.remember(entry, now)
	return AutonomyChange{Recorded: true, From: unchanged.From, To: a.step}, nil
}

// climb promotes the runbook as far as what it earned allows: propose
// takes the diagnoses, act-with-approval the proposals, act-within-policy
// all three (decision D14).
func (a *RunbookAutonomy) climb(t AutonomyThresholds) {
	for {
		switch {
		case a.step == AutonomyObserve && a.earned.Diagnoses >= t.diagnoses:
			a.step = AutonomyPropose
		case a.step == AutonomyPropose && a.earned.Proposals >= t.proposals:
			a.step = AutonomyActWithApproval
		case a.step == AutonomyActWithApproval && a.earned.meets(t):
			a.step = AutonomyActWithinPolicy
		default:
			return
		}
	}
}

// approverOf is who approved the apply of the incident's cycle.
func (a RunbookAutonomy) approverOf(incident IncidentID, cycle int) (string, bool) {
	for _, entry := range a.history {
		if entry.IncidentID == incident.String() && entry.Cycle == cycle && entry.Signal == SignalProposalApproved.String() {
			return entry.Actor, true
		}
	}
	return "", false
}

// isPerson reports a named actor that says it is no machine.
func isPerson(actor string) bool {
	name, err := ValidateActor("actor", actor)
	return err == nil && !IsMachineActor(name)
}

// maxAutonomyHistory is how many lines a record keeps. The counters stay
// exact; the idempotency of a cycle older than this is given up.
const maxAutonomyHistory = 4096

// remember keeps the entry, dropping the oldest line that moved no step
// once the history is full (the oldest of all when every line did).
func (a *RunbookAutonomy) remember(entry AutonomyEntry, now time.Time) {
	a.history = append(a.history, entry)
	a.history = capHistory(a.history)
	a.updatedAt = now
}

func capHistory(history []AutonomyEntry) []AutonomyEntry {
	over := len(history) - maxAutonomyHistory
	if over <= 0 {
		return history
	}
	kept := make([]AutonomyEntry, 0, maxAutonomyHistory)
	for _, entry := range history {
		if over > 0 && entry.From == "" {
			over--
			continue
		}
		kept = append(kept, entry)
	}
	if over > 0 {
		kept = kept[over:]
	}
	return kept
}

// Snapshot returns the record as plain data.
func (a RunbookAutonomy) Snapshot() RunbookAutonomySnapshot {
	return RunbookAutonomySnapshot{
		ID: a.id.String(), TenantID: a.tenant.String(), BotID: a.bot.String(), Runbook: a.runbook.String(), Namespace: a.namespace, Tool: a.tool.String(), Outstanding: a.Outstanding(),
		Step: a.step.String(), Start: a.start.String(), Totals: a.totals, Earned: a.earned, History: a.History(), CreatedAt: a.createdAt, UpdatedAt: a.updatedAt,
	}
}

// RestoreRunbookAutonomy rebuilds a record, validating every value.
func RestoreRunbookAutonomy(s RunbookAutonomySnapshot) (RunbookAutonomy, error) {
	tenant, err := NewTenantID(s.TenantID)
	if err != nil {
		return RunbookAutonomy{}, err
	}
	bot, err := NewBotID(s.BotID)
	if err != nil {
		return RunbookAutonomy{}, err
	}
	runbook, err := NewRunbookName(s.Runbook)
	if err != nil {
		return RunbookAutonomy{}, err
	}
	start, err := ParseAutonomyLevel(s.Start)
	if err != nil {
		return RunbookAutonomy{}, err
	}
	step, err := ParseAutonomyLevel(s.Step)
	if err != nil {
		return RunbookAutonomy{}, err
	}
	a, err := NewRunbookAutonomy(tenant, bot, runbook, s.Namespace, start, s.CreatedAt)
	if err != nil {
		return RunbookAutonomy{}, err
	}
	if s.ID != a.id.String() {
		return RunbookAutonomy{}, invalid("autonomy id", "does not name this runbook, namespace and bot")
	}
	if s.UpdatedAt.IsZero() {
		return RunbookAutonomy{}, invalid("updated at", "must not be zero")
	}
	if !s.Totals.valid() || !s.Earned.valid() {
		return RunbookAutonomy{}, invalid("autonomy counters", "must not be negative")
	}
	for _, entry := range s.History {
		signal, err := ParseAutonomySignal(entry.Signal)
		if err != nil {
			return RunbookAutonomy{}, err
		}
		if entry.At.IsZero() {
			return RunbookAutonomy{}, invalid("autonomy history", "an entry needs when")
		}
		if !signal.belongsToAnIncident() {
			continue
		}
		if _, err := NewIncidentID(entry.IncidentID); err != nil {
			return RunbookAutonomy{}, err
		}
		if entry.Cycle < 1 {
			return RunbookAutonomy{}, invalid("autonomy history", "an entry needs its cycle")
		}
	}
	for _, outstanding := range s.Outstanding {
		if _, err := NewIncidentID(outstanding.IncidentID); err != nil {
			return RunbookAutonomy{}, err
		}
		if outstanding.Cycle < 1 {
			return RunbookAutonomy{}, invalid("autonomy outstanding", "an entry needs its cycle")
		}
	}
	if s.Tool != "" {
		if a.tool, err = NewToolName(s.Tool); err != nil {
			return RunbookAutonomy{}, err
		}
	}
	a.step, a.totals, a.earned, a.updatedAt = step, s.Totals, s.Earned, s.UpdatedAt
	a.history = capHistory(append([]AutonomyEntry(nil), s.History...))
	a.open = append([]AutonomyOutstanding(nil), s.Outstanding...)
	return a, nil
}
