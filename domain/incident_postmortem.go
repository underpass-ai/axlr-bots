package domain

import (
	"strconv"
	"time"
)

const (
	// MaxIncidentPostmortemLength bounds a postmortem's Markdown, in
	// bytes: what a task wrote is cut to fit with IncidentText.
	MaxIncidentPostmortemLength = 32000
	// IncidentMaxPostmortemSendBacks is how many times a person may send
	// a postmortem back: each one but the last launches its task again,
	// the last leaves it to a person, as IncidentMaxSendBacks does for a
	// proposal. It has its own count: a reopening resets the incident's.
	IncidentMaxPostmortemSendBacks = 2
)

// IncidentPostmortem is what a resolved incident's postmortem task wrote
// and where it stands with the people who review it. It belongs to one
// cycle of the incident: a reopening leaves it as it was, no longer
// reviewable, until the next resolution writes another.
type IncidentPostmortem struct {
	state      IncidentPostmortemState
	cycle      int
	task       TaskID
	bot        BotID
	author     TaskID
	markdown   string
	note       string
	reviewedBy string
	reviewedAt *time.Time
	reason     string
	sendBacks  int
	approvedBy string
	approvedAt *time.Time
}

// State is where the postmortem is.
func (p IncidentPostmortem) State() IncidentPostmortemState { return p.state }

// Cycle is the incident's cycle it was written for.
func (p IncidentPostmortem) Cycle() int { return p.cycle }

// TaskID is the last task launched to write it.
func (p IncidentPostmortem) TaskID() TaskID { return p.task }

// BotID is the bot that took the postmortem phase: whose approvers
// review it.
func (p IncidentPostmortem) BotID() BotID { return p.bot }

// AuthorTaskID is the task whose answer Markdown is; empty before one
// answered.
func (p IncidentPostmortem) AuthorTaskID() TaskID { return p.author }

// Markdown is the postmortem's text; empty before a task answered.
func (p IncidentPostmortem) Markdown() string { return p.markdown }

// Note is what the person who last sent it back asked for.
func (p IncidentPostmortem) Note() string { return p.note }

// ReviewedBy is who last sent it back.
func (p IncidentPostmortem) ReviewedBy() string { return p.reviewedBy }

// SendBacks counts the times a person sent it back.
func (p IncidentPostmortem) SendBacks() int { return p.sendBacks }

// ApprovedBy is who approved it; empty until someone did.
func (p IncidentPostmortem) ApprovedBy() string { return p.approvedBy }

// Reason says why its last task did not write it.
func (p IncidentPostmortem) Reason() string { return p.reason }

// Snapshot returns the postmortem as plain data.
func (p IncidentPostmortem) Snapshot() IncidentPostmortemSnapshot {
	return IncidentPostmortemSnapshot{
		State: p.state.String(), Cycle: p.cycle, TaskID: p.task.String(), BotID: p.bot.String(), AuthorTaskID: p.author.String(), Markdown: p.markdown,
		Note: p.note, ReviewedBy: p.reviewedBy, ReviewedAt: copyTime(p.reviewedAt), Reason: p.reason, SendBacks: p.sendBacks,
		ApprovedBy: p.approvedBy, ApprovedAt: copyTime(p.approvedAt),
	}
}

func copyTime(at *time.Time) *time.Time {
	if at == nil {
		return nil
	}
	copied := *at
	return &copied
}

// restoreIncidentPostmortem rebuilds a postmortem from a snapshot, for an
// incident at the cycle given.
func restoreIncidentPostmortem(s IncidentPostmortemSnapshot, cycle int) (IncidentPostmortem, error) {
	state, err := ParseIncidentPostmortemState(s.State)
	if err != nil {
		return IncidentPostmortem{}, err
	}
	task, err := NewTaskID(s.TaskID)
	if err != nil {
		return IncidentPostmortem{}, err
	}
	bot, err := NewBotID(s.BotID)
	if err != nil {
		return IncidentPostmortem{}, err
	}
	p := IncidentPostmortem{
		state: state, cycle: s.Cycle, task: task, bot: bot, reviewedBy: s.ReviewedBy, reviewedAt: copyTime(s.ReviewedAt),
		sendBacks: s.SendBacks, approvedBy: s.ApprovedBy, approvedAt: copyTime(s.ApprovedAt),
	}
	switch {
	case s.Cycle < 1 || s.Cycle > cycle:
		return IncidentPostmortem{}, invalid("postmortem cycle", "must be one of the incident's")
	case s.SendBacks < 0 || s.SendBacks > IncidentMaxPostmortemSendBacks:
		return IncidentPostmortem{}, invalid("postmortem sendBacks", "must be 0 to "+strconv.Itoa(IncidentMaxPostmortemSendBacks))
	case (state == PostmortemDraft || state == PostmortemApproved) && (s.Markdown == "" || s.AuthorTaskID == ""):
		return IncidentPostmortem{}, invalid("postmortem", "a "+state.String()+" postmortem needs its text and its author task")
	case state == PostmortemApproved && (s.ApprovedBy == "" || s.ApprovedAt == nil):
		return IncidentPostmortem{}, invalid("postmortem", "an Approved postmortem says who approved it and when")
	case state != PostmortemApproved && (s.ApprovedBy != "" || s.ApprovedAt != nil):
		return IncidentPostmortem{}, invalid("postmortem", "only an Approved postmortem has an approver")
	}
	if s.AuthorTaskID != "" {
		if p.author, err = NewTaskID(s.AuthorTaskID); err != nil {
			return IncidentPostmortem{}, err
		}
	}
	if s.Markdown != "" {
		if p.markdown, err = validateText("postmortem", s.Markdown, MaxIncidentPostmortemLength); err != nil {
			return IncidentPostmortem{}, err
		}
	}
	if s.Note != "" {
		if p.note, err = validateText("postmortem note", s.Note, MaxIncidentReasonLength); err != nil {
			return IncidentPostmortem{}, err
		}
	}
	if s.Reason != "" {
		if p.reason, err = validateText("postmortem reason", s.Reason, MaxIncidentReasonLength); err != nil {
			return IncidentPostmortem{}, err
		}
	}
	return p, nil
}
