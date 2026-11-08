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
// opened: it names no namespace, so it never reaches a unit.
const cloudWatchSource = "cloudwatch"

// runbookTitleSeparator joins an alert name and its namespace in a title.
const runbookTitleSeparator = " in "

// RunbookOf is the alert name a title carries, for the audit: what comes
// before the last " in " of "<alertname> in <namespace>", or the whole
// title. False for a CloudWatch title (it has no namespace) and for a
// name a runbook could not have. It decides nothing: only
// LadderRunbook.Titles matches a title against a declared runbook.
func RunbookOf(source, title string) (RunbookName, bool) {
	if source == cloudWatchSource {
		return "", false
	}
	name := title
	if at := strings.LastIndex(title, runbookTitleSeparator); at >= 0 {
		name = title[:at]
	}
	runbook, err := NewRunbookName(name)
	return runbook, err == nil
}
