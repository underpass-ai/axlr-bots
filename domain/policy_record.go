package domain

// PolicyRecord says under which policy an execution ran: the bot's
// autonomy level, the level the runtime was asked for (the control plane
// translates when the runtime's levels differ), whether the runtime
// enforced the policy or the control plane only recorded it, and the
// policy file handed to the runtime, kept as evidence.
type PolicyRecord struct {
	Level        string `json:"level"`
	RuntimeLevel string `json:"runtimeLevel,omitempty"`
	Enforced     bool   `json:"enforced"`
	Path         string `json:"path,omitempty"`
}
