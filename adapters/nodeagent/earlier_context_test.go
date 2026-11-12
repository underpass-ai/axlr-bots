package nodeagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// fakeContext is a team's store: the lines under each key, or an error.
type fakeContext struct {
	mu    sync.Mutex
	lines map[string][]string
	err   error
	asked []string
}

func (f *fakeContext) ReadContext(_ context.Context, team domain.TeamName, key domain.ContextKey) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, team.String()+"/"+key.String())
	if f.err != nil {
		return nil, f.err
	}
	lines, ok := f.lines[key.String()]
	if !ok {
		return nil, application.ErrContextNotFound
	}
	return lines, nil
}

// capturingExecutor keeps the request it was given.
type capturingExecutor struct {
	steeringExecutor
	request application.ExecutionRequest
}

func (c *capturingExecutor) Execute(_ context.Context, request application.ExecutionRequest) (application.ExecutionReport, error) {
	c.request = request
	return application.ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "done"}}, nil
}

func contextAgent(store application.ContextReader, executor Executor, transport application.NodeTransport, teams ...string) (Agent, *bytes.Buffer) {
	logs := &bytes.Buffer{}
	return Agent{Transport: transport, Executor: executor, Context: store, Options: Options{NodeID: "b-node", MemoryTeams: teams, Log: log.New(logs, "", 0)}}, logs
}

func redeliveryRun(runtime domain.Runtime, input string, refs ...domain.ContextRef) application.Assignment {
	return application.Assignment{ExecutionID: "exec-1", Kind: domain.ExecutionRun, Run: &application.ExecutionRequest{
		ExecutionID: "exec-1", TaskID: "task-r", BotID: "b-triage", Objective: "Redelivery 1 of 2: triage F-13.", MemoryTeam: "team-b",
		Runtime: runtime, Input: json.RawMessage(input), Contexts: refs,
	}}
}

func TestAnLLMRunIsGivenWhatItsTeamsStoreHoldsUnderTheKey(t *testing.T) {
	store := &fakeContext{lines: map[string][]string{"ws:s1": {"(observation): Finding F-12 triaged as flaky.", "open: triage F-13"}}}
	executor, transport := &capturingExecutor{}, &fakeTransport{}
	agent, _ := contextAgent(store, executor, transport, "team-b")
	agent.carry(context.Background(), "b-node", redeliveryRun(domain.Runtime{}, `{"name":"team-b.item.arrived"}`,
		domain.ContextRef{Key: "ws:s1", Team: "team-b"}, domain.ContextRef{Key: "project:team-a.fix.7", Team: "team-a"}))
	prompt := executor.request.PromptText()
	for _, want := range []string{
		"Redelivery 1 of 2: triage F-13.\n\n" + domain.EarlierContextHeading,
		"ws:s1 (team team-b):\n- (observation): Finding F-12 triaged as flaky.\n- open: triage F-13",
		"project:team-a.fix.7 (team team-a): held by team team-a, not yours: ask that team through the team channel, naming the key.",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, prompt)
		}
	}
	if len(store.asked) != 1 || store.asked[0] != "team-b/ws:s1" {
		t.Errorf("asked %v: another team's key was read", store.asked)
	}
	reads := transport.report.ContextRead
	if len(reads) != 2 || len(reads[0].Read) != 2 || reads[1].Line == "" || reads[1].Read != nil {
		t.Errorf("report = %+v", reads)
	}
}

func TestAJevProgramIsGivenTheSameAsDataEarlier(t *testing.T) {
	program, err := domain.NewRuntime(&domain.RuntimeSnapshot{Kind: "jev", Jev: &domain.JevProgramSnapshot{Server: "s", Start: "a", Wait: "w", Act: domain.JevActSnapshot{Tool: "b", Choice: "c", Pass: []string{"id", "earlier"}}}})
	if err != nil {
		t.Fatal(err)
	}
	store := &fakeContext{lines: map[string][]string{"ws:s1": {"F-12 flaky"}}}
	executor, transport := &capturingExecutor{}, &fakeTransport{}
	agent, _ := contextAgent(store, executor, transport, "*")
	agent.carry(context.Background(), "b-node", redeliveryRun(program, `{"name":"team-b.item.arrived","data":{"id":"F-13","context":"ws:s1"}}`, domain.ContextRef{Key: "ws:s1", Team: "team-b"}))
	if executor.request.Prompt != "" {
		t.Errorf("a Jev run got a prompt: %q", executor.request.Prompt)
	}
	var input struct {
		Name string `json:"name"`
		Data struct {
			ID      string               `json:"id"`
			Context string               `json:"context"`
			Earlier []domain.ContextRead `json:"earlier"`
		} `json:"data"`
	}
	if err := json.Unmarshal(executor.request.Input, &input); err != nil {
		t.Fatal(err)
	}
	if input.Name != "team-b.item.arrived" || input.Data.ID != "F-13" || input.Data.Context != "ws:s1" || len(input.Data.Earlier) != 1 || input.Data.Earlier[0].Read[0] != "F-12 flaky" || input.Data.Earlier[0].Key != "ws:s1" {
		t.Errorf("input = %s", executor.request.Input)
	}
	if len(transport.report.ContextRead) != 1 {
		t.Errorf("report = %+v", transport.report.ContextRead)
	}
}

func TestWhatCannotBeReadIsOneLineNeverAFailedRun(t *testing.T) {
	ref := domain.ContextRef{Key: "ws:s1", Team: "team-b"}
	cases := map[string]struct {
		store application.ContextReader
		teams []string
		want  string
	}{
		"missing":    {&fakeContext{lines: map[string][]string{}}, []string{"team-b"}, "the store of team team-b holds nothing under this key."},
		"failing":    {&fakeContext{err: errors.New("kmp-mcp: the store is locked\nmore")}, []string{"team-b"}, "could not be read: kmp-mcp: the store is locked"},
		"empty":      {&fakeContext{lines: map[string][]string{"ws:s1": {}}}, []string{"team-b"}, "nothing recorded under this key yet."},
		"not shared": {&fakeContext{}, []string{"team-a"}, "not read: this node does not open the memory of team team-b."},
		"no reader":  {nil, []string{"team-b"}, "not read: this node reads no team memory"},
	}
	for name, c := range cases {
		executor, transport := &capturingExecutor{}, &fakeTransport{}
		agent, _ := contextAgent(c.store, executor, transport, c.teams...)
		agent.carry(context.Background(), "b-node", redeliveryRun(domain.Runtime{}, `{}`, ref))
		if transport.report.Outcome != domain.ExecutionCompleted {
			t.Errorf("%s: report %+v", name, transport.report)
		}
		if reads := transport.report.ContextRead; len(reads) != 1 || !strings.HasPrefix(reads[0].Line, c.want) || reads[0].Read != nil {
			t.Errorf("%s: reads = %+v", name, reads)
		}
		if !strings.Contains(executor.request.PromptText(), "ws:s1 (team team-b): "+c.want) {
			t.Errorf("%s: prompt = %s", name, executor.request.PromptText())
		}
	}
}

func TestAnOversizedReadIsBoundedNewestFirst(t *testing.T) {
	var lines []string
	for i := 0; i < 100; i++ {
		lines = append(lines, "line "+strings.Repeat("x", 60))
	}
	lines[0] = "newest"
	store := &fakeContext{lines: map[string][]string{"ws:s1": lines}}
	executor, transport := &capturingExecutor{}, &fakeTransport{}
	agent, _ := contextAgent(store, executor, transport, "team-b")
	assignment := redeliveryRun(domain.Runtime{}, `{}`, domain.ContextRef{Key: "ws:s1", Team: "team-b"})
	// A prompt near its limit leaves the block little room.
	assignment.Run.Objective = domain.Objective(strings.Repeat("o", domain.MaxPromptLength-600))
	agent.carry(context.Background(), "b-node", assignment)
	read := transport.report.ContextRead[0]
	total := 0
	for _, line := range read.Read {
		total += len(line) + 1
	}
	if read.Read[0] != "newest" || total > domain.MaxContextReadBytes || len(read.Read) == len(lines) {
		t.Errorf("read %d lines, %d bytes, first %q", len(read.Read), total, read.Read[0])
	}
	if prompt := executor.request.PromptText(); len(prompt) > domain.MaxPromptLength || !strings.HasSuffix(prompt, "…") || !strings.Contains(prompt, "- newest") {
		t.Errorf("prompt (%d) ends %q", len(prompt), prompt[len(prompt)-40:])
	}
}

func TestARunWithoutKeysIsUntouched(t *testing.T) {
	executor, transport := &capturingExecutor{}, &fakeTransport{}
	store := &fakeContext{}
	agent, _ := contextAgent(store, executor, transport, "team-b")
	agent.carry(context.Background(), "b-node", redeliveryRun(domain.Runtime{}, `{"a":1}`))
	if executor.request.Prompt != "" || string(executor.request.Input) != `{"a":1}` || transport.report.ContextRead != nil || len(store.asked) != 0 {
		t.Errorf("request %+v report %+v", executor.request, transport.report)
	}
	if got := inputWithEarlier(json.RawMessage(`[1]`), []domain.ContextRead{{Key: "ws:s1", Team: "team-b", Line: "x"}}); string(got) != `[1]` {
		t.Errorf("an input that is no object = %s", got)
	}
	if got := inputWithEarlier(nil, []domain.ContextRead{{Key: "ws:s1", Team: "team-b", Line: "x"}}); !strings.Contains(string(got), `"earlier"`) {
		t.Errorf("no input = %s", got)
	}
}

func TestAReadThePromptHasNoRoomForIsSaidSo(t *testing.T) {
	store := &fakeContext{lines: map[string][]string{"ws:s1": {"F-12 flaky"}}}
	executor, transport := &capturingExecutor{}, &fakeTransport{}
	agent, _ := contextAgent(store, executor, transport, "team-b")
	full := redeliveryRun(domain.Runtime{}, `{}`, domain.ContextRef{Key: "ws:s1", Team: "team-b"})
	full.Run.Objective = domain.Objective(strings.Repeat("o", domain.MaxPromptLength-10))
	agent.carry(context.Background(), "b-node", full)
	if executor.request.Prompt != "" {
		t.Errorf("a full prompt grew: %d", len(executor.request.Prompt))
	}
	if reads := transport.report.ContextRead; len(reads) != 1 || reads[0].Read != nil || !strings.Contains(reads[0].Line, "no room") {
		t.Errorf("reads = %+v", reads)
	}
}
