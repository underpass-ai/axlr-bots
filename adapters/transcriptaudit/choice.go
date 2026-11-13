package transcriptaudit

// Choice is one question put to the judge: the state (a block, at most
// MaxStateBytes), the question and the options.
type Choice struct {
	State    string
	Question string
	Options  []string
}
