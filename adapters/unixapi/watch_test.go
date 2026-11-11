package unixapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// A team's Jev watcher over the socket: the route answers only a Jev
// watcher, with its own team's questions once they are WatchAfter old;
// abandon cancels the question with Jev's verdict as the reason.
func TestTheWatchRoutesOverTheSocket(t *testing.T) {
	clock := &testClock{now: time.Now()}
	client, _, _ := serveWith(t, "completed", func(d *application.Dependencies) { d.Clock = clock })
	ctx := context.Background()
	asker := definition("qa-jefe", "observe")
	asker.ID, asker.Groups, asker.Ask = "qa-jefe", []string{"qa"}, &domain.AskSnapshot{Teams: []string{"dev"}}
	asker.Placement = domain.PlacementSnapshot{Nodes: []string{"qa-node"}}
	responder := definition("dev-a", "observe")
	responder.ID, responder.Groups = "dev-a", []string{"dev"}
	responder.Triggers = []domain.TriggerSnapshot{{Type: "manual"}, {Type: "question", From: []string{"qa"}}}
	responder.Placement = domain.PlacementSnapshot{Nodes: []string{"dev-node"}}
	watcher := definition("qa-vigia", "act-within-policy")
	watcher.ID, watcher.Groups, watcher.ToolRules = "qa-vigia", []string{"qa"}, domain.ToolRulesSnapshot{}
	watcher.AllowedTools = []string{domain.WatchNextTool.String(), domain.WatchDecideTool.String()}
	watcher.Runtime = &domain.RuntimeSnapshot{Kind: "jev", Jev: &domain.JevProgramSnapshot{
		Server: domain.WatchServer, Start: domain.WatchNextTool.String(), Wait: domain.WatchNextTool.String(),
		Act: domain.JevActSnapshot{Tool: domain.WatchDecideTool.String(), Choice: domain.WatchChoice, Verdict: domain.WatchVerdict},
	}}
	watcher.Placement = domain.PlacementSnapshot{Nodes: []string{"jev-node"}}
	for _, d := range []application.BotDefinition{asker, responder, watcher} {
		if _, err := client.RegisterBot(ctx, d); err != nil {
			t.Fatal(err)
		}
		if _, err := client.EnableBot(ctx, d.ID); err != nil {
			t.Fatal(err)
		}
	}
	for _, node := range []application.NodeRegistration{
		{ID: "qa-node", HostName: "qa-node", Capabilities: domain.NodeCapabilities{PolicySupported: true, TeamChannel: true}},
		{ID: "dev-node", HostName: "dev-node", Capabilities: domain.NodeCapabilities{PolicySupported: true, MemoryTeams: []string{"dev"}, TeamChannel: true}},
		{ID: "jev-node", HostName: "jev-node", Capabilities: domain.NodeCapabilities{PolicySupported: true, Runtimes: []string{"jev"}, TeamChannel: true}},
	} {
		if _, err := client.Register(ctx, node); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.SubmitTask(ctx, SubmitRequest{BotID: "qa-jefe", Objective: "Lead QA", IdempotencyKey: "k-1"}); err != nil {
		t.Fatal(err)
	}
	lead, found, err := client.NextAssignment(ctx, "qa-node", time.Second)
	if err != nil || !found {
		t.Fatalf("qa-node: %v %v", found, err)
	}
	question, err := client.Ask(ctx, "qa-node", lead.ExecutionID, application.AskTeamCommand{Team: "dev", Question: "Who owns the importer?"})
	if err != nil || question.TaskID == "" {
		t.Fatalf("ask: %v %+v", err, question)
	}
	// An AXLR bot is refused, as forbidden.
	if _, err := client.WatchNext(ctx, "qa-node", lead.ExecutionID, ""); !domain.IsForbidden(err) {
		t.Errorf("an AXLR bot watched: %v", err)
	}
	if _, err := client.DecideWatch(ctx, "qa-node", lead.ExecutionID, application.WatchDecision{Question: question.TaskID, Choice: "abandon"}); !domain.IsForbidden(err) {
		t.Errorf("an AXLR bot decided: %v", err)
	}
	if _, err := client.SubmitTask(ctx, SubmitRequest{BotID: "qa-vigia", Objective: "Watch the team's questions", IdempotencyKey: "w-1"}); err != nil {
		t.Fatal(err)
	}
	watch, found, err := client.NextAssignment(ctx, "jev-node", time.Second)
	if err != nil || !found || watch.Run.BotID != "qa-vigia" {
		t.Fatalf("jev-node: %v %v %+v", found, err, watch.Run)
	}
	// Younger than WatchAfter: not offered.
	if next, err := client.WatchNext(ctx, "jev-node", watch.ExecutionID, ""); err != nil || !next.Done || next.Team != "qa" {
		t.Errorf("a young question: %v %+v", err, next)
	}
	clock.advance(application.WatchAfter + time.Minute)
	next, err := client.WatchNext(ctx, "jev-node", watch.ExecutionID, "")
	if err != nil || next.Done || next.Cursor != question.TaskID || next.View.AskedBy != "qa-jefe" || !strings.Contains(next.View.Text(), "to team dev") {
		t.Fatalf("next: %v %+v", err, next)
	}
	if after, err := client.WatchNext(ctx, "jev-node", watch.ExecutionID, next.Cursor); err != nil || !after.Done {
		t.Errorf("after the cursor: %v %+v", err, after)
	}
	if waited, err := client.DecideWatch(ctx, "jev-node", watch.ExecutionID, application.WatchDecision{Question: question.TaskID, Choice: "wait", Probability: 0.7, Confidence: 0.6}); err != nil || waited.Abandoned || waited.Status != domain.TaskPending {
		t.Errorf("wait: %v %+v", err, waited)
	}
	decided, err := client.DecideWatch(ctx, "jev-node", watch.ExecutionID, application.WatchDecision{Question: question.TaskID, Choice: "abandon", Probability: 0.83, Confidence: 0.71})
	if err != nil || !decided.Abandoned || decided.Status != domain.TaskCancelled || decided.Reason != "abandoned by qa-vigia: Jev chose abandon (p=0.83, c=0.71)" {
		t.Fatalf("abandon: %v %+v", err, decided)
	}
	task, err := client.GetTask(ctx, question.TaskID.String())
	if err != nil || task.Status != "Cancelled" || task.FailureReason != decided.Reason {
		t.Errorf("the question: %v %+v", err, task)
	}
	// Errors: bad ids, a bad choice, an unknown question, a bad body.
	base := "/v1/nodes/jev-node/executions/" + watch.ExecutionID.String() + "/watch"
	for _, body := range []map[string]any{{"after": "Bad Id"}, {"cursor": "x"}} {
		if err := client.call(ctx, http.MethodPost, base, body, &map[string]any{}); err == nil {
			t.Errorf("%v was taken", body)
		}
	}
	for _, body := range []map[string]any{{"question": "Bad Id", "choice": "wait"}, {"question": question.TaskID.String(), "choice": "maybe"}, {"question": "task-404", "choice": "wait"}, {"question": question.TaskID.String(), "verdict": 1}} {
		if err := client.call(ctx, http.MethodPost, base+"/decisions", body, &map[string]any{}); err == nil {
			t.Errorf("%v was taken", body)
		}
	}
	if err := client.call(ctx, http.MethodPost, "/v1/nodes/Bad/executions/"+watch.ExecutionID.String()+"/watch", map[string]any{}, &map[string]any{}); err == nil {
		t.Error("a bad node id was taken")
	}
	if err := client.call(ctx, http.MethodPost, "/v1/nodes/Bad/executions/"+watch.ExecutionID.String()+"/watch/decisions", map[string]any{}, &map[string]any{}); err == nil {
		t.Error("a bad node id was taken")
	}
	// The node switched its team channel off: the watcher is forbidden.
	if _, err := client.Register(ctx, application.NodeRegistration{ID: "jev-node", HostName: "jev-node", Capabilities: domain.NodeCapabilities{PolicySupported: true, Runtimes: []string{"jev"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.WatchNext(ctx, "jev-node", watch.ExecutionID, ""); !domain.IsForbidden(err) || !strings.Contains(err.Error(), "team channel off") {
		t.Errorf("a watcher on a node with the channel off: %v", err)
	}
	if _, err := client.Ask(ctx, "jev-node", watch.ExecutionID, application.AskTeamCommand{Team: "dev", Question: "Who?"}); err == nil {
		t.Error("a node with the channel off asked")
	}
}
