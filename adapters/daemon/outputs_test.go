package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// An incident opened while the daemon runs reaches a command output,
// and the daemon's log names the target, never its URL.
func TestDaemonAnnouncesAnIncidentToItsOutputs(t *testing.T) {
	f := newFixture(t, "completed")
	announced := filepath.Join(f.dir, "announced.json")
	client, stop := f.start(t)
	defer stop()
	ctx := context.Background()
	definition := application.BotDefinition{
		Tenant: "ops", Name: "triage", Owner: "o", Mission: "find the cause of each incident", AllowedTools: []string{"local_read"},
		Triggers: []domain.TriggerSnapshot{{Type: "incident"}},
	}
	if _, err := client.RegisterBot(ctx, definition); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableBot(ctx, "triage"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.AddOutput(ctx, application.OutputDefinition{Name: "file", Type: "command", Command: []string{"/bin/sh", "-c", `cat > "$1"`, "out", announced}, Events: []string{"opened"}}); err != nil {
		t.Fatal(err)
	}
	opened, err := client.OpenIncident(ctx, application.OpenIncidentCommand{Tenant: "ops", BotID: "triage", GroupKey: "g", Title: "Disk full", Severity: "sev1"})
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var event application.IncidentOutputEvent
	for {
		raw, err := os.ReadFile(announced)
		if err == nil && json.Unmarshal(raw, &event) == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not announced: %s", f.logs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if event.IncidentID != opened.Incident.ID || event.Event != "incident.opened" || event.Severity != "sev1" {
		t.Errorf("event = %+v", event)
	}
	for deadline := time.Now().Add(5 * time.Second); !strings.Contains(f.logs.String(), "(incident.opened) to file: delivered (attempt 1)"); {
		if time.Now().After(deadline) {
			t.Fatalf("logs:\n%s", f.logs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// An output that hangs holds up no approval notification: they go out
// on loops of their own (review of #136).
func TestAHangingOutputDelaysNoApprovalNotification(t *testing.T) {
	f := newFixture(t, "approval-flow")
	notified := filepath.Join(f.dir, "notified.json")
	client, stop := f.start(t)
	defer stop()
	ctx := context.Background()
	definition := application.BotDefinition{
		Tenant: "ops", Name: "writer", Owner: "o", Mission: "do what each task asks", Level: "act-with-approval", AllowedTools: []string{"local_read", "local_write"},
		Triggers:  []domain.TriggerSnapshot{{Type: "incident"}},
		Approvals: domain.ApprovalsSnapshot{TTL: "2m", Notify: []domain.NotifyTargetSnapshot{{Type: "command", Name: "file", Command: []string{"/bin/sh", "-c", `cat > "$1"`, "notify", notified}}}},
	}
	if _, err := client.RegisterBot(ctx, definition); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableBot(ctx, "writer"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"slow-a", "slow-b"} {
		if _, err := client.AddOutput(ctx, application.OutputDefinition{Name: name, Type: "command", Command: []string{"/bin/sh", "-c", "sleep 8"}}); err != nil {
			t.Fatal(err)
		}
	}
	started := time.Now()
	if _, err := client.OpenIncident(ctx, application.OpenIncidentCommand{Tenant: "ops", BotID: "writer", GroupKey: "g", Title: "Disk full"}); err != nil {
		t.Fatal(err)
	}
	for {
		if raw, err := os.ReadFile(notified); err == nil && len(raw) > 0 {
			break
		}
		if time.Since(started) > 6*time.Second {
			t.Fatalf("the notification waited behind the outputs:\n%s", f.logs.String())
		}
		time.Sleep(20 * time.Millisecond)
	}
}
