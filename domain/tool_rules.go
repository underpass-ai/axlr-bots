package domain

// ToolRules narrows a bot's allowed tools by their arguments: the allow
// rules say which calls of an allowed tool may run (a tool without an
// allow rule is allowed as a whole), the deny rules say which calls never
// run, whatever the allow rules say. Every allow rule must name a tool
// in the bot's allow-list; a deny rule may name anything.
type ToolRules struct {
	allow []ToolRule
	deny  []ToolRule
}

// NewToolRules validates the rules against the bot's allowed tools.
func NewToolRules(allow, deny []ToolRule, allowed AllowedTools) (ToolRules, error) {
	rules := ToolRules{}
	for _, rule := range allow {
		if !allowed.Allows(rule.Tool()) {
			return ToolRules{}, invalid("tool rules", "allow rule names "+rule.Tool().String()+", which is not an allowed tool")
		}
		rules.allow = append(rules.allow, rule)
	}
	rules.deny = append(rules.deny, deny...)
	return rules, nil
}

// Allow returns the allow rules, copied.
func (r ToolRules) Allow() []ToolRule { return append([]ToolRule(nil), r.allow...) }

// Deny returns the deny rules, copied.
func (r ToolRules) Deny() []ToolRule { return append([]ToolRule(nil), r.deny...) }

// IsEmpty reports rules that narrow nothing.
func (r ToolRules) IsEmpty() bool { return len(r.allow) == 0 && len(r.deny) == 0 }

// Constrains reports whether an allow rule narrows the tool's arguments.
func (r ToolRules) Constrains(tool ToolName) bool {
	for _, rule := range r.allow {
		if rule.Tool() == tool {
			return true
		}
	}
	return false
}

// Snapshot returns the rules as plain data.
func (r ToolRules) Snapshot() ToolRulesSnapshot {
	var s ToolRulesSnapshot
	for _, rule := range r.allow {
		s.Allow = append(s.Allow, rule.Snapshot())
	}
	for _, rule := range r.deny {
		s.Deny = append(s.Deny, rule.Snapshot())
	}
	return s
}

// RestoreToolRules rebuilds rules from a snapshot, validating each.
func RestoreToolRules(s ToolRulesSnapshot, allowed AllowedTools) (ToolRules, error) {
	allow, err := restoreRules(s.Allow)
	if err != nil {
		return ToolRules{}, err
	}
	deny, err := restoreRules(s.Deny)
	if err != nil {
		return ToolRules{}, err
	}
	return NewToolRules(allow, deny, allowed)
}

func restoreRules(snapshots []ToolRuleSnapshot) ([]ToolRule, error) {
	rules := make([]ToolRule, 0, len(snapshots))
	for _, s := range snapshots {
		rule, err := NewToolRule(s.Tool, s.Arguments)
		if err != nil {
			return nil, err
		}
		rules = append(rules, rule)
	}
	return rules, nil
}
