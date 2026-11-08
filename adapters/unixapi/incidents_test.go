package unixapi

import (
	"context"
	"encoding/json"
	"net/http"
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
	if got.NeedsYou || got.CurrentTask == nil || got.CurrentTask.TaskID != opened.Task.ID || got.CurrentTask.Phase != "triage" || got.CurrentTask.Status != "Pending" || got.CurrentTask.Approval != nil ||
		got.Approvals == nil || *got.Approvals != (IncidentApprovalsResponse{}) {
		t.Errorf("get's current task and approvals = %+v %+v", got.CurrentTask, got.Approvals)
	}
	if listed[0].CurrentTask != nil || listed[0].Approvals != nil || listed[0].NeedsYou {
		t.Errorf("a listed incident carries more than needsYou: %+v", listed[0])
	}
	raw, _ := json.Marshal(got)
	if !strings.Contains(string(raw), `"id":"`+opened.Incident.ID+`"`) || strings.Contains(string(raw), "needsYou") || !strings.Contains(string(raw), `"currentTask":{"taskId"`) {
		t.Errorf("the response is not the snapshot's fields and more: %s", raw)
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
	events, _ := client.ListTaskEvents(ctx, application.TaskEventsQuery{ID: domain.TaskID(opened.Task.ID), Limit: 50})
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

func TestAPostmortemOverTheSocket(t *testing.T) {
	client, local, _ := serve(t, "completed")
	ctx := context.Background()
	team := func(name string, triggers ...domain.TriggerSnapshot) {
		d := definition(name, "observe")
		d.Triggers, d.Groups = triggers, []string{"sre"}
		d.Approvals = domain.ApprovalsSnapshot{Approvers: []string{"tirso"}}
		if _, err := client.RegisterBot(ctx, d); err != nil {
			t.Fatal(err)
		}
		if _, err := client.EnableBot(ctx, name); err != nil {
			t.Fatal(err)
		}
	}
	team("triage", domain.TriggerSnapshot{Type: "incident"})
	team("scribe", domain.TriggerSnapshot{Type: "incident", Phase: "postmortem", Severities: []string{"sev2"}})
	if got, _ := client.GetBot(ctx, "scribe"); got.Triggers[0].Severities[0] != "sev2" {
		t.Fatalf("the trigger's severities: %+v", got.Triggers)
	}
	opened, err := client.OpenIncident(ctx, application.OpenIncidentCommand{Tenant: "ops", BotID: "triage", GroupKey: "g", Title: "Disk full", Severity: "sev2"})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := client.ResolveIncident(ctx, opened.Incident.ID, ResolveRequest{Reason: "green"})
	if err != nil || resolved.Postmortem == nil || resolved.Postmortem.State != "Writing" {
		t.Fatalf("resolve = %+v %v", resolved.Postmortem, err)
	}
	if _, err := local.RunTask(ctx, resolved.Postmortem.TaskID); err != nil {
		t.Fatal(err)
	}
	got, err := client.GetIncident(ctx, opened.Incident.ID)
	if err != nil || got.Status != "Resolved" || got.Postmortem == nil || got.Postmortem.State != "Draft" || got.Postmortem.Markdown == "" || got.Postmortem.AuthorTaskID != resolved.Postmortem.TaskID {
		t.Fatalf("drafted = %+v %v", got.Postmortem, err)
	}
	path := "/v1/incidents/" + opened.Incident.ID + "/postmortem"
	as := func(actor string) http.Header {
		header := http.Header{}
		if actor != "" {
			header.Set(ActorHeader, actor)
		}
		return header
	}
	for _, c := range []struct {
		route, actor string
		body         any
		want         int
	}{
		{"/approve", "", nil, http.StatusForbidden},
		{"/approve", "mallory", nil, http.StatusForbidden},
		{"/send-back", "tirso", PostmortemReviewRequest{AuthorTaskID: got.Postmortem.AuthorTaskID}, http.StatusBadRequest},
		{"/approve", "tirso", PostmortemReviewRequest{DecidedBy: "oncall"}, http.StatusBadRequest},
		{"/approve", "tirso", nil, http.StatusConflict},
		{"/approve", "tirso", PostmortemReviewRequest{AuthorTaskID: "task-other"}, http.StatusConflict},
		{"/approve", "tirso", PostmortemReviewRequest{AuthorTaskID: "no spaces"}, http.StatusBadRequest},
	} {
		if status, body, err := client.doWith(ctx, http.MethodPost, path+c.route, as(c.actor), c.body); err != nil || status != c.want {
			t.Errorf("%s as %q: %d %s %v", c.route, c.actor, status, body, err)
		}
	}
	back, err := client.SendBackPostmortem(ctx, opened.Incident.ID, PostmortemReviewRequest{DecidedBy: "tirso", Note: "add the owner", AuthorTaskID: got.Postmortem.AuthorTaskID})
	if err != nil || back.Postmortem.State != "Writing" || back.Postmortem.SendBacks != 1 || back.Postmortem.Note != "add the owner" || back.Postmortem.ReviewedBy != "tirso" {
		t.Fatalf("send-back = %+v %v", back.Postmortem, err)
	}
	if _, err := local.RunTask(ctx, back.Postmortem.TaskID); err != nil {
		t.Fatal(err)
	}
	status, body, err := client.doWith(ctx, http.MethodPost, path+"/approve", as("tirso"), PostmortemReviewRequest{AuthorTaskID: back.Postmortem.TaskID})
	if err != nil || status != http.StatusOK || !strings.Contains(string(body), `"approvedBy":"tirso"`) {
		t.Fatalf("approve: %d %s %v", status, body, err)
	}
	if _, err := client.ApprovePostmortem(ctx, opened.Incident.ID, PostmortemReviewRequest{DecidedBy: "tirso"}); err == nil {
		t.Error("approved twice")
	}
	for _, call := range []func(context.Context, string, PostmortemReviewRequest) (domain.IncidentSnapshot, error){local.ApprovePostmortem, local.SendBackPostmortem} {
		if _, err := call(ctx, "no spaces allowed", PostmortemReviewRequest{DecidedBy: "tirso", Note: "x"}); err == nil {
			t.Error("a bad id")
		}
		if _, err := call(ctx, "inc-ghost", PostmortemReviewRequest{DecidedBy: "tirso", Note: "x", AuthorTaskID: "no spaces"}); err == nil {
			t.Error("a bad task")
		}
		if _, err := call(ctx, "inc-ghost", PostmortemReviewRequest{DecidedBy: "tirso", Note: "x"}); !domain.IsNotFound(err) {
			t.Errorf("ghost: %v", err)
		}
	}
}
