package domain

// ToolRuleSnapshot is a tool rule as plain data: the tool and the
// argument patterns, as a bot definition writes them.
type ToolRuleSnapshot struct {
	Tool      string            `json:"tool"`
	Arguments map[string]string `json:"arguments,omitempty"`
}
