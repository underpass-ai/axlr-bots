package unixapi

import (
	"context"
	"net/http"
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// A person marks a diagnosis over the socket and the runbook's standing
// reads back on the autonomy routes and on the incident.
func TestAutonomyOverTheSocket(t *testing.T) {
	client, _, _ := serve(t, "completed")
	ctx := context.Background()
	register := func(d application.BotDefinition) {
		t.Helper()
		if _, err := client.RegisterBot(ctx, d); err != nil {
			t.Fatal(err)
		}
		if _, err := client.EnableBot(ctx, d.Name); err != nil {
			t.Fatal(err)
		}
	}
	triage := definition("triage", "observe")
	triage.Groups = []string{"sre"}
	triage.Triggers = []domain.TriggerSnapshot{{Type: "incident"}}
	register(triage)
	fix := definition("fix", "act-within-policy")
	fix.Groups = []string{"sre"}
	fix.AllowedTools = []string{"local_read", "patch"}
	fix.ToolRules = domain.ToolRulesSnapshot{}
	fix.Triggers = []domain.TriggerSnapshot{
		{Type: "incident", Name: "fix", Phase: "fix", Level: "propose", Tools: []string{"local_read"}},
		{Type: "incident", Name: "apply", Phase: "apply", Level: "act-with-approval", Tools: []string{"local_read", "patch"}, Ladder: &domain.LadderSnapshot{Runbooks: []domain.LadderRunbookSnapshot{{Name: "KubePodCrashLooping", Tool: "patch", Namespaces: []string{"shop"}}}}},
	}
	register(fix)
	opened, err := client.OpenIncident(ctx, application.OpenIncidentCommand{Tenant: "ops", BotID: "triage", GroupKey: "g", Source: "alertmanager", Title: "KubePodCrashLooping in shop", Severity: "sev2", Namespace: "shop", Fingerprints: []string{"fp-a"}})
	if err != nil {
		t.Fatal(err)
	}
	id := opened.Incident.ID
	if _, err := client.MarkDiagnosis(ctx, id, DiagnosisRequest{Verdict: "correct", MarkedBy: "tirso"}); err == nil {
		t.Fatal("marked before the diagnosis")
	}
	if _, err := client.RunTask(ctx, opened.Task.ID); err != nil {
		t.Fatal(err)
	}
	marked, err := client.MarkDiagnosis(ctx, id, DiagnosisRequest{Verdict: "correct", MarkedBy: "tirso"})
	if err != nil || marked.Totals.Diagnoses != 1 || marked.DiagnosisMark != "diagnosis-correct" || !marked.Saved || marked.Step != "act-with-approval" || marked.Thresholds.Applies != 20 {
		t.Fatalf("mark = %+v %v", marked, err)
	}
	listed, err := client.ListAutonomy(ctx, application.AutonomyQuery{Tenant: "ops", Bot: "fix", Runbook: "KubePodCrashLooping", Namespace: "shop"})
	if err != nil || len(listed) != 1 || listed[0].ID != marked.ID {
		t.Fatalf("list = %+v %v", listed, err)
	}
	got, err := client.GetAutonomy(ctx, marked.ID)
	if err != nil || len(got.History) != 1 || got.History[0].Actor != "tirso" {
		t.Fatalf("get = %+v %v", got, err)
	}
	incident, err := client.GetIncident(ctx, id)
	if err != nil || incident.Autonomy == nil || incident.Autonomy.DiagnosisMark != "diagnosis-correct" {
		t.Fatalf("incident = %+v %v", incident.Autonomy, err)
	}
	if _, err := client.GetAutonomy(ctx, "not-a-ladder"); err == nil {
		t.Error("a bad id was read")
	}
	if _, err := client.MarkDiagnosis(ctx, "bad id", DiagnosisRequest{Verdict: "correct"}); err == nil {
		t.Error("a bad incident id was marked")
	}
	as := func(actor string) http.Header {
		header := http.Header{}
		if actor != "" {
			header.Set(ActorHeader, actor)
		}
		return header
	}
	for _, c := range []struct {
		name, actor string
		body        any
		want        int
	}{
		{"no one named", "", DiagnosisRequest{Verdict: "wrong"}, http.StatusForbidden},
		{"a policy", "policy:autoApprove", DiagnosisRequest{Verdict: "wrong"}, http.StatusForbidden},
		{"two names", "mallory", DiagnosisRequest{Verdict: "wrong", MarkedBy: "tirso"}, http.StatusBadRequest},
		{"bad verdict", "tirso", DiagnosisRequest{Verdict: "maybe"}, http.StatusBadRequest},
		{"the other way", "tirso", DiagnosisRequest{Verdict: "wrong"}, http.StatusConflict},
		{"the same again", "tirso", DiagnosisRequest{Verdict: "correct"}, http.StatusOK},
	} {
		status, body, err := client.doWith(ctx, http.MethodPost, "/v1/incidents/"+id+"/diagnosis", as(c.actor), c.body)
		if err != nil || status != c.want {
			t.Errorf("%s: %d %s %v", c.name, status, body, err)
		}
	}
}
