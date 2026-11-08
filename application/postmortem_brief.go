package application

import (
	"cmp"
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/underpass-ai/axlr-bots/domain"
)

const (
	// postmortemBotEvents is how many of the triage bot's newest events
	// are read for the incident's own; postmortemTaskEvents how many of
	// each task's.
	postmortemBotEvents  = 2000
	postmortemTaskEvents = 200
	// maxPostmortemTimeline bounds the timeline: the newest entries.
	maxPostmortemTimeline = 200
	// maxPostmortemEntryText bounds an entry's reason.
	maxPostmortemEntryText = 200
	// postmortemTimeLayout writes a moment on a line of the objective.
	postmortemTimeLayout = "2006-01-02T15:04:05Z"
)

// postmortemBrief gathers what a postmortem's task is told, from the
// incident, the events of its tasks and phases and its verify tasks.
type postmortemBrief struct {
	deps Dependencies
}

// input is the task's whole input.
func (b postmortemBrief) input(ctx context.Context, incident domain.Incident) (PostmortemInput, error) {
	events, err := b.events(ctx, incident)
	if err != nil {
		return PostmortemInput{}, err
	}
	s := incident.Snapshot()
	input := PostmortemInput{
		Schema: PostmortemInputSchema, Untrusted: postmortemUntrustedFields, IncidentID: s.ID, Title: s.Title, Severity: s.Severity, Source: s.Source, Namespaces: incident.Namespaces(),
		Cycle: s.Cycle, OpenedAt: s.OpenedAt, Reason: s.Reason, Diagnosis: s.Diagnosis, Proposal: s.Proposal, CostUSD: s.CostUSD, CostUnknown: s.CostUnknown,
		Decisions: decisionsOf(events, incident), Verdicts: b.verdicts(ctx, incident),
	}
	input.Timeline, input.TimelineOmitted = timelineOf(events, incident)
	if at, ok := incident.ResolvedAt(); ok {
		input.ResolvedAt, input.DurationSeconds = at, int64(at.Sub(s.OpenedAt).Seconds())
	}
	if postmortem, ok := incident.Postmortem(); ok && postmortem.Cycle() == incident.Cycle() && postmortem.State() == domain.PostmortemSentBack {
		input.Review = &PostmortemReview{Attempt: postmortem.SendBacks() + 1, Note: postmortem.Note(), By: postmortem.ReviewedBy(), Draft: postmortem.Markdown()}
	}
	return input, nil
}

// postmortemUntrustedFields names the input's fields that hold what a
// bot wrote: data, never instructions.
var postmortemUntrustedFields = []string{"diagnosis", "proposal", "review.draft"}

// events are the incident's own events and those of its tasks, once
// each, oldest first.
func (b postmortemBrief) events(ctx context.Context, incident domain.Incident) ([]domain.Event, error) {
	own, err := b.deps.EventReader.ListEvents(ctx, incident.BotID(), postmortemBotEvents)
	if err != nil {
		return nil, err
	}
	seen := map[domain.EventID]bool{}
	var events []domain.Event
	for _, event := range own {
		if event.IncidentID == incident.ID() {
			seen[event.ID] = true
			events = append(events, event)
		}
	}
	for _, task := range incident.Tasks() {
		if task.Phase() == domain.IncidentPhasePostmortem {
			continue
		}
		list, err := b.deps.EventReader.ListTaskEvents(ctx, task.TaskID(), postmortemTaskEvents)
		if err != nil {
			return nil, err
		}
		for _, event := range list {
			if !seen[event.ID] {
				seen[event.ID] = true
				events = append(events, event)
			}
		}
	}
	slices.SortStableFunc(events, func(x, y domain.Event) int { return x.CreatedAt.Compare(y.CreatedAt) })
	return events, nil
}

// verdicts reads what each verify task of the incident answered; one
// whose task or execution is gone is left out.
func (b postmortemBrief) verdicts(ctx context.Context, incident domain.Incident) []PostmortemVerdict {
	verdicts := []PostmortemVerdict{}
	for _, attached := range incident.Tasks() {
		if attached.Phase() != domain.IncidentPhaseVerify {
			continue
		}
		task, err := b.deps.Tasks.Get(ctx, attached.TaskID())
		if err != nil {
			continue
		}
		execution, err := b.deps.Executions.Get(ctx, task.CurrentExecutionID())
		if err != nil {
			continue
		}
		verdict := domain.ParseVerifyVerdict(execution.Result().Answer).String()
		if verdict == "" {
			verdict = "none"
		}
		verdicts = append(verdicts, PostmortemVerdict{Cycle: attached.Cycle(), TaskID: attached.TaskID().String(), Verdict: verdict})
	}
	return verdicts
}

// inTimeline reports the events a postmortem's timeline tells: the
// incident's moves, and the start, the end and the decisions of its
// tasks. A repeat comes every repeat interval; a postmortem's own moves
// are not the incident's story.
func inTimeline(kind domain.EventType) bool {
	switch kind {
	case domain.EventTaskStarted, domain.EventTaskCompleted, domain.EventTaskFailed, domain.EventTaskCancelled, domain.EventTaskExpired,
		domain.EventTaskWaitingApproval, domain.EventTaskDecided, domain.EventApprovalFirst, domain.EventApprovalExpired:
		return true
	case domain.EventIncidentRepeated, domain.EventIncidentPostmortemDrafted, domain.EventIncidentPostmortemFailed,
		domain.EventIncidentPostmortemSentBack, domain.EventIncidentPostmortemApproved:
		return false
	}
	return kind.AboutIncident()
}

// timelineOf tells the events, newest maxPostmortemTimeline, and says
// how many older ones it left out.
func timelineOf(events []domain.Event, incident domain.Incident) ([]PostmortemTimelineEntry, int) {
	tasks := attachedTasks(incident)
	timeline := []PostmortemTimelineEntry{}
	for _, event := range events {
		if !inTimeline(event.Type) || expiredDecision(event) {
			continue
		}
		entry := PostmortemTimelineEntry{
			At: event.CreatedAt, Event: event.Type.String(), TaskID: event.TaskID.String(), Actor: firstText(event, "decidedBy", "approvedBy", "by"),
			Decision: payloadText(event, "decision"), Tool: payloadText(event, "tool"), Status: payloadText(event, "status"), From: payloadText(event, "from"), Verdict: payloadText(event, "verdict"),
		}
		if event.Type == domain.EventIncidentResolved {
			// The one reason that is a person's or the source's words: a
			// failure's holds a runtime's stderr, which may hold a secret.
			if reason := payloadText(event, "reason"); reason != "" {
				entry.Reason = domain.IncidentText(reason, maxPostmortemEntryText)
			}
		}
		if attached, ok := tasks[event.TaskID]; ok {
			entry.Phase, entry.Cycle, entry.BotID = attached.Phase().String(), attached.Cycle(), attached.BotID().String()
		}
		timeline = append(timeline, entry)
	}
	omitted := max(len(timeline)-maxPostmortemTimeline, 0)
	return timeline[omitted:], omitted
}

// decisionsOf reads who decided what from the decision events; the
// tool of a call comes from the wait that opened it.
func decisionsOf(events []domain.Event, incident domain.Incident) []PostmortemDecision {
	tasks := attachedTasks(incident)
	tools := map[string]string{}
	decisions := []PostmortemDecision{}
	for _, event := range events {
		call := event.TaskID.String() + "\x00" + payloadText(event, "callId")
		if tool := payloadText(event, "tool"); tool != "" {
			tools[call] = tool
		}
		decision := PostmortemDecision{At: event.CreatedAt, TaskID: event.TaskID.String(), Tool: tools[call]}
		switch event.Type {
		case domain.EventTaskDecided:
			if expiredDecision(event) {
				continue
			}
			decision.Outcome, decision.By = domain.ApprovalDenied, payloadText(event, "decidedBy")
			if payloadText(event, "decision") == DecisionApprove {
				decision.Outcome = domain.ApprovalApproved
			}
		case domain.EventApprovalFirst:
			decision.Outcome, decision.By = "first approval", payloadText(event, "approvedBy")
		case domain.EventApprovalExpired:
			decision.Outcome = domain.ApprovalExpired
		default:
			continue
		}
		if attached, ok := tasks[event.TaskID]; ok {
			decision.Phase, decision.Cycle = attached.Phase().String(), attached.Cycle()
		}
		decisions = append(decisions, decision)
	}
	return decisions
}

// expiredDecision reports the decided event of a call whose deadline
// denied it: nobody decided it, and its approval.expired says so.
func expiredDecision(event domain.Event) bool {
	return event.Type == domain.EventTaskDecided && payloadText(event, "outcome") == domain.ApprovalExpired
}

func attachedTasks(incident domain.Incident) map[domain.TaskID]domain.IncidentTask {
	tasks := map[domain.TaskID]domain.IncidentTask{}
	for _, task := range incident.Tasks() {
		tasks[task.TaskID()] = task
	}
	return tasks
}

// payloadText is a payload's value when it is text.
func payloadText(event domain.Event, key string) string {
	text, _ := event.Payload[key].(string)
	return text
}

func firstText(event domain.Event, keys ...string) string {
	for _, key := range keys {
		if text := payloadText(event, key); text != "" {
			return text
		}
	}
	return ""
}

// oneLine keeps a value on the line it is written on.
func oneLine(text string) string { return strings.Join(strings.Fields(text), " ") }

// timelineLines writes the timeline for the objective, one entry a line.
func timelineLines(timeline []PostmortemTimelineEntry) []string {
	lines := make([]string, 0, len(timeline))
	for _, entry := range timeline {
		line := []string{entry.At.UTC().Format(postmortemTimeLayout), entry.Event}
		for _, part := range [][2]string{{"phase", entry.Phase}, {"bot", entry.BotID}, {"by", entry.Actor}, {"decision", entry.Decision}, {"tool", entry.Tool}, {"status", entry.Status}, {"verdict", entry.Verdict}} {
			if part[1] != "" {
				line = append(line, part[0]+"="+oneLine(part[1]))
			}
		}
		if entry.Reason != "" {
			line = append(line, "reason: "+oneLine(entry.Reason))
		}
		lines = append(lines, strings.Join(line, " "))
	}
	return lines
}

// decisionLines writes who decided what for the objective.
func decisionLines(decisions []PostmortemDecision) []string {
	if len(decisions) == 0 {
		return []string{"No call waited for a decision."}
	}
	lines := make([]string, 0, len(decisions))
	for _, decision := range decisions {
		line := decision.At.UTC().Format(postmortemTimeLayout) + " " + cmp.Or(decision.Phase, "task") + " " + oneLine(cmp.Or(decision.Tool, "call")) + ": " + decision.Outcome
		if decision.By != "" {
			line += " by " + oneLine(decision.By)
		}
		lines = append(lines, line)
	}
	return lines
}

// verdictParts writes verify's verdicts for the objective, one part a
// verdict.
func verdictParts(verdicts []PostmortemVerdict) []string {
	if len(verdicts) == 0 {
		return []string{"no verify task ran"}
	}
	parts := make([]string, 0, len(verdicts))
	for _, verdict := range verdicts {
		parts = append(parts, "cycle "+strconv.Itoa(verdict.Cycle)+": "+verdict.Verdict)
	}
	return parts
}

// costLine writes the incident's cost for the objective.
func costLine(usd float64, unknown bool) string {
	line := "$" + strconv.FormatFloat(usd, 'f', 4, 64)
	if unknown {
		line += " at least: a run did not report"
	}
	return line
}
