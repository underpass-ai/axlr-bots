package domain

// IncidentPostmortemState is where a resolved incident's postmortem is:
// its task writing it, its draft waiting for a person, sent back, failed,
// left to a person or approved. It is not the incident's status: the
// incident stays Resolved through all of them, and nothing that happens
// to a postmortem reopens or escalates it.
type IncidentPostmortemState string

const (
	// PostmortemWriting is a postmortem whose task is pending or running.
	PostmortemWriting IncidentPostmortemState = "Writing"
	// PostmortemDraft is a postmortem whose task answered: a person
	// approves it or sends it back.
	PostmortemDraft IncidentPostmortemState = "Draft"
	// PostmortemSentBack is a postmortem a person sent back, or asked for
	// when none started, whose next task has not started: the launch
	// failed or is about to happen.
	PostmortemSentBack IncidentPostmortemState = "SentBack"
	// PostmortemFailed is a postmortem whose task failed, expired or was
	// cancelled: a person may send it back to try again.
	PostmortemFailed IncidentPostmortemState = "Failed"
	// PostmortemLeftToPerson is a postmortem sent back
	// IncidentMaxPostmortemSendBacks times: no task is launched again, and
	// its last draft, if it has one, may still be approved.
	PostmortemLeftToPerson IncidentPostmortemState = "LeftToPerson"
	// PostmortemApproved is a postmortem a person approved.
	PostmortemApproved IncidentPostmortemState = "Approved"
)

// ParseIncidentPostmortemState validates a postmortem's state.
func ParseIncidentPostmortemState(raw string) (IncidentPostmortemState, error) {
	switch IncidentPostmortemState(raw) {
	case PostmortemWriting, PostmortemDraft, PostmortemSentBack, PostmortemFailed, PostmortemLeftToPerson, PostmortemApproved:
		return IncidentPostmortemState(raw), nil
	}
	return "", invalid("postmortem state", "must be Writing, Draft, SentBack, Failed, LeftToPerson or Approved")
}

func (s IncidentPostmortemState) String() string { return string(s) }
