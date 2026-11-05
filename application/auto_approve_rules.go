package application

import (
	"bytes"
	"encoding/json"
	"errors"

	"github.com/underpass-ai/axlr-bots/domain"
)

// AutoApproveRules is a patch's list of the calls the control plane
// approves without a person. Each item is a rule ({"tool": …,
// "arguments": {…}}) or, for short, a tool name: a name is the rule
// with no arguments, every call of that tool. It is written back as
// rules.
type AutoApproveRules []domain.ToolRuleSnapshot

// errAutoApproveShape shows the shapes autoApprove takes.
var errAutoApproveShape = errors.New(`autoApprove is a list of tool names or rules, e.g. ["local_exec"] or [{"tool": "local_exec", "arguments": {"program": "systemctl"}}]`)

// UnmarshalJSON takes each item as a tool name or a rule; a rule with a
// field it does not know is refused, as the rest of the body's are.
func (r *AutoApproveRules) UnmarshalJSON(data []byte) error {
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil {
		return errAutoApproveShape
	}
	rules := make(AutoApproveRules, 0, len(items))
	for _, item := range items {
		var name string
		if err := json.Unmarshal(item, &name); err == nil {
			rules = append(rules, domain.ToolRuleSnapshot{Tool: name})
			continue
		}
		if len(bytes.TrimSpace(item)) == 0 || bytes.TrimSpace(item)[0] != '{' {
			return errAutoApproveShape
		}
		decoder := json.NewDecoder(bytes.NewReader(item))
		decoder.DisallowUnknownFields()
		var rule domain.ToolRuleSnapshot
		if err := decoder.Decode(&rule); err != nil {
			return errors.Join(errAutoApproveShape, err)
		}
		rules = append(rules, rule)
	}
	*r = rules
	return nil
}
