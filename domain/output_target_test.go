package domain

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

const testOutputSecret = "a-webhook-secret-of-32-characters"

func TestOutputTargetTypes(t *testing.T) {
	webhook, err := NewOutputTarget(OutputTargetSnapshot{Name: "hook", Type: "webhook", URL: "https://receiver.example.test/axlr", Secret: testOutputSecret})
	if err != nil || webhook.Kind() != OutputWebhook || webhook.URL() == "" || webhook.Host() != "receiver.example.test" || webhook.Command() != nil {
		t.Fatalf("webhook = %+v %v", webhook, err)
	}
	if !slices.Equal(webhook.Events(), DefaultOutputEvents) || webhook.Tenants() != nil || webhook.Severities() != nil || webhook.PanelURL() != "" {
		t.Errorf("defaults = %v %v %v", webhook.Events(), webhook.Tenants(), webhook.Severities())
	}
	slack, err := NewOutputTarget(OutputTargetSnapshot{Name: "chat", Type: "slack", URL: "https://hooks.slack.test/services/T/B/SECRET"})
	if err != nil || slack.Sign([]byte("x")) != "" {
		t.Fatalf("slack = %v", err)
	}
	command, err := NewOutputTarget(OutputTargetSnapshot{Name: "relay", Type: "command", Command: []string{"/usr/local/bin/relay", "--to", "teams"}})
	if err != nil || command.Host() != "" || command.Sign([]byte("x")) != "" || len(command.Command()) != 3 {
		t.Fatalf("command = %v", err)
	}
	if sig := webhook.Sign([]byte(`{"a":1}`)); !strings.HasPrefix(sig, "sha256=") || len(sig) != 71 {
		t.Errorf("signature = %q", sig)
	}
	for name, s := range map[string]OutputTargetSnapshot{
		"no name":           {Type: "webhook", URL: "https://x.test/", Secret: testOutputSecret},
		"bad type":          {Name: "x", Type: "email"},
		"webhook command":   {Name: "x", Type: "webhook", URL: "https://x.test/", Secret: testOutputSecret, Command: []string{"/bin/true"}},
		"webhook no url":    {Name: "x", Type: "webhook", Secret: testOutputSecret},
		"webhook no secret": {Name: "x", Type: "webhook", URL: "https://x.test/"},
		"short secret":      {Name: "x", Type: "webhook", URL: "https://x.test/", Secret: "short"},
		"slack secret":      {Name: "x", Type: "slack", URL: "https://x.test/", Secret: testOutputSecret},
		"slack command":     {Name: "x", Type: "slack", URL: "https://x.test/", Command: []string{"/bin/true"}},
		"slack no url":      {Name: "x", Type: "slack"},
		"slack ftp":         {Name: "x", Type: "slack", URL: "ftp://x.test/"},
		"slack credentials": {Name: "x", Type: "slack", URL: "https://u:p@x.test/"},
		"command url":       {Name: "x", Type: "command", URL: "https://x.test/", Command: []string{"/bin/true"}},
		"command relative":  {Name: "x", Type: "command", Command: []string{"relay"}},
		"command none":      {Name: "x", Type: "command"},
		"bad event":         {Name: "x", Type: "slack", URL: "https://x.test/", Events: []string{"bot.task.completed"}},
		"bad tenant":        {Name: "x", Type: "slack", URL: "https://x.test/", Tenants: []string{"Bad Tenant"}},
		"empty severity":    {Name: "x", Type: "slack", URL: "https://x.test/", Severities: []string{" "}},
		"bad severity":      {Name: "x", Type: "slack", URL: "https://x.test/", Severities: []string{"sev9"}},
		"bad panel":         {Name: "x", Type: "slack", URL: "https://x.test/", PanelURL: "panel"},
		"too many events":   {Name: "x", Type: "slack", URL: "https://x.test/", Events: slices.Repeat([]string{"opened"}, 65)},
		"too many tenants":  {Name: "x", Type: "slack", URL: "https://x.test/", Tenants: slices.Repeat([]string{"a"}, 65)},
		"too many sevs":     {Name: "x", Type: "slack", URL: "https://x.test/", Severities: slices.Repeat([]string{"sev1"}, 65)},
	} {
		var verr ValidationError
		if _, err := NewOutputTarget(s); !errors.As(err, &verr) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := ParseOutputKind("email"); err == nil {
		t.Error("email is a kind")
	}
}

func TestOutputTargetFilters(t *testing.T) {
	target, err := NewOutputTarget(OutputTargetSnapshot{
		Name: "x", Type: "slack", URL: "https://x.test/",
		Events: []string{"opened", "incident.resolved", "opened"}, Tenants: []string{"ops", "ops"}, Severities: []string{"sev1", "sev2", "sev2"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(target.Events(), []EventType{EventIncidentOpened, EventIncidentResolved}) || len(target.Tenants()) != 1 || len(target.Severities()) != 2 {
		t.Fatalf("filters = %v %v %v", target.Events(), target.Tenants(), target.Severities())
	}
	incident := sampleIncident(t) // ops, sev2
	if !target.Wants(EventIncidentOpened, incident) {
		t.Error("an opened sev2 incident of ops was refused")
	}
	if target.Wants(EventIncidentDiagnosed, incident) {
		t.Error("an event not in the list passed")
	}
	other, _ := NewIncident("inc-2", "shop", "triage", "g", "manual", "t", Sev2, nil, t0)
	if target.Wants(EventIncidentOpened, other) {
		t.Error("another tenant passed")
	}
	minor, _ := NewIncident("inc-3", "ops", "triage", "g", "manual", "t", Sev4, nil, t0)
	if target.Wants(EventIncidentOpened, minor) {
		t.Error("a sev4 passed")
	}
	snapshot := target.Snapshot()
	again, err := NewOutputTarget(snapshot)
	if err != nil || again.Fingerprint() != target.Fingerprint() || !slices.Equal(again.Snapshot().Events, snapshot.Events) {
		t.Errorf("round trip: %v", err)
	}
}

func TestOutputTargetLinksAndFingerprint(t *testing.T) {
	plain, _ := NewOutputTarget(OutputTargetSnapshot{Name: "x", Type: "slack", URL: "https://x.test/a"})
	own, _ := NewOutputTarget(OutputTargetSnapshot{Name: "x", Type: "slack", URL: "https://x.test/a", PanelURL: "https://ops.test/axlr/"})
	if got := plain.PanelLink("inc-1", ""); got != "" {
		t.Errorf("no base = %q", got)
	}
	if got := plain.PanelLink("inc-1", "https://panel.test/"); got != "https://panel.test/incidents/inc-1" {
		t.Errorf("control plane base = %q", got)
	}
	if got := own.PanelLink("inc-1", "https://panel.test"); got != "https://ops.test/axlr/incidents/inc-1" {
		t.Errorf("own base = %q", got)
	}
	a, _ := NewOutputTarget(OutputTargetSnapshot{Name: "x", Type: "webhook", URL: "https://x.test/", Secret: testOutputSecret})
	b, _ := NewOutputTarget(OutputTargetSnapshot{Name: "x", Type: "webhook", URL: "https://x.test/", Secret: testOutputSecret + "!"})
	if a.Fingerprint() == b.Fingerprint() || !strings.HasPrefix(a.Fingerprint(), "sha256:") || len(a.Fingerprint()) != 19 {
		t.Errorf("fingerprints %s %s", a.Fingerprint(), b.Fingerprint())
	}
	if strings.Contains(a.Fingerprint(), testOutputSecret) {
		t.Error("the fingerprint shows the secret")
	}
	broken := OutputTarget{kind: OutputSlack, url: "://bad"}
	if broken.Host() != "" {
		t.Error("a host from a bad URL")
	}
}

func TestParseOutputEvent(t *testing.T) {
	for raw, want := range map[string]EventType{"opened": EventIncidentOpened, " incident.escalated ": EventIncidentEscalated, "repeated": EventIncidentRepeated} {
		if got, err := ParseOutputEvent(raw); err != nil || got != want {
			t.Errorf("%q = %s %v", raw, got, err)
		}
	}
	for _, raw := range []string{"", "task.completed", "incident.", "incident.closed"} {
		if _, err := ParseOutputEvent(raw); err == nil {
			t.Errorf("%q passed", raw)
		}
	}
}
