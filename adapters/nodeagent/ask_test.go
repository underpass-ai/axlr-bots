package nodeagent

import (
	"context"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// askingTeams registers qa-jefe on qa-node, which may ask dev, and dev-a
// on dev-node, which answers qa.
func askingTeams(t *testing.T, f *fixture) {
	t.Helper()
	ctx := context.Background()
	for _, definition := range []application.BotDefinition{
		{
			ID: "qa-jefe", Tenant: "t", Name: "qa-jefe", Owner: "o", Mission: "lead QA", AllowedTools: []string{"local_read"},
			Groups: []string{"qa"}, Ask: &domain.AskSnapshot{Teams: []string{"dev"}},
			Placement: domain.PlacementSnapshot{Nodes: []string{"qa-node"}}, Budgets: application.BudgetsDefinition{TaskTimeoutSeconds: 120},
		},
		{
			ID: "dev-a", Tenant: "t", Name: "dev-a", Owner: "o", Mission: "develop", AllowedTools: []string{"local_read"},
			Groups: []string{"dev"}, Triggers: []domain.TriggerSnapshot{{Type: "manual"}, {Type: "question", From: []string{"qa"}}},
			Placement: domain.PlacementSnapshot{Nodes: []string{"dev-node"}}, Budgets: application.BudgetsDefinition{TaskTimeoutSeconds: 120},
		},
	} {
		if _, err := f.client.RegisterBot(ctx, definition); err != nil {
			t.Fatal(err)
		}
		if _, err := f.client.EnableBot(ctx, definition.ID); err != nil {
			t.Fatal(err)
		}
	}
}

// A bot asks another team through the tool and goes on: the call answers
// at once with the question and who answers it; the member of the other
// team, on its own node, answers it as a task of its own.
func TestAgentAsksAnotherTeamAndGoesOn(t *testing.T) {
	f := control(t, "completed")
	ctx := context.Background()
	askingTeams(t, f)
	executor := &scriptedExecutor{tool: application.AskToolName, script: map[domain.BotID]map[string]any{"qa-jefe": {"team": "dev", "question": "Who owns the importer?"}}}
	runNode(t, f, executor, "qa-node")
	runNode(t, f, executor, "dev-node", "dev")
	submitted, err := f.client.SubmitTask(ctx, unixapiSubmit("qa-jefe", "Find who owns the importer"))
	if err != nil {
		t.Fatal(err)
	}
	parent := waitFor(t, f.client, submitted.Task.ID, "Completed")
	executions, err := f.client.ListExecutions(ctx, parent.ID)
	if err != nil || len(executions) != 1 {
		t.Fatal(err)
	}
	said := executions[0].Result.Answer
	if !strings.Contains(said, "asked: error=false") || !strings.Contains(said, `"answeredBy":"dev-a"`) || !strings.Contains(said, "comes back later") || strings.Contains(said, "I am dev-a") {
		t.Fatalf("the asker read %s (%s)", said, f.logs.String())
	}
	if len(parent.Questions) != 1 || len(parent.Children) != 0 {
		t.Fatalf("questions %v children %v", parent.Questions, parent.Children)
	}
	child := waitFor(t, f.client, parent.Questions[0], "Completed")
	if child.BotID != "dev-a" || child.Trigger.Type != "question" || child.CorrelationID != parent.CorrelationID || !strings.Contains(child.Objective, "Team qa asks you through the team channel") {
		t.Errorf("child = %+v", child)
	}
	answered, err := f.client.ListExecutions(ctx, child.ID)
	if err != nil || len(answered) != 1 || answered[0].Result.Answer != "I am dev-a" || answered[0].NodeID != "dev-node" {
		t.Errorf("the answer: %v %+v", err, answered)
	}
	node, err := f.client.GetNode(ctx, "dev-node")
	if err != nil || len(node.Capabilities.MemoryTeams) != 1 || node.Capabilities.MemoryTeams[0] != "dev" {
		t.Errorf("dev-node: %v %+v", err, node.Capabilities)
	}
	executor.mu.Lock()
	defer executor.mu.Unlock()
	for _, request := range executor.seen {
		switch request.BotID {
		case "qa-jefe":
			if request.Ask == nil || request.Ask.Teams[0] != "dev" || !strings.Contains(strings.Join(request.NodeTools(), ","), application.AskToolName) {
				t.Errorf("qa-jefe's request = %+v", request)
			}
		case "dev-a":
			if request.Ask != nil || request.Delegation != nil || request.Events != nil || request.DelegationSocket != "" {
				t.Errorf("dev-a answers with node tools: %+v", request)
			}
		}
	}
	if !strings.Contains(f.logs.String(), "may ask the teams dev") || !strings.Contains(f.logs.String(), "shares the memory of the teams dev") {
		t.Errorf("logs = %s", f.logs.String())
	}
}

// A refused question reaches the model as the tool's error, with why.
func TestAgentHandsTheModelARefusedQuestion(t *testing.T) {
	f := control(t, "completed")
	ctx := context.Background()
	askingTeams(t, f)
	executor := &scriptedExecutor{tool: application.AskToolName, script: map[domain.BotID]map[string]any{"qa-jefe": {"team": "marketing", "question": "Any launch?"}}}
	runNode(t, f, executor, "qa-node")
	submitted, err := f.client.SubmitTask(ctx, unixapiSubmit("qa-jefe", "Ask marketing"))
	if err != nil {
		t.Fatal(err)
	}
	parent := waitFor(t, f.client, submitted.Task.ID, "Completed")
	executions, err := f.client.ListExecutions(ctx, parent.ID)
	if err != nil || len(executions) != 1 {
		t.Fatal(err)
	}
	if said := executions[0].Result.Answer; !strings.Contains(said, "asked: error=true") || !strings.Contains(said, domain.RefusedNotAllowed) {
		t.Errorf("the asker read %s", said)
	}
	if len(parent.Questions) != 0 {
		t.Errorf("questions = %v", parent.Questions)
	}
}
