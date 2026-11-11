package nodeagent

import (
	"bytes"
	"context"
	"log"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// requestRecorder keeps what the node handed the runtime, run or decision.
type requestRecorder struct {
	Executor
	mu        sync.Mutex
	runs      []application.ExecutionRequest
	decisions []application.DecisionRequest
}

func (r *requestRecorder) Execute(_ context.Context, request application.ExecutionRequest) (application.ExecutionReport, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs = append(r.runs, request)
	return application.ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "done"}}, nil
}

func (r *requestRecorder) Decide(_ context.Context, request application.DecisionRequest) (application.ExecutionReport, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.decisions = append(r.decisions, request)
	return application.ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "done"}}, nil
}

// channelToolsIn names the team channel's tools a request carries.
func channelToolsIn(ask *application.AskOffer, allowed, node []string) []string {
	var found []string
	for _, tool := range append(slices.Clone(allowed), node...) {
		if slices.Contains(channelTools, tool) {
			found = append(found, tool)
		}
	}
	if ask != nil {
		found = append(found, "ask offer")
	}
	return found
}

// Two real agents: qa-node with the team channel off, dev-node with it
// on. qa-jefe may ask dev, but on qa-node it is offered no ask tool and
// asks nothing; the node reports its switch.
func TestANodeWithTheTeamChannelOffNeverAsks(t *testing.T) {
	f := control(t, "completed")
	ctx := context.Background()
	askingTeams(t, f)
	executor := &scriptedExecutor{tool: application.AskToolName}
	runSwitchedNode(t, f, executor, "qa-node", false, "qa")
	runNode(t, f, executor, "dev-node", "dev")
	submitted, err := f.client.SubmitTask(ctx, unixapiSubmit("qa-jefe", "Find who owns the importer"))
	if err != nil {
		t.Fatal(err)
	}
	parent := waitFor(t, f.client, submitted.Task.ID, "Completed")
	if len(parent.Questions) != 0 {
		t.Fatalf("asked from a node with the channel off: %v", parent.Questions)
	}
	executor.mu.Lock()
	for _, request := range executor.seen {
		if request.BotID == "qa-jefe" {
			if found := channelToolsIn(request.Ask, request.AllowedTools, request.NodeTools()); len(found) != 0 {
				t.Errorf("qa-jefe was offered %v", found)
			}
		}
	}
	executor.mu.Unlock()
	off, err := f.client.GetNode(ctx, "qa-node")
	if err != nil || off.Capabilities.TeamChannel {
		t.Errorf("qa-node: %v %+v", err, off.Capabilities)
	}
	on, err := f.client.GetNode(ctx, "dev-node")
	if err != nil || !on.Capabilities.TeamChannel {
		t.Errorf("dev-node: %v %+v", err, on.Capabilities)
	}
	if !strings.Contains(f.logs.String(), "node: team channel on") {
		t.Errorf("logs = %s", f.logs.String())
	}
}

// Two real agents: qa-node with the team channel on asks dev, and
// dev-node shares dev's memory with the channel off: no member of dev may
// answer, and the model reads the refusal.
func TestAMemberOnANodeWithTheTeamChannelOffNeverAnswers(t *testing.T) {
	f := control(t, "completed")
	ctx := context.Background()
	askingTeams(t, f)
	executor := &scriptedExecutor{tool: application.AskToolName, script: map[domain.BotID]map[string]any{"qa-jefe": {"team": "dev", "question": "Who owns the importer?"}}}
	runNode(t, f, executor, "qa-node", "qa")
	runSwitchedNode(t, f, executor, "dev-node", false, "dev")
	submitted, err := f.client.SubmitTask(ctx, unixapiSubmit("qa-jefe", "Find who owns the importer"))
	if err != nil {
		t.Fatal(err)
	}
	parent := waitFor(t, f.client, submitted.Task.ID, "Completed")
	executions, err := f.client.ListExecutions(ctx, parent.ID)
	if err != nil || len(executions) != 1 {
		t.Fatal(err)
	}
	if said := executions[0].Result.Answer; !strings.Contains(said, "asked: error=true") || !strings.Contains(said, domain.RefusedTeamUnavailable) {
		t.Fatalf("the asker read %s (%s)", said, f.logs.String())
	}
	if len(parent.Questions) != 0 {
		t.Errorf("questions %v", parent.Questions)
	}
}

// Defence in depth: a node with the team channel off serves none of the
// channel's tools, nor a watcher's, even when an assignment offers them.
func TestANodeWithTheTeamChannelOffServesNoChannelToolEvenIfOffered(t *testing.T) {
	watching, err := domain.NewRuntime(watcherDefinition("qa-vigia", "qa").Runtime)
	if err != nil {
		t.Fatal(err)
	}
	offered := []string{"local_read", application.AskToolName, application.QuestionStatusToolName}
	for _, on := range []bool{false, true} {
		logs := &bytes.Buffer{}
		recorder := &requestRecorder{}
		agent := Agent{Transport: &fakeTransport{}, Executor: recorder, Options: Options{NodeID: "qa-node", TeamChannel: on, Log: log.New(logs, "", 0)}}
		ask := &application.AskOffer{Teams: []string{"dev"}}
		agent.carry(context.Background(), "qa-node", application.Assignment{ExecutionID: "exec-1", Kind: domain.ExecutionRun,
			Run: &application.ExecutionRequest{ExecutionID: "exec-1", TaskID: "task-a", BotID: "qa-jefe", AllowedTools: offered, Ask: ask}})
		agent.carry(context.Background(), "qa-node", application.Assignment{ExecutionID: "exec-2", Kind: domain.ExecutionDecide,
			Decide: &application.DecisionRequest{ExecutionID: "exec-2", TaskID: "task-a", BotID: "qa-jefe", AllowedTools: offered, Ask: ask}})
		agent.carry(context.Background(), "qa-node", application.Assignment{ExecutionID: "exec-3", Kind: domain.ExecutionRun,
			Run: &application.ExecutionRequest{ExecutionID: "exec-3", TaskID: "task-w", BotID: "qa-vigia", Runtime: watching}})
		if len(recorder.runs) != 2 || len(recorder.decisions) != 1 {
			t.Fatalf("on=%v: runs %d decisions %d", on, len(recorder.runs), len(recorder.decisions))
		}
		run, decision, watcher := recorder.runs[0], recorder.decisions[0], recorder.runs[1]
		if on {
			if run.Ask == nil || decision.Ask == nil || len(run.AllowedTools) != 3 || len(decision.AllowedTools) != 3 || watcher.OnNodeTool == nil {
				t.Errorf("on, the node serves what it is offered: %+v %+v", run, decision)
			}
			continue
		}
		if found := channelToolsIn(run.Ask, run.AllowedTools, run.NodeTools()); len(found) != 0 {
			t.Errorf("the run was served %v", found)
		}
		if found := channelToolsIn(decision.Ask, decision.AllowedTools, decision.NodeTools()); len(found) != 0 {
			t.Errorf("the decision was served %v", found)
		}
		if watcher.OnNodeTool != nil {
			t.Error("a watcher was served its tools")
		}
		if !slices.Equal(offered, []string{"local_read", application.AskToolName, application.QuestionStatusToolName}) {
			t.Errorf("the assignment's own list was changed: %v", offered)
		}
		if !strings.Contains(logs.String(), "the team channel is off on this node") || !strings.Contains(logs.String(), "no watch tools") {
			t.Errorf("logs = %s", logs.String())
		}
	}
}
