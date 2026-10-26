package domain

import (
	"bytes"
	"encoding/json"
	"sort"
	"unicode"
	"unicode/utf8"
)

// ToolRule names a tool and, optionally, patterns its arguments must
// match: a glob over each argument's value as text, where * matches
// within a path segment, ** across segments and ? one character (the
// runtime contract's "Policy per execution"). A rule with patterns
// matches only a call that carries every named argument with a matching
// value, so {"tool": "local_exec", "arguments": {"program": "journalctl"}}
// allows journalctl and nothing else through local_exec.
type ToolRule struct {
	tool      ToolName
	arguments map[string]string
}

const (
	maxToolRuleArgumentKey = 64
	maxToolRulePattern     = 256
)

// NewToolRule validates a rule: a tool name, and argument names of 1 to
// 64 printable characters with patterns of 1 to 256.
func NewToolRule(tool string, arguments map[string]string) (ToolRule, error) {
	name, err := NewToolName(tool)
	if err != nil {
		return ToolRule{}, err
	}
	rule := ToolRule{tool: name}
	if len(arguments) > 0 {
		rule.arguments = make(map[string]string, len(arguments))
	}
	for key, pattern := range arguments {
		if key == "" || len(key) > maxToolRuleArgumentKey || !printable(key) {
			return ToolRule{}, invalid("tool rule "+tool, "an argument name is 1 to 64 printable characters")
		}
		if pattern == "" || len(pattern) > maxToolRulePattern || !printable(pattern) {
			return ToolRule{}, invalid("tool rule "+tool, "the pattern of "+key+" is 1 to 256 printable characters")
		}
		rule.arguments[key] = pattern
	}
	return rule, nil
}

// Tool is the tool the rule names.
func (r ToolRule) Tool() ToolName { return r.tool }

// Arguments returns the argument patterns, copied; nil when the rule
// names the tool as a whole.
func (r ToolRule) Arguments() map[string]string {
	if r.arguments == nil {
		return nil
	}
	out := make(map[string]string, len(r.arguments))
	for k, v := range r.arguments {
		out[k] = v
	}
	return out
}

// ArgumentNames lists the argument names the rule constrains, sorted.
func (r ToolRule) ArgumentNames() []string {
	names := make([]string, 0, len(r.arguments))
	for k := range r.arguments {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// Snapshot returns the rule as plain data.
func (r ToolRule) Snapshot() ToolRuleSnapshot {
	return ToolRuleSnapshot{Tool: r.tool.String(), Arguments: r.Arguments()}
}

func printable(text string) bool {
	if !utf8.ValidString(text) {
		return false
	}
	for _, c := range text {
		if unicode.IsControl(c) {
			return false
		}
	}
	return true
}

// Matches reports whether a call of tool with these arguments (a JSON
// object) fits the rule: the tool is the rule's, and every argument the
// rule names is in the call with a value that fits its pattern, a string
// as it is and anything else as its compact JSON.
func (r ToolRule) Matches(tool string, arguments json.RawMessage) bool {
	if tool != r.tool.String() {
		return false
	}
	if len(r.arguments) == 0 {
		return true
	}
	var values map[string]json.RawMessage
	if err := json.Unmarshal(arguments, &values); err != nil {
		return false
	}
	for key, pattern := range r.arguments {
		raw, ok := values[key]
		if !ok {
			return false
		}
		var text string
		if err := json.Unmarshal(raw, &text); err != nil {
			var compact bytes.Buffer
			if json.Compact(&compact, raw) != nil {
				return false
			}
			text = compact.String()
		}
		if !globMatches(pattern, text) {
			return false
		}
	}
	return true
}
