package unixapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// A node asks for its execution: the answer is at once, the question's
// task or the refusal, never the answer itself.
func TestAQuestionOverTheSocket(t *testing.T) {
	client, _, _ := serve(t, "completed")
	ctx := context.Background()
	asker := definition("qa-jefe", "observe")
	asker.ID, asker.Groups, asker.Ask = "qa-jefe", []string{"qa"}, &domain.AskSnapshot{Teams: []string{"dev"}}
	asker.Placement = domain.PlacementSnapshot{Nodes: []string{"qa-node"}}
	responder := definition("dev-a", "observe")
	responder.ID, responder.Groups = "dev-a", []string{"dev"}
	responder.Triggers = []domain.TriggerSnapshot{{Type: "manual"}, {Type: "question", From: []string{"qa"}}}
	responder.Placement = domain.PlacementSnapshot{Nodes: []string{"dev-node"}}
	for _, d := range []application.BotDefinition{asker, responder} {
		if _, err := client.RegisterBot(ctx, d); err != nil {
			t.Fatal(err)
		}
		if _, err := client.EnableBot(ctx, d.ID); err != nil {
			t.Fatal(err)
		}
	}
	for _, node := range []application.NodeRegistration{
		{ID: "qa-node", HostName: "qa-node", Capabilities: domain.NodeCapabilities{PolicySupported: true}},
		{ID: "dev-node", HostName: "dev-node", Capabilities: domain.NodeCapabilities{PolicySupported: true, MemoryTeams: []string{"dev"}}},
	} {
		if _, err := client.Register(ctx, node); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := client.SubmitTask(ctx, SubmitRequest{BotID: "qa-jefe", Objective: "Lead QA", IdempotencyKey: "k-1"}); err != nil {
		t.Fatal(err)
	}
	assignment, found, err := client.NextAssignment(ctx, "qa-node", time.Second)
	if err != nil || !found || assignment.Run.Ask == nil || assignment.Run.Ask.Teams[0] != "dev" {
		t.Fatalf("assignment: %v %v %+v", found, err, assignment.Run)
	}
	answer, err := client.Ask(ctx, "qa-node", assignment.ExecutionID, application.AskTeamCommand{Team: "dev", Question: "Who owns the importer?"})
	if err != nil || !answer.Created || answer.BotID != "dev-a" || answer.Status != domain.TaskPending || answer.TaskID == "" {
		t.Fatalf("ask: %v %+v %+v", err, answer, answer.Refused)
	}
	again, err := client.Ask(ctx, "qa-node", assignment.ExecutionID, application.AskTeamCommand{Team: "dev", Question: "Who owns the importer?"})
	if err != nil || again.Created || again.TaskID != answer.TaskID {
		t.Errorf("again: %v %+v", err, again)
	}
	refused, err := client.Ask(ctx, "qa-node", assignment.ExecutionID, application.AskTeamCommand{Team: "marketing", Question: "Any launch?"})
	if err != nil || refused.Refused == nil || refused.Refused.Reason != domain.RefusedNotAllowed {
		t.Errorf("refused: %v %+v", err, refused)
	}
	parent, err := client.GetTask(ctx, assignment.Run.TaskID.String())
	if err != nil || len(parent.Questions) != 1 || parent.Questions[0] != answer.TaskID.String() {
		t.Errorf("GET /v1/tasks/{parent}: %v %v", err, parent.Questions)
	}
	// The question goes to the node that shares dev's memory.
	next, found, err := client.NextAssignment(ctx, "dev-node", time.Second)
	if err != nil || !found || next.Run.TaskID != answer.TaskID || next.Run.Ask != nil {
		t.Fatalf("dev-node: %v %v %+v", found, err, next.Run)
	}
	// Errors: another node, an empty question, a bad body, a bad node id.
	if _, err := client.Ask(ctx, "dev-node", assignment.ExecutionID, application.AskTeamCommand{Team: "dev", Question: "x"}); err == nil {
		t.Error("another node asked")
	}
	if _, err := client.Ask(ctx, "qa-node", assignment.ExecutionID, application.AskTeamCommand{Team: "dev", Question: " "}); err == nil {
		t.Error("an empty question was taken")
	}
	path := "/v1/nodes/qa-node/executions/" + assignment.ExecutionID.String() + "/questions"
	if err := client.call(ctx, http.MethodPost, path, map[string]any{"team": "dev", "wait": true}, &map[string]any{}); err == nil {
		t.Error("an unknown field was taken")
	}
	if err := client.call(ctx, http.MethodPost, "/v1/nodes/Bad/executions/"+assignment.ExecutionID.String()+"/questions", map[string]any{"team": "dev"}, &map[string]any{}); err == nil {
		t.Error("a bad node id was taken")
	}
}
