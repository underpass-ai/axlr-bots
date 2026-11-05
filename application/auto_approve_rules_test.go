package application

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAutoApproveRulesTakeNamesOrRules(t *testing.T) {
	var patch BotPatch
	if err := json.Unmarshal([]byte(`{"autoApprove": ["local_exec", {"tool": "local_write", "arguments": {"path": "a/*"}}]}`), &patch); err != nil {
		t.Fatal(err)
	}
	rules := *patch.AutoApprove
	if len(rules) != 2 || rules[0].Tool != "local_exec" || rules[0].Arguments != nil || rules[1].Arguments["path"] != "a/*" {
		t.Errorf("rules = %+v", rules)
	}
	// Written back as rules.
	out, _ := json.Marshal(rules)
	if string(out) != `[{"tool":"local_exec"},{"tool":"local_write","arguments":{"path":"a/*"}}]` {
		t.Errorf("written = %s", out)
	}
	for _, bad := range []string{`{"autoApprove": "x"}`, `{"autoApprove": [true]}`, `{"autoApprove": [[]]}`, `{"autoApprove": [{"tool": "x", "other": 1}]}`} {
		if err := json.Unmarshal([]byte(bad), &BotPatch{}); err == nil || !strings.Contains(err.Error(), "list of tool names or rules") {
			t.Errorf("%s: %v", bad, err)
		}
	}
	var none BotPatch
	if err := json.Unmarshal([]byte(`{"autoApprove": null}`), &none); err != nil || none.AutoApprove != nil {
		t.Errorf("null: %v %v", err, none.AutoApprove)
	}
}
