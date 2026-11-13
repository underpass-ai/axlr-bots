package domain

// PolicyRecord says under which policy an execution ran: the autonomy
// level (the bot's), the tool allow-list it was given, the level the runtime was asked for (the control plane
// translates when the runtime's levels differ), whether the runtime
// enforced the policy or the control plane only recorded it, the
// policy file handed to the runtime, kept as evidence, and the MCP
// manifest of the delegation tool when the run was given it.
type PolicyRecord struct {
	Level        string   `json:"level"`
	AllowedTools []string `json:"allowedTools,omitempty"`
	RuntimeLevel string   `json:"runtimeLevel,omitempty"`
	Enforced     bool     `json:"enforced"`
	Path         string   `json:"path,omitempty"`
	Delegation   string   `json:"delegation,omitempty"`
}
