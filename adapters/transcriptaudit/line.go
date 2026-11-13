package transcriptaudit

// line is one message of a session as one compact JSON line, with what
// it answers when it is a tool's result (the call's tool and what it
// named: a path or a program and its arguments), so a piece of it cut
// away from the call still says whose result it is.
type line struct {
	text  string
	label string
}
