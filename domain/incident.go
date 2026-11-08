package domain

import (
	"slices"
	"strconv"
	"strings"
	"time"
)

// Limits of an incident's text fields and of its alerts. The diagnosis,
// proposal and reason limits are exported so that what a task wrote can
// be cut to fit with IncidentText.
const (
	maxIncidentTitleLength     = 200
	MaxIncidentDiagnosisLength = 16000
	MaxIncidentProposalLength  = 16000
	MaxIncidentReasonLength    = 1000
	maxIncidentFingerprints    = 64
	maxIncidentNamespaceLength = 253
	maxIncidentNamespaces      = 32
	// maxIncidentCostedExecutions is how many execution ids an incident
	// remembers having counted the cost of: the last ones.
	maxIncidentCostedExecutions = 64
	// IncidentReopenWindow is how long after a resolution a new alert of
	// the same group reopens the incident instead of opening another.
	IncidentReopenWindow = 30 * time.Minute
	// IncidentEscalationRetries is how many times an alert of an
	// Escalated incident starts another triage before the incident is
	// left to a person: a triage that failed (a model, a node, a tool
	// that was down) gets another try when its alert comes again, and a
	// fault that keeps failing stops costing.
	IncidentEscalationRetries = 2
	// IncidentMaxSendBacks is how many times an incident may go back to
	// Open from a proposal (its apply denied, expired, failed or
	// cancelled, its verify red) before it is left to a person: each one
	// starts another triage and proposal, the last one escalates it.
	IncidentMaxSendBacks = 2
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
	namespaces   []string
	alerts       AlertGroup
	status       IncidentStatus
	fingerprints []string
	resolved     []string
	cycle        int
	tasks        []IncidentTask
	diagnosis    string
	proposal     string
	reason       string
	repeats      int
	retries      int
	sendBacks    int
	costUSD      float64
	costUnknown  bool
	costed       []ExecutionID
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

// addFingerprints keeps the alerts not seen yet, in the order given, up
// to maxIncidentFingerprints.
func (i *Incident) addFingerprints(fingerprints []string) error {
	for _, raw := range fingerprints {
		fingerprint := strings.TrimSpace(raw)
		if fingerprint == "" {
			return invalid("fingerprint", "must not be empty")
		}
		if len(fingerprint) > maxIdentifierLength*2 {
			return invalid("fingerprint", "is too long")
		}
		if slices.Contains(i.fingerprints, fingerprint) || len(i.fingerprints) >= maxIncidentFingerprints {
			// The first 64 alerts are kept; a group that grows past them
			// (a node down takes a hundred pods with it) is still one
			// incident, never a refused delivery.
			continue
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

// Namespace is the first Kubernetes namespace the incident's alerts
// named (their namespace label), empty when they named none: what a
// person reads first. Namespaces has them all.
func (i Incident) Namespace() string {
	if len(i.namespaces) == 0 {
		return ""
	}
	return i.namespaces[0]
}

// Namespaces are every namespace the incident's alerts named, in the
// order they came, the first 32: what the bot's approvals.twoPersons
// reads (any one listed is enough).
func (i Incident) Namespaces() []string { return append([]string(nil), i.namespaces...) }

// ValidateNamespace checks a namespace an alert or a person names: not
// empty, at most 253 characters, no spaces or control characters.
func ValidateNamespace(raw string) (string, error) {
	namespace, err := validateText("namespace", raw, maxIncidentNamespaceLength)
	if err != nil {
		return "", err
	}
	if strings.ContainsAny(namespace, " \t\n") {
		return "", invalid("namespace", "must not contain spaces")
	}
	return namespace, nil
}

// RecordNamespaces adds the namespaces the incident's alerts name that
// it has not seen, up to 32; empty ones change nothing. An invalid one
// is left out and returned, so a strange label never refuses an alert
// (review of #139): the caller audits it.
func (i *Incident) RecordNamespaces(raw ...string) []string {
	var ignored []string
	for _, value := range raw {
		if strings.TrimSpace(value) == "" {
			continue
		}
		namespace, err := ValidateNamespace(value)
		if err != nil {
			ignored = append(ignored, value)
			continue
		}
		if slices.Contains(i.namespaces, namespace) || len(i.namespaces) >= maxIncidentNamespaces {
			continue
		}
		i.namespaces = append(i.namespaces, namespace)
	}
	return ignored
}

// AlertGroup says what is known of the incident's alerts.
func (i Incident) AlertGroup() AlertGroup { return i.alerts }

// ObserveAlerts takes what one delivery says of the group. Mixed is for
// the incident's life: a delivery that is mixed, names another alert or
// is worse than the incident's severity makes it so. Unknown, a hand's
// report, changes nothing.
func (i *Incident) ObserveAlerts(group AlertGroup, title string, severity IncidentSeverity) {
	if group == AlertGroupUnknown || i.alerts == AlertGroupMixed {
		return
	}
	if group == AlertGroupMixed || title != i.title || severity.moreSevereThan(i.severity) {
		i.alerts = AlertGroupMixed
		return
	}
	i.alerts = AlertGroupUniform
}

// Status is where the incident stands.
func (i Incident) Status() IncidentStatus { return i.status }

// Fingerprints lists the alerts seen in the incident, oldest first.
func (i Incident) Fingerprints() []string { return append([]string(nil), i.fingerprints...) }

// ResolvedFingerprints lists the alerts the source said went green, in
// the order they did.
func (i Incident) ResolvedFingerprints() []string { return append([]string(nil), i.resolved...) }

// FiringFingerprints lists the alerts seen that the source has not
// resolved.
func (i Incident) FiringFingerprints() []string {
	var firing []string
	for _, fingerprint := range i.fingerprints {
		if !slices.Contains(i.resolved, fingerprint) {
			firing = append(firing, fingerprint)
		}
	}
	return firing
}

// MarkResolved records the alerts the source said went green; an alert
// not seen before is recorded as seen and resolved at once. It changes
// no status: the source says when the whole group is resolved.
func (i *Incident) MarkResolved(fingerprints []string, now time.Time) error {
	if err := i.addFingerprints(fingerprints); err != nil {
		return err
	}
	for _, raw := range fingerprints {
		fingerprint := strings.TrimSpace(raw)
		// Only an alert the incident kept is resolved: one dropped past
		// maxIncidentFingerprints would make the snapshot one that
		// RestoreIncident refuses, and the store could not load it again.
		if slices.Contains(i.fingerprints, fingerprint) && !slices.Contains(i.resolved, fingerprint) {
			i.resolved = append(i.resolved, fingerprint)
		}
	}
	i.lastSeenAt, i.updatedAt = now, now
	return nil
}

// Cycle counts the times the incident was opened: 1, then one more for
// each reopening.
func (i Incident) Cycle() int { return i.cycle }

// Tasks lists the tasks that worked on the incident, oldest first: at
// most one per phase in each cycle.
func (i Incident) Tasks() []IncidentTask { return append([]IncidentTask(nil), i.tasks...) }

// TaskIDs lists the tasks that worked on the incident, oldest first.
func (i Incident) TaskIDs() []TaskID {
	var ids []TaskID
	for _, task := range i.tasks {
		ids = append(ids, task.task)
	}
	return ids
}

// CurrentTask is the last task attached: the one whose end moves the
// incident on. False before one was attached.
func (i Incident) CurrentTask() (IncidentTask, bool) {
	if len(i.tasks) == 0 {
		return IncidentTask{}, false
	}
	return i.tasks[len(i.tasks)-1], true
}

// CurrentTaskID is the last task attached, or empty before one was.
func (i Incident) CurrentTaskID() TaskID {
	task, _ := i.CurrentTask()
	return task.task
}

// HasTask reports a task of the phase attached in the current cycle.
func (i Incident) HasTask(phase IncidentPhase) bool {
	for _, task := range i.tasks {
		if task.phase == phase && task.cycle == i.cycle {
			return true
		}
	}
	return false
}

// Diagnosis is what the last completed triage task said.
func (i Incident) Diagnosis() string { return i.diagnosis }

// Proposal is what the fix task of the current diagnosis proposed: the
// change and its rollback, applied by nobody. Empty until a fix task
// completes, and again once a new triage replaces the diagnosis.
func (i Incident) Proposal() string { return i.proposal }

// Reason says why the incident was escalated, resolved or sent back to
// Open.
func (i Incident) Reason() string { return i.reason }

// Repeats counts the alerts received while the incident was open.
func (i Incident) Repeats() int { return i.repeats }

// CostUSD sums what the incident's executions reported.
func (i Incident) CostUSD() float64 { return i.costUSD }

// CostUnknown reports that one of the incident's executions stopped
// before it said what it cost: CostUSD is then a floor, not the total.
func (i Incident) CostUnknown() bool { return i.costUnknown }

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
// cycle, before the task runs. The incident's own bot runs it.
func (i *Incident) AttachTask(task TaskID, now time.Time) error {
	if i.status.IsResolved() {
		return i.refuse("attach a task")
	}
	return i.attach(task, i.bot, IncidentPhaseTriage, now)
}

// AttachPhaseTask records the task of a phase after triage (fix, apply
// or verify) of the current cycle, once per cycle, when the incident has
// the status the phase is launched at (IncidentPhase.LaunchedAt): the bot
// is the teammate that takes the phase.
func (i *Incident) AttachPhaseTask(task TaskID, bot BotID, phase IncidentPhase, now time.Time) error {
	if phase == IncidentPhaseTriage || i.status != phase.LaunchedAt() {
		return i.refuse("attach a " + phase.String() + " task")
	}
	return i.attach(task, bot, phase, now)
}

func (i *Incident) attach(task TaskID, bot BotID, phase IncidentPhase, now time.Time) error {
	if task == "" {
		return invalid("task id", "must not be empty")
	}
	if i.HasTask(phase) {
		return i.refuse("attach a second " + phase.String() + " task in cycle " + strconv.Itoa(i.cycle))
	}
	attached, err := NewIncidentTask(task, bot, phase, i.cycle)
	if err != nil {
		return err
	}
	i.tasks = append(i.tasks, attached)
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
	i.unresolve(fingerprints)
	i.repeats++
	i.lastSeenAt, i.updatedAt = now, now
	return nil
}

// CanRetry reports an Escalated incident a new alert gives another
// triage: fewer than IncidentEscalationRetries retries since its last
// proposal or reopening. On 1 November 2026 a triage failed on a tool
// server that was down, and its Escalated incident took every later
// alert of the group as a repeat: nobody diagnosed the fault again. A
// diagnosis does not give the retries back: a fix task that fails every
// time would otherwise start triage and fix again on every alert.
//
// An incident escalated because it was sent back IncidentMaxSendBacks
// times is not retried: its fix was refused or failed twice, and another proposal
// would only ask again.
func (i Incident) CanRetry() bool {
	return i.status == IncidentEscalated && i.retries < IncidentEscalationRetries && i.sendBacks < IncidentMaxSendBacks
}

// Retry opens an Escalated incident again on a new alert: one more
// cycle, Open, with the alerts; the reason it was escalated is kept
// until the new triage settles.
func (i *Incident) Retry(fingerprints []string, now time.Time) error {
	if !i.CanRetry() {
		return i.refuse("retry")
	}
	if err := i.addFingerprints(fingerprints); err != nil {
		return err
	}
	i.unresolve(fingerprints)
	i.cycle++
	i.retries++
	i.status = IncidentOpen
	i.lastSeenAt, i.updatedAt = now, now
	return nil
}

// Retries counts the triages an alert started again on the Escalated
// incident since its last proposal or reopening.
func (i Incident) Retries() int { return i.retries }

// CanReopen reports a resolved incident a new alert at now reopens: the
// resolution is less than IncidentReopenWindow ago.
func (i Incident) CanReopen(now time.Time) bool {
	return i.status.IsResolved() && i.resolvedAt != nil && now.Before(i.resolvedAt.Add(IncidentReopenWindow))
}

// Reopen opens a resolved incident again, within the window: one more
// cycle, Open, with the new alerts; the diagnosis, the proposal and the
// reason of the last cycle are kept until a new triage replaces them.
func (i *Incident) Reopen(fingerprints []string, now time.Time) error {
	if !i.CanReopen(now) {
		return i.refuse("reopen")
	}
	if err := i.addFingerprints(fingerprints); err != nil {
		return err
	}
	i.unresolve(fingerprints)
	i.cycle++
	i.retries = 0
	i.sendBacks = 0
	i.status = IncidentOpen
	i.resolvedAt = nil
	i.lastSeenAt, i.updatedAt = now, now
	return nil
}

// Diagnose records the cause the triage task found: Open becomes
// Diagnosed. A proposal of an earlier cycle answered another diagnosis:
// it is dropped.
func (i *Incident) Diagnose(diagnosis string, now time.Time) error {
	if i.status != IncidentOpen {
		return i.refuse("diagnose")
	}
	text, err := validateText("diagnosis", diagnosis, MaxIncidentDiagnosisLength)
	if err != nil {
		return err
	}
	i.diagnosis = text
	i.proposal = ""
	i.status = IncidentDiagnosed
	i.updatedAt = now
	return nil
}

// Propose records the change and its rollback the fix task of the
// current cycle proposed: Diagnosed becomes Proposed. Nothing is applied;
// the incident has gone through its phases, so it gets its retries back.
func (i *Incident) Propose(proposal string, now time.Time) error {
	if i.status != IncidentDiagnosed || !i.HasTask(IncidentPhaseFix) {
		return i.refuse("propose")
	}
	text, err := validateText("proposal", proposal, MaxIncidentProposalLength)
	if err != nil {
		return err
	}
	i.proposal = text
	i.retries = 0
	i.status = IncidentProposed
	i.updatedAt = now
	return nil
}

// Escalate hands the incident to a person: the bots could not go on.
// Any status but Resolved and Escalated may be escalated.
func (i *Incident) Escalate(reason string, now time.Time) error {
	if i.status.IsResolved() || i.status == IncidentEscalated {
		return i.refuse("escalate")
	}
	text, err := validateText("reason", reason, MaxIncidentReasonLength)
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
		text, err := validateText("reason", reason, MaxIncidentReasonLength)
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

// AddCost adds what an execution of the incident's tasks reported; a
// usage whose cost is unknown marks the incident's cost unknown too.
func (i *Incident) AddCost(usage Usage, now time.Time) error {
	if usage.CostUSD < 0 {
		return invalid("cost", "must not be negative")
	}
	i.costUSD += usage.CostUSD
	i.costUnknown = i.costUnknown || usage.CostUnknown
	i.updatedAt = now
	return nil
}

// AddExecutionCost adds what one execution of the incident's tasks
// reported, once per execution: a report delivered twice, or a waiting
// run counted when it stopped and seen again when its task ends, adds
// nothing more. False when the execution was counted already. An
// execution without an id (an expiry that ran nothing) adds nothing.
func (i *Incident) AddExecutionCost(execution ExecutionID, usage Usage, now time.Time) (bool, error) {
	if execution == "" || slices.Contains(i.costed, execution) {
		return false, nil
	}
	if err := i.AddCost(usage, now); err != nil {
		return false, err
	}
	i.costed = append(i.costed, execution)
	if len(i.costed) > maxIncidentCostedExecutions {
		// The oldest ids go; their cost stays added. A report of one of
		// them coming back that late is not a redelivery to guard.
		i.costed = append([]ExecutionID(nil), i.costed[len(i.costed)-maxIncidentCostedExecutions:]...)
	}
	return true, nil
}

// unresolve takes alerts that fire again out of the resolved ones.
func (i *Incident) unresolve(fingerprints []string) {
	for _, raw := range fingerprints {
		fingerprint := strings.TrimSpace(raw)
		i.resolved = slices.DeleteFunc(i.resolved, func(resolved string) bool { return resolved == fingerprint })
	}
}

func (i Incident) refuse(attempted string) error {
	return StateError{Subject: "incident " + i.id.String(), Current: i.status.String(), Attempted: attempted}
}

// Snapshot returns the incident as plain data.
func (i Incident) Snapshot() IncidentSnapshot {
	var (
		ids   []string
		tasks []IncidentTaskSnapshot
	)
	for _, task := range i.tasks {
		ids = append(ids, task.task.String())
		tasks = append(tasks, task.Snapshot())
	}
	var resolvedAt *time.Time
	if i.resolvedAt != nil {
		at := *i.resolvedAt
		resolvedAt = &at
	}
	return IncidentSnapshot{
		ID: i.id.String(), TenantID: i.tenant.String(), BotID: i.bot.String(), GroupKey: i.groupKey, Source: i.source,
		Title: i.title, Severity: i.severity.String(), Namespace: i.Namespace(), Namespaces: i.namespacesSnapshot(), AlertGroup: i.alerts.String(), Status: i.status.String(), Fingerprints: i.Fingerprints(), ResolvedFingerprints: i.ResolvedFingerprints(),
		Cycle: i.cycle, TaskIDs: ids, Tasks: tasks, Diagnosis: i.diagnosis, Proposal: i.proposal, Reason: i.reason, Repeats: i.repeats, Retries: i.retries, SendBacks: i.sendBacks, CostUSD: i.costUSD, CostUnknown: i.costUnknown, CostedExecutions: i.costedSnapshot(),
		OpenedAt: i.openedAt, UpdatedAt: i.updatedAt, LastSeenAt: i.lastSeenAt, ResolvedAt: resolvedAt,
	}
}

// namespacesSnapshot lists the namespaces when there are two or more:
// one is said by namespace alone, so an incident of one namespace reads
// back as it was written.
func (i Incident) namespacesSnapshot() []string {
	if len(i.namespaces) < 2 {
		return nil
	}
	return i.Namespaces()
}

func (i Incident) costedSnapshot() []string {
	var ids []string
	for _, id := range i.costed {
		ids = append(ids, id.String())
	}
	return ids
}

// restoreTasks rebuilds the incident's tasks: from Tasks, or, for an
// incident stored before its tasks had phases, from TaskIDs, which then
// lists triage tasks of the incident's bot, one per cycle. Tasks run in
// cycle order, one per phase in each cycle, none past the current cycle.
func restoreTasks(s IncidentSnapshot) ([]IncidentTask, error) {
	snapshots := s.Tasks
	if len(snapshots) == 0 {
		for n, raw := range s.TaskIDs {
			snapshots = append(snapshots, IncidentTaskSnapshot{TaskID: raw, BotID: s.BotID, Phase: IncidentPhaseTriage.String(), Cycle: n + 1})
		}
	}
	var tasks []IncidentTask
	for _, snapshot := range snapshots {
		task, err := restoreIncidentTask(snapshot)
		if err != nil {
			return nil, err
		}
		if task.cycle > s.Cycle {
			return nil, invalid("tasks", "an incident has no task past its cycle")
		}
		for _, earlier := range tasks {
			if earlier.cycle > task.cycle || (earlier.cycle == task.cycle && earlier.phase == task.phase) {
				return nil, invalid("tasks", "an incident has one task per phase in each cycle, in cycle order")
			}
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
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
	stored := s.Namespaces
	if len(stored) == 0 && s.Namespace != "" {
		// Stored when the incident kept one namespace only.
		stored = []string{s.Namespace}
	}
	if ignored := incident.RecordNamespaces(stored...); len(ignored) > 0 {
		return Incident{}, invalid("namespaces", "a stored namespace is not valid: "+ignored[0])
	}
	if incident.alerts, err = ParseAlertGroup(s.AlertGroup); err != nil {
		return Incident{}, err
	}
	if s.Namespace != "" && incident.Namespace() != strings.TrimSpace(s.Namespace) {
		return Incident{}, invalid("namespace", "must be the first of namespaces")
	}
	switch {
	case s.UpdatedAt.IsZero():
		return Incident{}, invalid("updated at", "must not be zero")
	case s.LastSeenAt.IsZero():
		return Incident{}, invalid("last seen at", "must not be zero")
	case s.Cycle < 1:
		return Incident{}, invalid("cycle", "must be at least 1")
	case s.Repeats < 0 || s.Retries < 0 || s.CostUSD < 0:
		return Incident{}, invalid("repeats, retries or cost", "must not be negative")
	case s.SendBacks < 0 || s.SendBacks > IncidentMaxSendBacks:
		return Incident{}, invalid("sendBacks", "must be 0 to "+strconv.Itoa(IncidentMaxSendBacks))
	case status.IsResolved() && s.ResolvedAt == nil:
		return Incident{}, invalid("resolved at", "a Resolved incident needs it")
	case !status.IsResolved() && s.ResolvedAt != nil:
		return Incident{}, invalid("resolved at", "only a Resolved incident has it")
	case status == IncidentEscalated && s.Reason == "":
		return Incident{}, invalid("reason", "an Escalated incident needs it")
	case status.HasProposal() && s.Proposal == "":
		return Incident{}, invalid("proposal", "a "+status.String()+" incident needs it")
	}
	if incident.tasks, err = restoreTasks(s); err != nil {
		return Incident{}, err
	}
	if s.Diagnosis != "" {
		if incident.diagnosis, err = validateText("diagnosis", s.Diagnosis, MaxIncidentDiagnosisLength); err != nil {
			return Incident{}, err
		}
	}
	if s.Proposal != "" {
		if incident.proposal, err = validateText("proposal", s.Proposal, MaxIncidentProposalLength); err != nil {
			return Incident{}, err
		}
	}
	if s.Reason != "" {
		if incident.reason, err = validateText("reason", s.Reason, MaxIncidentReasonLength); err != nil {
			return Incident{}, err
		}
	}
	if s.ResolvedAt != nil {
		at := *s.ResolvedAt
		incident.resolvedAt = &at
	}
	for _, raw := range s.CostedExecutions {
		id, err := NewExecutionID(raw)
		if err != nil {
			return Incident{}, err
		}
		incident.costed = append(incident.costed, id)
	}
	for _, fingerprint := range s.ResolvedFingerprints {
		if !slices.Contains(incident.fingerprints, fingerprint) {
			return Incident{}, invalid("resolved fingerprints", "must be alerts the incident saw")
		}
		if !slices.Contains(incident.resolved, fingerprint) {
			incident.resolved = append(incident.resolved, fingerprint)
		}
	}
	incident.cycle, incident.repeats, incident.retries, incident.sendBacks, incident.costUSD, incident.costUnknown = s.Cycle, s.Repeats, s.Retries, s.SendBacks, s.CostUSD, s.CostUnknown
	incident.status, incident.updatedAt, incident.lastSeenAt = status, s.UpdatedAt, s.LastSeenAt
	return incident, nil
}
