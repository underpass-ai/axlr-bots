package domain

import "time"

// The postmortem of a resolved incident (decided 8 November 2026). The
// design's state diagram draws Resolved -> Postmortem; here the incident
// stays Resolved and the postmortem is a value of its own, with its own
// states: every reader of Resolved (the reopen window, a status filter,
// an output) stays right, and no failure of the postmortem can touch the
// incident (docs/architecture/incident-postmortem.md).

// Postmortem is the incident's postmortem, and false before one started.
func (i Incident) Postmortem() (IncidentPostmortem, bool) {
	if i.postmortem == nil {
		return IncidentPostmortem{}, false
	}
	return *i.postmortem, true
}

// PostmortemDue reports a resolved incident a postmortem task should be
// launched for now: none was for this cycle, or a person sent the last
// one back. Whether its severity qualifies is the trigger's to say.
func (i Incident) PostmortemDue() bool {
	if !i.status.IsResolved() {
		return false
	}
	return i.postmortem == nil || i.postmortem.cycle < i.cycle || i.postmortem.state == PostmortemSentBack
}

// AttachPostmortemTask records the task launched to write the postmortem:
// the first of a cycle replaces the postmortem of an earlier one, and one
// after a send-back keeps the draft it rewrites and the count. Unlike the
// other phases a cycle may hold several, one per send-back.
func (i *Incident) AttachPostmortemTask(task TaskID, bot BotID, now time.Time) error {
	if !i.PostmortemDue() {
		return i.refuse("attach a postmortem task")
	}
	if err := i.attach(task, bot, IncidentPhasePostmortem, now); err != nil {
		return err
	}
	postmortem := IncidentPostmortem{cycle: i.cycle}
	if i.postmortem != nil && i.postmortem.cycle == i.cycle {
		postmortem = *i.postmortem
	}
	postmortem.state, postmortem.task, postmortem.bot, postmortem.reason = PostmortemWriting, task, bot, ""
	i.postmortem = &postmortem
	return nil
}

// reviewable is a copy of the postmortem of the current cycle of a
// resolved incident, put in its place so that moving it never reaches
// another copy of the incident, when it is in one of the states; refused
// otherwise under the attempt's name.
func (i *Incident) reviewable(attempted string, states ...IncidentPostmortemState) (*IncidentPostmortem, error) {
	if !i.status.IsResolved() || i.postmortem == nil || i.postmortem.cycle != i.cycle {
		return nil, i.refuse(attempted)
	}
	for _, state := range states {
		if i.postmortem.state == state {
			postmortem := *i.postmortem
			i.postmortem = &postmortem
			return i.postmortem, nil
		}
	}
	return nil, StateError{Subject: "postmortem of incident " + i.id.String(), Current: i.postmortem.state.String(), Attempted: attempted}
}

// RecordPostmortem stores what the postmortem's task answered, as
// Markdown with its author: Writing becomes Draft. Only the task the
// postmortem is waiting for may write it.
func (i *Incident) RecordPostmortem(task TaskID, markdown string, now time.Time) error {
	postmortem, err := i.reviewable("record a postmortem", PostmortemWriting)
	if err != nil {
		return err
	}
	if postmortem.task != task {
		return StateError{Subject: "postmortem of incident " + i.id.String(), Current: "written by task " + postmortem.task.String(), Attempted: "record task " + task.String()}
	}
	text, err := validateText("postmortem", markdown, MaxIncidentPostmortemLength)
	if err != nil {
		return err
	}
	postmortem.state, postmortem.author, postmortem.markdown, postmortem.reason = PostmortemDraft, task, text, ""
	i.updatedAt = now
	return nil
}

// FailPostmortem records that the postmortem's task failed, expired or
// was cancelled: Writing becomes Failed, with the reason. The incident
// stays as it is, and the draft a rewrite was replacing is kept.
func (i *Incident) FailPostmortem(task TaskID, reason string, now time.Time) error {
	postmortem, err := i.reviewable("fail a postmortem", PostmortemWriting)
	if err != nil {
		return err
	}
	if postmortem.task != task {
		return StateError{Subject: "postmortem of incident " + i.id.String(), Current: "written by task " + postmortem.task.String(), Attempted: "fail task " + task.String()}
	}
	text, err := validateText("postmortem reason", reason, MaxIncidentReasonLength)
	if err != nil {
		return err
	}
	postmortem.state, postmortem.reason = PostmortemFailed, text
	i.updatedAt = now
	return nil
}

// checkRead refuses an answer given on another text than the one the
// postmortem holds: read names the task whose text the person read, empty
// when they read none.
func (i *Incident) checkRead(postmortem *IncidentPostmortem, read TaskID, attempted string) error {
	if read == postmortem.author {
		return nil
	}
	current := "holding no text"
	if postmortem.author != "" {
		current = "holding the text of task " + postmortem.author.String()
	}
	return StateError{Subject: "postmortem of incident " + i.id.String(), Current: current, Attempted: attempted + " on the text of task " + read.String()}
}

// ApprovePostmortem records that the person approved the text of the
// task read: a Draft, or the last draft of one left to a person. A text
// they did not read is refused, a StateError.
func (i *Incident) ApprovePostmortem(actor string, read TaskID, now time.Time) error {
	postmortem, err := i.reviewable("approve the postmortem", PostmortemDraft, PostmortemLeftToPerson)
	if err != nil {
		return err
	}
	if postmortem.markdown == "" {
		return StateError{Subject: "postmortem of incident " + i.id.String(), Current: "without a draft", Attempted: "approve the postmortem"}
	}
	if err := i.checkRead(postmortem, read, "approve the postmortem"); err != nil {
		return err
	}
	at := now
	postmortem.state, postmortem.approvedBy, postmortem.approvedAt = PostmortemApproved, actor, &at
	i.updatedAt = now
	return nil
}

// SendBackPostmortem records the person's note on the text of the task
// read. A Draft counts: it becomes SentBack for the control plane to
// launch its task again, or LeftToPerson at the
// IncidentMaxPostmortemSendBacks-th time. A Failed one becomes SentBack
// and does not count, nobody reviewed a text; nor does one already
// SentBack, whose launch did not start: the note is replaced. A resolved
// incident with no postmortem of this cycle gets a request for one, the
// way out when its first launch could not start.
func (i *Incident) SendBackPostmortem(actor, note string, read TaskID, now time.Time) error {
	if !i.status.IsResolved() {
		return i.refuse("send the postmortem back")
	}
	text, err := validateText("postmortem note", note, MaxIncidentReasonLength)
	if err != nil {
		return err
	}
	var postmortem *IncidentPostmortem
	if i.postmortem == nil || i.postmortem.cycle < i.cycle {
		postmortem = &IncidentPostmortem{cycle: i.cycle}
		if read != "" {
			return i.checkRead(postmortem, read, "send the postmortem back")
		}
		i.postmortem = postmortem
	} else {
		if postmortem, err = i.reviewable("send the postmortem back", PostmortemDraft, PostmortemFailed, PostmortemSentBack); err != nil {
			return err
		}
		if err := i.checkRead(postmortem, read, "send the postmortem back"); err != nil {
			return err
		}
	}
	counts := postmortem.state == PostmortemDraft
	if counts {
		postmortem.sendBacks++
	}
	at := now
	postmortem.note, postmortem.reviewedBy, postmortem.reviewedAt = text, actor, &at
	postmortem.state = PostmortemSentBack
	if counts && postmortem.sendBacks >= IncidentMaxPostmortemSendBacks {
		postmortem.state = PostmortemLeftToPerson
	}
	i.updatedAt = now
	return nil
}
