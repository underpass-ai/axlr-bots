package domain

// RuntimeSnapshot is a bot's runtime as it is stored and as the operator
// writes it: its kind and, for jev, the program.
type RuntimeSnapshot struct {
	Kind string              `json:"kind"`
	Jev  *JevProgramSnapshot `json:"jev,omitempty"`
}

// JevProgramSnapshot is a Jev bot's program: the MCP server on the node
// that serves its tools, the tool that starts a task, the tool that acts
// on each of Jev's choices, the tool that waits for the next question,
// the tool a stop.* task calls, and the Jev model.
type JevProgramSnapshot struct {
	Server string         `json:"server"`
	Start  string         `json:"start"`
	Act    JevActSnapshot `json:"act"`
	Wait   string         `json:"wait"`
	Stop   string         `json:"stop,omitempty"`
	Model  string         `json:"model,omitempty"`
}

// JevActSnapshot is how a Jev bot acts on a choice: the tool, the
// argument that takes the choice (its first whole number, else its
// text), the argument that takes Jev's verdict {choice, probability,
// confidence}, and the fields of the event's data passed on as they are.
type JevActSnapshot struct {
	Tool    string   `json:"tool"`
	Choice  string   `json:"choice"`
	Verdict string   `json:"verdict,omitempty"`
	Pass    []string `json:"pass,omitempty"`
}
