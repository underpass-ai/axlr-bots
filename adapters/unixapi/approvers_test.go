package unixapi

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// The decide routes take who decides from the X-Axlr-Actor header or
// decidedBy; a bot that lists approvers answers 403 to anyone else, and
// to nobody named.
func TestTheDecideRoutesNameWhoDecides(t *testing.T) {
	client, _, _ := serveWith(t, "approval-flow", nil)
	ctx := context.Background()
	bot := definition("bot-b", "act-with-approval")
	bot.Approvals = domain.ApprovalsSnapshot{Approvers: []string{"alice", "oncall"}}
	if _, err := client.RegisterBot(ctx, bot); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableBot(ctx, "bot-b"); err != nil {
		t.Fatal(err)
	}
	waiting := func(key string) string {
		submitted, err := client.SubmitTask(ctx, SubmitRequest{BotID: "bot-b", Objective: "Restart demo-broken.service", IdempotencyKey: key})
		if err != nil {
			t.Fatal(err)
		}
		if run, err := client.RunTask(ctx, submitted.Task.ID); err != nil || run.Task.Status != "WaitingApproval" {
			t.Fatalf("run: %v %+v", err, run.Task)
		}
		return submitted.Task.ID
	}
	id := waiting("k-1")
	as := func(actor string) http.Header {
		header := http.Header{}
		if actor != "" {
			header.Set(ActorHeader, actor)
		}
		return header
	}
	for _, c := range []struct {
		path, actor string
		body        any
		want        int
	}{
		{"/approve", "", nil, http.StatusForbidden},
		{"/deny", "mallory", nil, http.StatusForbidden},
		{"/decide", "", DecideRequest{Decision: "approve", DecidedBy: "mallory"}, http.StatusForbidden},
		{"/decide", "mallory", DecideRequest{Decision: "approve", DecidedBy: "alice"}, http.StatusBadRequest},
		{"/decide", "", map[string]string{"decision": "approve", "actor": "alice"}, http.StatusBadRequest},
	} {
		status, body, err := client.doWith(ctx, http.MethodPost, "/v1/tasks/"+id+c.path, as(c.actor), c.body)
		if err != nil || status != c.want {
			t.Errorf("%s as %q: %d %s %v", c.path, c.actor, status, body, err)
		}
		if c.want == http.StatusForbidden && !strings.Contains(string(body), `"kind":"forbidden"`) {
			t.Errorf("%s: %s", c.path, body)
		}
	}
	if _, err := client.DecideTask(ctx, id, DecideRequest{Decision: "deny", DecidedBy: "cli:mallory"}); !domain.IsForbidden(err) {
		t.Errorf("the client lost the refusal: %v", err)
	}
	if task, _ := client.GetTask(ctx, id); task.Status != "WaitingApproval" {
		t.Fatalf("a refusal changed the task: %+v", task)
	}
	status, body, err := client.doWith(ctx, http.MethodPost, "/v1/tasks/"+id+"/approve", as("alice"), nil)
	if err != nil || status != http.StatusOK {
		t.Fatalf("approve as alice: %d %s %v", status, body, err)
	}
	other := waiting("k-2")
	if decided, err := client.DecideTask(ctx, other, DecideRequest{Decision: "deny", DecidedBy: "oncall"}); err != nil || decided.Task.Approval == nil || decided.Task.Approval.Outcome != domain.ApprovalDenied {
		t.Fatalf("deny as oncall: %v %+v", err, decided.Task)
	}
	status, body, err = client.doWith(ctx, http.MethodPost, "/v1/tasks/"+waiting("k-3")+"/decide", as("oncall"), DecideRequest{Decision: "deny"})
	if err != nil || status != http.StatusOK {
		t.Errorf("decide with the header alone: %d %s %v", status, body, err)
	}
}

// Review of #139, P3: over the API nobody decides under a policy's name
// or, on a daemon, under the control plane's own.
func TestTheDecideRoutesRefuseAPolicyAndTheDaemonsName(t *testing.T) {
	client, _, _ := serveWith(t, "approval-flow", func(deps *application.Dependencies) { deps.Daemon = true })
	ctx := context.Background()
	bot := definition("bot-b", "act-with-approval")
	bot.Approvals = domain.ApprovalsSnapshot{Approvers: []string{"alice", "test:api"}}
	if _, err := client.RegisterBot(ctx, bot); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableBot(ctx, "bot-b"); err != nil {
		t.Fatal(err)
	}
	submitted, err := client.SubmitTask(ctx, SubmitRequest{BotID: "bot-b", Objective: "Restart demo-broken.service", IdempotencyKey: "k-1"})
	if err != nil {
		t.Fatal(err)
	}
	if run, err := client.RunTask(ctx, submitted.Task.ID); err != nil || run.Task.Status != "WaitingApproval" {
		t.Fatalf("run: %v %+v", err, run.Task)
	}
	path := "/v1/tasks/" + submitted.Task.ID
	for _, c := range []struct {
		route, header string
		body          any
		says          string
	}{
		{"/approve", "policy:team-event", nil, "policy's name"},
		{"/deny", "policy:autoApprove", nil, "policy's name"},
		{"/decide", "", DecideRequest{Decision: "approve", DecidedBy: "policy:team-event"}, "policy's name"},
		{"/approve", "test:api", nil, "own name"},
		{"/approve", "cli:test:api", nil, "own name"},
		{"/decide", "", DecideRequest{Decision: "approve", DecidedBy: "test:api"}, "own name"},
	} {
		header := http.Header{}
		if c.header != "" {
			header.Set(ActorHeader, c.header)
		}
		status, body, err := client.doWith(ctx, http.MethodPost, path+c.route, header, c.body)
		if err != nil || status != http.StatusForbidden || !strings.Contains(string(body), c.says) {
			t.Errorf("%s as %q: %d %s %v", c.route, c.header, status, body, err)
		}
	}
	if task, _ := client.GetTask(ctx, submitted.Task.ID); task.Status != "WaitingApproval" {
		t.Fatalf("a refused name changed the task: %+v", task)
	}
}
