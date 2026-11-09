package application

// JudgeQuestion is one choice put to Jev on a node's behalf.
type JudgeQuestion struct {
	State    string
	Question string
	Options  []string
}
