package domain

import (
	"strconv"
	"time"
)

// The transitions after a proposal (decided 8 November 2026): a person's
// approval of the apply task's call moves Proposed to Applying, the apply
// task's completion moves Applying to Verifying, and the source's
// resolution closes the incident as it does from any status. A denial or
// an expiry of that approval, a failed apply and a verify that says the
// alert is still red send the incident back to Open: one more cycle, the
// diagnosis and the proposal kept until a new triage replaces them, and
// the next alert of the group starts that triage. The IncidentMaxDenials-th
// denial escalates it instead.

// Denials counts the applies of the incident's proposals a person denied
// or let expire since it was last reopened.
func (i Incident) Denials() int { return i.denials }

// StartApplying records that a person approved the apply task's call:
// Proposed becomes Applying.
func (i *Incident) StartApplying(now time.Time) error {
	if i.status != IncidentProposed || !i.HasTask(IncidentPhaseApply) {
		return i.refuse("start applying")
	}
	i.status = IncidentApplying
	i.updatedAt = now
	return nil
}

// Verify records that the apply task completed: Applying becomes
// Verifying.
func (i *Incident) Verify(now time.Time) error {
	if i.status != IncidentApplying || !i.HasTask(IncidentPhaseApply) {
		return i.refuse("verify")
	}
	i.status = IncidentVerifying
	i.updatedAt = now
	return nil
}

// Deny records that a person denied the apply task's call, or let its
// approval expire, while the incident was Proposed: it goes back to Open
// in a new cycle with the reason, or, at the IncidentMaxDenials-th
// denial, is escalated to a person.
func (i *Incident) Deny(reason string, now time.Time) error {
	if i.status != IncidentProposed {
		return i.refuse("deny the apply")
	}
	text, err := validateText("reason", reason, MaxIncidentReasonLength)
	if err != nil {
		return err
	}
	i.denials++
	if i.denials >= IncidentMaxDenials {
		suffix := " (denied " + strconv.Itoa(i.denials) + " times: left to a person)"
		i.reason = IncidentText(text, MaxIncidentReasonLength-len(suffix)) + suffix
		i.status = IncidentEscalated
		i.updatedAt = now
		return nil
	}
	i.sendBack(text, now)
	return nil
}

// SendBack records that the apply failed, or that the verify task said
// the alert is still red: Proposed, Applying or Verifying goes back to
// Open in a new cycle with the reason.
func (i *Incident) SendBack(reason string, now time.Time) error {
	if !i.status.HasProposal() {
		return i.refuse("send back to Open")
	}
	text, err := validateText("reason", reason, MaxIncidentReasonLength)
	if err != nil {
		return err
	}
	i.sendBack(text, now)
	return nil
}

func (i *Incident) sendBack(reason string, now time.Time) {
	i.reason = reason
	i.cycle++
	i.status = IncidentOpen
	i.updatedAt = now
}
