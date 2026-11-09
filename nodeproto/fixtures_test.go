package nodeproto

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/domain"
)

// updateFixtures rewrites testdata/fixtures from the messages below:
//
//	go test ./nodeproto -run TestFixtures -update
var updateFixtures = flag.Bool("update", false, "rewrite the nodeproto fixtures")

// fixture is one message as it travels, kept in testdata/fixtures so a
// client in another language (axlr-nanobot vendors them) can check it
// reads and writes the same JSON. skip names the fields left empty on
// purpose: the other branch of a message, or what the nanobot does not
// take part in (delegation, events).
type fixture struct {
	name    string
	message any
	skip    []string
}

var (
	fixtureAt    = time.Date(2026, 11, 7, 9, 30, 0, 0, time.UTC)
	fixtureLater = time.Date(2027, 11, 7, 9, 30, 0, 0, time.UTC)
)

func fixtureNode() domain.NodeSnapshot {
	labels := map[string]string{"arch": "aarch64", "class": "sbc", "ram": "512Mi"}
	return domain.NodeSnapshot{
		ID: "pi-garden", HostName: "pi-garden.local",
		Labels:         map[string]string{"arch": "aarch64", "class": "sbc", "ram": "512Mi", "zone": "garden"},
		OperatorLabels: map[string]string{"zone": "garden"},
		ReportedLabels: labels,
		Capabilities:   fixtureCapabilities(),
		Status:         "Online", RegisteredAt: fixtureAt, LastSeenAt: fixtureAt.Add(30 * time.Second),
		CertificateSerial: "4f2a9c", CertificateNotAfter: &fixtureLater, RevokedAt: &fixtureLater,
	}
}

func fixtureCapabilities() domain.NodeCapabilities {
	return domain.NodeCapabilities{AXLRVersion: "nanobot-0.1.0", PolicySupported: true, CorrelationSupported: true, Dedicated: true, Runtimes: []string{"jev"}}
}

func fixtures() []fixture {
	rules := domain.ToolRulesSnapshot{
		Allow: []domain.ToolRuleSnapshot{{Tool: "gpio_write", Arguments: map[string]string{"pin": "17"}}},
		Deny:  []domain.ToolRuleSnapshot{{Tool: "shell", Arguments: map[string]string{"command": "rm *"}}},
	}
	enrolled := EnrollResponse{
		Node: fixtureNode(), Certificate: "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n",
		CA: "-----BEGIN CERTIFICATE-----\nMIIC\n-----END CERTIFICATE-----\n", Serial: "4f2a9c", NotAfter: fixtureLater,
	}
	return []fixture{
		{name: "enroll_request", message: EnrollRequest{TokenID: "tok-1", Secret: "s3cr3t", HostName: "pi-garden.local", CSR: "-----BEGIN CERTIFICATE REQUEST-----\nMIIB\n-----END CERTIFICATE REQUEST-----\n"}},
		{name: "enroll_response", message: enrolled},
		{name: "register_request", message: RegisterRequest{ID: "pi-garden", HostName: "pi-garden.local", Labels: map[string]string{"arch": "aarch64", "class": "sbc", "ram": "512Mi"}, Capabilities: fixtureCapabilities()}},
		// The control plane answers a registration and a heartbeat with
		// the node as it knows it; a heartbeat has no body.
		{name: "node_snapshot", message: fixtureNode()},
		{name: "assignment_run", skip: []string{"AssignmentMessage.Decide", "RunMessage.Delegation", "RunMessage.Events"}, message: AssignmentMessage{
			ExecutionID: "exec-1", Kind: "run", LeaseUntil: fixtureAt.Add(time.Minute),
			Run: &RunMessage{
				TaskID: "task-1", BotID: "garden", TenantID: "home", CorrelationID: "corr-1", Objective: "water the bed if the soil is dry", Model: "jev-default",
				Level: "act-within-policy", AllowedTools: []string{"gpio_write", "soil_read"}, ToolRules: rules, TimeoutSeconds: 90,
				Budgets: &BudgetsMessage{MaxModelCalls: 4, MaxToolCalls: 8},
				Runtime: &domain.RuntimeSnapshot{Kind: "jev", Jev: &domain.JevProgramSnapshot{
					Server: "in-process", Start: "soil_read", Act: domain.JevActSnapshot{Tool: "gpio_write", Choice: "open the valve?", Verdict: "yes", Pass: []string{"pin"}},
					Wait: "30s", Stop: "soil is wet", Tell: &domain.JevTellSnapshot{Name: "watered", Fields: []string{"moisture"}}, Model: "jev-default",
				}},
				Input: json.RawMessage(`{"moisture":0.12}`),
			},
		}},
		{name: "assignment_decide", skip: []string{"AssignmentMessage.Run", "DecideMessage.Delegation", "DecideMessage.Events"}, message: AssignmentMessage{
			ExecutionID: "exec-2", Kind: "decide", LeaseUntil: fixtureAt.Add(time.Minute),
			Decide: &DecideMessage{
				TaskID: "task-1", BotID: "garden", CorrelationID: "corr-1", SessionID: "sess-1", CallID: "call-1", Decision: "approve",
				Level: "act-with-approval", AllowedTools: []string{"gpio_write"}, ToolRules: rules, TimeoutSeconds: 60,
			},
		}},
		// A lease is renewed with an empty POST; this is the answer.
		{name: "lease_response", message: LeaseMessage{LeaseUntil: fixtureAt.Add(2 * time.Minute)}},
		{name: "report_request", message: ReportMessage{
			Outcome: "Completed",
			Result: domain.Result{
				Answer: "watered for 30 s", SessionID: "sess-1", Sessions: []string{"sess-1"},
				Artifacts:       []domain.Artifact{{Name: "moisture.csv", MediaType: "text/csv", Reference: "node:moisture.csv", Size: 42, SHA256: "ab12", Description: "readings", Source: "soil_read", SessionID: "sess-1", CallID: "call-1", Stored: true}},
				ToolCalls:       []domain.ToolCall{{Name: "gpio_write", CallID: "call-1", Decision: "approved", Status: "ok"}},
				PendingDecision: &domain.PendingDecision{Tool: "gpio_write", CallID: "call-2", SessionID: "sess-1", Arguments: json.RawMessage(`{"pin":"17"}`)},
			},
			Usage:         domain.Usage{Requests: 1, PromptTokens: 120, CachedTokens: 20, CompletionTokens: 8, ReasoningTokens: 4, ToolCalls: 2, CostUSD: 0.0012},
			FailureReason: "none", TraceReference: "node:trace/exec-1.jsonl",
			Policy:      &domain.PolicyRecord{Level: "act-within-policy", RuntimeLevel: "act-within-policy", Enforced: true, Path: "policy/exec-1.json", Delegation: "none"},
			Correlation: &domain.CorrelationRecord{BotID: "garden", TaskID: "task-1", ExecutionID: "exec-1", TraceID: "trace-1", Carried: true, Path: "correlation/exec-1.json"},
			Receipt:     json.RawMessage(`{"runtime":"jev","steps":2}`),
		}},
		{name: "report_ack", message: ReportAck{Accepted: true, Outcome: "Completed"}},
		// A renewal is answered like an enrollment (enroll_response).
		{name: "renew_request", message: RenewRequest{CSR: "-----BEGIN CERTIFICATE REQUEST-----\nMIIC\n-----END CERTIFICATE REQUEST-----\n"}},
	}
}

// TestFixtures keeps testdata/fixtures equal to the messages: a change to
// a message the nanobot reads or writes fails here until the fixtures
// are rewritten (-update), and then fails the nanobot's checksum gate
// until it vendors them again.
func TestFixtures(t *testing.T) {
	dir := filepath.Join("testdata", "fixtures")
	if *updateFixtures {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range fixtures() {
		t.Run(f.name, func(t *testing.T) {
			requireFilled(t, reflect.ValueOf(f.message), reflect.TypeOf(f.message).Name(), f.skip)
			want, err := json.MarshalIndent(f.message, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			want = append(want, '\n')
			path := filepath.Join(dir, f.name+".json")
			if *updateFixtures {
				if err := os.WriteFile(path, want, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (go test ./nodeproto -run TestFixtures -update)", err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("%s changed; rewrite it with -update and vendor it again in axlr-nanobot:\n%s", path, want)
			}
		})
	}
}

// requireFilled fails on any empty field outside skip, so a field added
// to a message, omitted when empty, cannot slip past the fixtures unseen.
func requireFilled(t *testing.T, v reflect.Value, path string, skip []string) {
	t.Helper()
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			requireFilled(t, v.Elem(), path, skip)
		}
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(time.Time{}) {
			return
		}
		for i := 0; i < v.NumField(); i++ {
			field := v.Type().Field(i)
			name := v.Type().Name() + "." + field.Name
			if contains(skip, name) {
				continue
			}
			value := v.Field(i)
			if value.IsZero() {
				t.Errorf("%s (%s) is empty: fill it or skip it", name, path)
				continue
			}
			requireFilled(t, value, path+"."+field.Name, skip)
		}
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			return
		}
		for i := 0; i < v.Len(); i++ {
			requireFilled(t, v.Index(i), path, skip)
		}
	}
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}
