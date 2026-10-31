package domain

import (
	"slices"
	"strconv"
	"strings"
	"time"
)

// Limits of an incident's text fields and of its alerts.
const (
	maxIncidentTitleLength     = 200
	maxIncidentDiagnosisLength = 16000
	maxIncidentReasonLength    = 1000
	maxIncidentFingerprints    = 64
	// IncidentReopenWindow is how long after a resolution a new alert of
	// the same group reopens the incident instead of opening another.
	IncidentReopenWindow = 30 * time.Minute
)

// Incident is one grouped fault of a tenant: what the alerts say, which
// bot triages it, the tasks that worked on it and where it stands. An
// incident is keyed by the source's group key; a new alert of a group
// that is already open is a repeat, one that comes soon after a
// resolution reopens it, a later one opens another incident.
type Incident struct {
	id           IncidentID
	tenant       TenantID
	bot          BotID
	groupKey     string
	source       string
	title        string
	severity     IncidentSeverity
	status       IncidentStatus
	fingerprints []string
	cycle        int
	tasks        []TaskID
	diagnosis    string
	reason       string
	repeats      int
	costUSD      float64
	openedAt     time.Time
	updatedAt    time.Time
	lastSeenAt   time.Time
	resolvedAt   *time.Time
}

// NewIncident opens an incident on its first alert: cycle 1, Open, with
// no task yet.
func NewIncident(id IncidentID, tenant TenantID, bot BotID, groupKey, source, title string, severity IncidentSeverity, fingerprints []string, now time.Time) (Incident, error) {
	if now.IsZero() {
		return Incident{}, invalid("opened at", "must not be zero")
	}
	if tenant == "" || bot == "" {
		return Incident{}, invalid("incident", "needs a tenant and a bot")
	}
	groupKey, err := validateText("group key", groupKey, maxIdentifierLength*4)
	if err != nil {
		return Incident{}, err
	}
	source, err = validateIdentifier("source", source)
	if err != nil {
		return Incident{}, err
	}
	title, err = validateText("title", title, maxIncidentTitleLength)
	if err != nil {
		return Incident{}, err
	}
	if _, err := ParseIncidentSeverity(severity.String()); err != nil {
		return Incident{}, err
	}
	i := Incident{
		id: id, tenant: tenant, bot: bot, groupKey: groupKey, source: source, title: title, severity: severity,
		status: IncidentOpen, cycle: 1, openedAt: now, updatedAt: now, lastSeenAt: now,
	}
	if err := i.addFingerprints(fingerprints); err != nil {
		return Incident{}, err
	}
	return i, nil
}

// addFingerprints keeps the alerts not seen yet, in the order given.
func (i *Incident) addFingerprints(fingerprints []string) error {
	for _, raw := range fingerprints {
		fingerprint := strings.TrimSpace(raw)
		if fingerprint == "" {
			return invalid("fingerprint", "must not be empty")
		}
		if len(fingerprint) > maxIdentifierLength*2 {
			return invalid("fingerprint", "is too long")
		}
		if slices.Contains(i.fingerprints, fingerprint) {
			continue
		}
		if len(i.fingerprints) >= maxIncidentFingerprints {
			return invalid("fingerprints", "an incident keeps at most 64 alerts")
		}
		i.fingerprints = append(i.fingerprints, fingerprint)
	}
	return nil
}

// ID is the incident's identifier.
func (i Incident) ID() IncidentID { return i.id }

// TenantID is the tenant the incident belongs to.
func (i Incident) TenantID() TenantID { return i.tenant }

// BotID is the bot that triages the incident.
func (i Incident) BotID() BotID { return i.bot }

// GroupKey is what the source groups the incident's alerts by.
func (i Incident) GroupKey() string { return i.groupKey }

// Source names where the alerts come from (alertmanager, manual).
func (i Incident) Source() string { return i.source }

// Title is the incident in one line.
func (i Incident) Title() string { return i.title }

// Severity ranks the incident.
func (i Incident) Severity() IncidentSeverity { return i.severity }

// Status is where the incident stands.
func (i Incident) Status() IncidentStatus { return i.status }

// Fingerprints lists the alerts seen in the incident, oldest first.
func (i Incident) Fingerprints() []string { return append([]string(nil), i.fingerprints...) }

// Cycle counts the times the incident was opened: 1, then one more for
// each reopening.
func (i Incident) Cycle() int { return i.cycle }

// TaskIDs lists the triage tasks, one per cycle, oldest first.
func (i Incident) TaskIDs() []TaskID { return append([]TaskID(nil), i.tasks...) }

// CurrentTaskID is the task of the current cycle, or empty before one
// was attached.
func (i Incident) CurrentTaskID() TaskID {
	if len(i.tasks) == 0 {
		return ""
	}
	return i.tasks[len(i.tasks)-1]
}

// Diagnosis is what the last completed triage task said.
func (i Incident) Diagnosis() string { return i.diagnosis }

// Reason says why the incident was escalated or resolved.
func (i Incident) Reason() string { return i.reason }

// Repeats counts the alerts received while the incident was open.
func (i Incident) Repeats() int { return i.repeats }

// CostUSD sums what the incident's executions reported.
func (i Incident) CostUSD() float64 { return i.costUSD }

// OpenedAt is when the incident was first opened.
func (i Incident) OpenedAt() time.Time { return i.openedAt }

// UpdatedAt is when the incident last changed.
func (i Incident) UpdatedAt() time.Time { return i.updatedAt }

// LastSeenAt is when an alert of the incident last arrived.
func (i Incident) LastSeenAt() time.Time { return i.lastSeenAt }

// ResolvedAt is when the incident was resolved, and false while it is not.
func (i Incident) ResolvedAt() (time.Time, bool) {
	if i.resolvedAt == nil {
		return time.Time{}, false
	}
	return *i.resolvedAt, true
}

// AttachTask records the triage task of the current cycle, once per
// cycle, before the task runs.
func (i *Incident) AttachTask(task TaskID, now time.Time) error {
	if i.status.IsResolved() {
		return i.refuse("attach a task")
	}
	if task == "" {
		return invalid("task id", "must not be empty")
	}
	if len(i.tasks) >= i.cycle {
		return i.refuse("attach a second task in cycle " + strconv.Itoa(i.cycle))
	}
	i.tasks = append(i.tasks, task)
	i.updatedAt = now
	return nil
}

// Repeat records an alert of a group that is already open: the alerts
// not seen yet are added, nothing else changes.
func (i *Incident) Repeat(fingerprints []string, now time.Time) error {
	if i.status.IsResolved() {
		return i.refuse("repeat")
	}
	if err := i.addFingerprints(fingerprints); err != nil {
		return err
	}
	i.repeats++
	i.lastSeenAt, i.updatedAt = now, now
	return nil
}

// CanReopen reports a resolved incident a new alert at now reopens: the
// resolution is less than IncidentReopenWindow ago.
func (i Incident) CanReopen(now time.Time) bool {
	return i.status.IsResolved() && i.resolvedAt != nil && now.Before(i.resolvedAt.Add(IncidentReopenWindow))
}

// Reopen opens a resolved incident again, within the window: one more
// cycle, Open, with the new alerts; the diagnosis and the reason of the
// last cycle are kept until a new triage replaces them.
func (i *Incident) Reopen(fingerprints []string, now time.Time) error {
	if !i.CanReopen(now) {
		return i.refuse("reopen")
	}
	if err := i.addFingerprints(fingerprints); err != nil {
		return err
	}
	i.cycle++
	i.status = IncidentOpen
	i.resolvedAt = nil
	i.lastSeenAt, i.updatedAt = now, now
	return nil
}

// Diagnose records the cause the triage task found: Open becomes
// Diagnosed.
func (i *Incident) Diagnose(diagnosis string, now time.Time) error {
	if i.status != IncidentOpen {
		return i.refuse("diagnose")
	}
	text, err := validateText("diagnosis", diagnosis, maxIncidentDiagnosisLength)
	if err != nil {
		return err
	}
	i.diagnosis = text
	i.status = IncidentDiagnosed
	i.updatedAt = now
	return nil
}

// Escalate hands the incident to a person: the bots could not go on.
func (i *Incident) Escalate(reason string, now time.Time) error {
	if i.status != IncidentOpen && i.status != IncidentDiagnosed {
		return i.refuse("escalate")
	}
	text, err := validateText("reason", reason, maxIncidentReasonLength)
	if err != nil {
		return err
	}
	i.reason = text
	i.status = IncidentEscalated
	i.updatedAt = now
	return nil
}

// Resolve closes the incident: the alerts went green, or a person said
// so. The reason may be empty.
func (i *Incident) Resolve(reason string, now time.Time) error {
	if i.status.IsResolved() {
		return i.refuse("resolve")
	}
	if reason != "" {
		text, err := validateText("reason", reason, maxIncidentReasonLength)
		if err != nil {
			return err
		}
		reason = text
	}
	i.reason = reason
	i.status = IncidentResolved
	at := now
	i.resolvedAt = &at
	i.updatedAt = now
	return nil
}

// AddCost adds what an execution of the incident's tasks reported.
func (i *Incident) AddCost(usd float64, now time.Time) error {
	if usd < 0 {
		return invalid("cost", "must not be negative")
	}
	i.costUSD += usd
	i.updatedAt = now
	return nil
}

func (i Incident) refuse(attempted string) error {
	return StateError{Subject: "incident " + i.id.String(), Current: i.status.String(), Attempted: attempted}
}

// Snapshot returns the incident as plain data.
func (i Incident) Snapshot() IncidentSnapshot {
	var tasks []string
	for _, task := range i.tasks {
		tasks = append(tasks, task.String())
	}
	var resolvedAt *time.Time
	if i.resolvedAt != nil {
		at := *i.resolvedAt
		resolvedAt = &at
	}
	return IncidentSnapshot{
		ID: i.id.String(), TenantID: i.tenant.String(), BotID: i.bot.String(), GroupKey: i.groupKey, Source: i.source,
		Title: i.title, Severity: i.severity.String(), Status: i.status.String(), Fingerprints: i.Fingerprints(),
		Cycle: i.cycle, TaskIDs: tasks, Diagnosis: i.diagnosis, Reason: i.reason, Repeats: i.repeats, CostUSD: i.costUSD,
		OpenedAt: i.openedAt, UpdatedAt: i.updatedAt, LastSeenAt: i.lastSeenAt, ResolvedAt: resolvedAt,
	}
}

// RestoreIncident rebuilds an incident from a snapshot, validating every
// value.
func RestoreIncident(s IncidentSnapshot) (Incident, error) {
	id, err := NewIncidentID(s.ID)
	if err != nil {
		return Incident{}, err
	}
	tenant, err := NewTenantID(s.TenantID)
	if err != nil {
		return Incident{}, err
	}
	bot, err := NewBotID(s.BotID)
	if err != nil {
		return Incident{}, err
	}
	severity, err := ParseIncidentSeverity(s.Severity)
	if err != nil {
		return Incident{}, err
	}
	status, err := ParseIncidentStatus(s.Status)
	if err != nil {
		return Incident{}, err
	}
	incident, err := NewIncident(id, tenant, bot, s.GroupKey, s.Source, s.Title, severity, s.Fingerprints, s.OpenedAt)
	if err != nil {
		return Incident{}, err
	}
	switch {
	case s.UpdatedAt.IsZero():
		return Incident{}, invalid("updated at", "must not be zero")
	case s.LastSeenAt.IsZero():
		return Incident{}, invalid("last seen at", "must not be zero")
	case s.Cycle < 1:
		return Incident{}, invalid("cycle", "must be at least 1")
	case len(s.TaskIDs) > s.Cycle:
		return Incident{}, invalid("task ids", "an incident has at most one task per cycle")
	case s.Repeats < 0 || s.CostUSD < 0:
		return Incident{}, invalid("repeats or cost", "must not be negative")
	case status.IsResolved() && s.ResolvedAt == nil:
		return Incident{}, invalid("resolved at", "a Resolved incident needs it")
	case !status.IsResolved() && s.ResolvedAt != nil:
		return Incident{}, invalid("resolved at", "only a Resolved incident has it")
	case status == IncidentEscalated && s.Reason == "":
		return Incident{}, invalid("reason", "an Escalated incident needs it")
	}
	for _, raw := range s.TaskIDs {
		task, err := NewTaskID(raw)
		if err != nil {
			return Incident{}, err
		}
		incident.tasks = append(incident.tasks, task)
	}
	if s.Diagnosis != "" {
		if incident.diagnosis, err = validateText("diagnosis", s.Diagnosis, maxIncidentDiagnosisLength); err != nil {
			return Incident{}, err
		}
	}
	if s.Reason != "" {
		if incident.reason, err = validateText("reason", s.Reason, maxIncidentReasonLength); err != nil {
			return Incident{}, err
		}
	}
	if s.ResolvedAt != nil {
		at := *s.ResolvedAt
		incident.resolvedAt = &at
	}
	incident.cycle, incident.repeats, incident.costUSD = s.Cycle, s.Repeats, s.CostUSD
	incident.status, incident.updatedAt, incident.lastSeenAt = status, s.UpdatedAt, s.LastSeenAt
	return incident, nil
}
