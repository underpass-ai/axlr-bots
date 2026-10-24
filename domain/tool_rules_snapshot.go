package domain

// ToolRulesSnapshot is a bot's tool rules as plain data.
type ToolRulesSnapshot struct {
	Allow []ToolRuleSnapshot `json:"allow,omitempty"`
	Deny  []ToolRuleSnapshot `json:"deny,omitempty"`
}
