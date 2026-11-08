package domain

import "strings"

// VerifyVerdict is what a verify task's answer says of the applied
// change: the line `VERDICT: red` says the alert is still red, `VERDICT:
// green` that the change took. An answer without the line says nothing:
// the incident waits in Verifying for the source to resolve it.
type VerifyVerdict string

const (
	// VerdictNone is an answer that names no verdict.
	VerdictNone VerifyVerdict = ""
	// VerdictGreen says the change took.
	VerdictGreen VerifyVerdict = "green"
	// VerdictRed says the alert is still red after the change.
	VerdictRed VerifyVerdict = "red"
)

// verdictPrefix starts the verdict line of a verify task's answer.
const verdictPrefix = "VERDICT:"

// ParseVerifyVerdict reads the first verdict line of an answer, in any
// case and with any spaces around it; a line that names neither red nor
// green is no verdict.
func ParseVerifyVerdict(answer string) VerifyVerdict {
	for _, line := range strings.Split(answer, "\n") {
		line = strings.TrimSpace(line)
		if len(line) < len(verdictPrefix) || !strings.EqualFold(line[:len(verdictPrefix)], verdictPrefix) {
			continue
		}
		word := strings.ToLower(strings.Trim(strings.TrimSpace(line[len(verdictPrefix):]), ".*`'\""))
		switch VerifyVerdict(word) {
		case VerdictGreen, VerdictRed:
			return VerifyVerdict(word)
		}
		return VerdictNone
	}
	return VerdictNone
}

// IsRed reports a verdict that sends the incident back to Open.
func (v VerifyVerdict) IsRed() bool { return v == VerdictRed }

func (v VerifyVerdict) String() string { return string(v) }
