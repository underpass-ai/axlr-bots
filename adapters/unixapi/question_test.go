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
	// The operator reads the link the two bots make, from either team.
	links, err := client.ListGroupLinks(ctx, "dev", "ops")
	if err != nil || len(links) != 1 || links[0].From != "qa" || links[0].To != "dev" || links[0].Askers[0] != "qa-jefe" || links[0].Responders[0] != "dev-a" {
		t.Fatalf("links = %+v %v", links, err)
	}
	if links, err := client.ListGroupLinks(ctx, "qa", ""); err != nil || len(links) != 1 {
		t.Errorf("qa's links = %+v %v", links, err)
	}
	if _, err := client.ListGroupLinks(ctx, "platform", ""); !domain.IsNotFound(err) {
		t.Errorf("a missing team's links: %v", err)
	}
	for _, node := range []application.NodeRegistration{
		{ID: "qa-node", HostName: "qa-node", Capabilities: domain.NodeCapabilities{PolicySupported: true, TeamChannel: true}},
		{ID: "dev-node", HostName: "dev-node", Capabilities: domain.NodeCapabilities{PolicySupported: true, MemoryTeams: []string{"dev"}, TeamChannel: true}},
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
	// The asker reads where it stands.
	state, err := client.QuestionState(ctx, "qa-node", assignment.ExecutionID, answer.TaskID)
	if err != nil || state.Status != domain.TaskPending || state.Responder != "dev-a" || state.Team != "dev" || state.Waiting == "" || state.Deadline.IsZero() {
		t.Errorf("state: %v %+v", err, state)
	}
	if _, err := client.QuestionState(ctx, "qa-node", assignment.ExecutionID, "task-404"); err == nil {
		t.Error("an unknown question was read")
	}
	if err := client.call(ctx, http.MethodGet, "/v1/nodes/qa-node/executions/"+assignment.ExecutionID.String()+"/questions/Bad%20Id", nil, &map[string]any{}); err == nil {
		t.Error("a bad question id was read")
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
	// dev-node answers: the report's answer carries what dev's store keeps
	// of the question, and the asker's message what qa's keeps.
	ack, err := client.Report(ctx, "dev-node", next.ExecutionID, application.ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "Alice."}})
	if err != nil || !ack.Accepted || ack.ChannelMemory == nil || ack.ChannelMemory.Side != domain.ChannelAnswered || ack.ChannelMemory.Team != "dev" || ack.ChannelMemory.Answer != "Alice." {
		t.Fatalf("report: %v %+v", err, ack)
	}
	messages, err := client.ExecutionMessages(ctx, "qa-node", assignment.ExecutionID, "")
	if err != nil || len(messages) != 1 || messages[0].Memory == nil || messages[0].Memory.Side != "asked" || messages[0].Memory.Team != "qa" || messages[0].Memory.Text != "Who owns the importer?" {
		t.Fatalf("messages: %v %+v", err, messages)
	}
}
