package domain

import "strings"

const maxRunbookNameLength = 128

// RunbookName names a runbook on an autonomy ladder: the alert name of
// the incidents it covers (Alertmanager's alertname, CloudWatch's alarm
// name), which is what an incident already carries.
type RunbookName string

// NewRunbookName validates a name: 1 to 128 printable characters without
// spaces, slashes or "..".
func NewRunbookName(raw string) (RunbookName, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", invalid("runbook name", "must not be empty")
	}
	if len(value) > maxRunbookNameLength {
		return "", invalid("runbook name", "must be at most 128 characters")
	}
	for _, r := range value {
		if r <= ' ' || r == 0x7f || r == '/' || r == '\\' {
			return "", invalid("runbook name", "must not contain spaces, slashes or control characters")
		}
	}
	if strings.Contains(value, "..") {
		return "", invalid("runbook name", "must not contain ..")
	}
	return RunbookName(value), nil
}

func (n RunbookName) String() string { return string(n) }

// cloudWatchSource is the source of an incident a cloudwatch trigger
// opened: its title is "<AlarmName>: <reason>".
const cloudWatchSource = "cloudwatch"

// RunbookOf is the runbook an incident's title names, from its source:
// "<AlarmName>: <reason>" for CloudWatch, and for every other source
// "<alertname> in <namespace>" or "<alertname>" (AlertmanagerPayload.Title
// writes them). False for a title that names none: one that holds no
// valid name (a group key with spaces) matches no ladder, so the apply
// keeps its card and the cycle counts nothing.
func RunbookOf(source, title string) (RunbookName, bool) {
	separator := " in "
	if source == cloudWatchSource {
		separator = ": "
	}
	name, _, _ := strings.Cut(title, separator)
	runbook, err := NewRunbookName(name)
	return runbook, err == nil
}
