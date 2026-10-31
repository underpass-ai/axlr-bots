package unixapi

import (
	"context"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

func TestIncidentsOverTheSocket(t *testing.T) {
	client, local, _ := serve(t, "completed")
	ctx := context.Background()
	d := definition("triage", "observe")
	d.Triggers = []domain.TriggerSnapshot{{Type: "incident"}}
	if _, err := client.RegisterBot(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableBot(ctx, "triage"); err != nil {
		t.Fatal(err)
	}
	command := application.OpenIncidentCommand{Tenant: "ops", BotID: "triage", GroupKey: `{}/{alertname="KubePodCrashLooping"}`, Source: "alertmanager", Title: "KubePodCrashLooping in shop", Severity: "sev2", Fingerprints: []string{"fp-a"}}
	opened, err := client.OpenIncident(ctx, command)
	if err != nil || !opened.Created || opened.Reopened || opened.Task == nil || opened.Incident.Status != "Open" || opened.Incident.Cycle != 1 || opened.Task.IncidentID != opened.Incident.ID {
		t.Fatalf("open = %+v %v", opened, err)
	}
	again, err := client.OpenIncident(ctx, command)
	if err != nil || again.Created || again.Task != nil || again.Incident.ID != opened.Incident.ID || again.Incident.Repeats != 1 {
		t.Fatalf("repeat = %+v %v", again, err)
	}
	listed, err := client.ListIncidents(ctx, application.IncidentQuery{Tenant: "ops", Status: "Open", Limit: 10})
	if err != nil || len(listed) != 1 || listed[0].ID != opened.Incident.ID {
		t.Fatalf("list = %+v %v", listed, err)
	}
	if _, err := client.ListIncidents(ctx, application.IncidentQuery{Status: "Closed"}); err == nil {
		t.Error("a bad status listed")
	}
	got, err := client.GetIncident(ctx, opened.Incident.ID)
	if err != nil || got.TaskIDs[0] != opened.Task.ID || len(got.Fingerprints) != 1 {
		t.Fatalf("get = %+v %v", got, err)
	}
	if _, err := client.GetIncident(ctx, "inc-ghost"); !domain.IsNotFound(err) {
		t.Errorf("ghost: %v", err)
	}
	// The triage task runs on the control plane itself; its answer is
	// the diagnosis, and the incident's events sit in the bot's timeline.
	if _, err := local.RunTask(ctx, opened.Task.ID); err != nil {
		t.Fatal(err)
	}
	diagnosed, _ := client.GetIncident(ctx, opened.Incident.ID)
	if diagnosed.Status != "Diagnosed" || diagnosed.Diagnosis == "" {
		t.Fatalf("after the run = %+v", diagnosed)
	}
	events, _ := client.ListTaskEvents(ctx, opened.Task.ID, 50)
	kinds := map[string]string{}
	for _, event := range events {
		kinds[event.Type.String()] = string(event.IncidentID)
	}
	if kinds["incident.opened"] != opened.Incident.ID || kinds["incident.diagnosed"] != opened.Incident.ID {
		t.Errorf("events = %v", kinds)
	}
	resolved, err := client.ResolveIncident(ctx, opened.Incident.ID, ResolveRequest{Reason: "alerts green"})
	if err != nil || resolved.Status != "Resolved" || resolved.Reason != "alerts green" || resolved.ResolvedAt == nil {
		t.Fatalf("resolve = %+v %v", resolved, err)
	}
	var remote RemoteError
	if _, err := client.ResolveIncident(ctx, opened.Incident.ID, ResolveRequest{}); err == nil || !errorsAs(err, &remote) || remote.Kind != KindState {
		t.Errorf("resolve twice: %v", err)
	}
	reopened, err := client.OpenIncident(ctx, command)
	if err != nil || !reopened.Reopened || reopened.Created || reopened.Incident.Cycle != 2 || reopened.Task == nil || reopened.Task.ID == opened.Task.ID {
		t.Fatalf("reopen = %+v %v", reopened, err)
	}
	bad := command
	bad.BotID = ""
	if _, err := client.OpenIncident(ctx, bad); err == nil || !errorsAs(err, &remote) || remote.Kind != KindValidation {
		t.Errorf("bad command: %v", err)
	}
	bad = command
	bad.BotID = "ghost"
	if _, err := client.OpenIncident(ctx, bad); !domain.IsNotFound(err) {
		t.Errorf("ghost bot: %v", err)
	}
	if _, err := client.ResolveIncident(ctx, "Bad Id", ResolveRequest{}); err == nil || !strings.Contains(err.Error(), "incident id") {
		t.Errorf("bad id: %v", err)
	}
}
