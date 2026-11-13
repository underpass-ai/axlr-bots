package transcriptaudit

import (
	"strings"
	"testing"
)

func TestEvidenceFindsTheLinesThatShowAFailure(t *testing.T) {
	block := strings.Join([]string{
		`{"role":"assistant","content":"","tool_calls":[{"name":"local_exec","arguments":{"program":"checker"}}]}`,
		`{"role":"tool","content":"{\"exit_code\":0,\"stdout\":\"fine\"}"}`,
		`{"role":"tool","content":"forbidden by the execution's policy: the arguments of local_exec match no allow rule"}`,
		`{"role":"tool","content":"{\"ok\": false, \"refusal\": \"bad_plan\"}"}`,
		`{"role":"tool","content":"{\"exit_code\":2,\"stderr\":\"usage\"}"}`,
		strings.Repeat("x", 900) + ` Traceback (most recent call last) ` + strings.Repeat("é", 900),
	}, "\n")
	got := evidence(block)
	if len(got) != 4 {
		t.Fatalf("evidence = %q", got)
	}
	if !strings.Contains(got[0], "forbidden by the execution's policy") || !strings.Contains(got[1], `\"ok\": false`) || !strings.Contains(got[2], `exit_code\":2`) {
		t.Errorf("evidence = %q", got)
	}
	if len(got[3]) > evidenceBytes || !strings.Contains(got[3], "Traceback") {
		t.Errorf("long line: %d bytes %q", len(got[3]), got[3][:80])
	}
	many := strings.Repeat("error here\n", 20)
	if len(evidence(many)) != maxEvidence {
		t.Error("more than the bound")
	}
	if got := evidence(`{"role":"tool","content":"all good"}`); got == nil || len(got) != 0 {
		t.Errorf("clean = %#v", got)
	}
}
