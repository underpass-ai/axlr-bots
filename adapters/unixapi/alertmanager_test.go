package unixapi

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func TestAlertmanagerOverTheSocket(t *testing.T) {
	client, _, _ := serve(t, "completed")
	ctx := context.Background()
	d := definition("triage", "observe")
	d.Triggers = []domain.TriggerSnapshot{{Type: "alertmanager", Name: "alerts", Secret: "s3cret-s3cret-s3cret"}}
	if _, err := client.RegisterBot(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableBot(ctx, "triage"); err != nil {
		t.Fatal(err)
	}
	body := func(name string) []byte {
		raw, err := os.ReadFile(filepath.Join("..", "..", "domain", "testdata", "alertmanager-"+name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	fired, err := client.FireTrigger(ctx, FireRequest{BotID: "triage", Trigger: "alerts", Body: body("firing")})
	if err != nil || !fired.Created || fired.Action != "opened" || fired.Task == nil || fired.Incident == nil || fired.Incident.Status != "Open" || fired.Task.IncidentID != fired.Incident.ID {
		t.Fatalf("fired = %+v %v", fired, err)
	}
	again, err := client.FireTrigger(ctx, FireRequest{BotID: "triage", Trigger: "alerts", Body: body("repeat")})
	if err != nil || again.Created || again.Action != "repeated" || again.Task != nil || again.Incident.ID != fired.Incident.ID || len(again.Incident.Fingerprints) != 2 {
		t.Fatalf("again = %+v %v", again, err)
	}
	resolved, err := client.FireTrigger(ctx, FireRequest{BotID: "triage", Trigger: "alerts", Body: body("resolved")})
	if err != nil || resolved.Action != "resolved" || resolved.Task != nil || resolved.Incident.Status != "Resolved" || len(resolved.Incident.ResolvedFingerprints) != 2 {
		t.Fatalf("resolved = %+v %v", resolved, err)
	}
	var remote RemoteError
	if _, err := client.FireTrigger(ctx, FireRequest{BotID: "triage", Trigger: "alerts", Body: []byte(`{"type":"deploy"}`)}); err == nil || !errorsAs(err, &remote) || remote.Kind != KindValidation {
		t.Errorf("a plain webhook body: %v", err)
	}
	incidents, err := client.ListIncidents(ctx, application.IncidentQuery{Tenant: "ops", Status: "Resolved"})
	if err != nil || len(incidents) != 1 {
		t.Errorf("list = %d %v", len(incidents), err)
	}
}
