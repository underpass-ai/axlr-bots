package nodeagent

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// fakeMemory is the team channel's writer: it keeps what it was asked to
// write.
type fakeMemory struct {
	mu      sync.Mutex
	written []domain.ChannelRecord
	err     error
}

func (f *fakeMemory) WriteChannelRecord(_ context.Context, record domain.ChannelRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.written = append(f.written, record)
	return nil
}

func (f *fakeMemory) records() []domain.ChannelRecord {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.ChannelRecord(nil), f.written...)
}

// memoryTransport answers a report with the answering side's record and
// a message read with the asking side's.
type memoryTransport struct {
	*fakeTransport
	ack      *domain.ChannelRecord
	messages []application.RelayedMessage
}

func (m *memoryTransport) Report(ctx context.Context, node domain.NodeID, execution domain.ExecutionID, report application.ExecutionReport) (application.ReportAck, error) {
	ack, err := m.fakeTransport.Report(ctx, node, execution, report)
	ack.ChannelMemory = m.ack
	return ack, err
}

func (m *memoryTransport) ExecutionMessages(context.Context, domain.NodeID, domain.ExecutionID, string) ([]application.RelayedMessage, error) {
	return m.messages, nil
}

// steeringExecutor steers and runs at once; nothing else of an executor.
type steeringExecutor struct {
	Executor
	mu      sync.Mutex
	steered []string
}

func (s *steeringExecutor) Steer(_ context.Context, _ domain.ExecutionID, texts []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.steered = append(s.steered, texts...)
	return nil
}

func (s *steeringExecutor) Execute(context.Context, application.ExecutionRequest) (application.ExecutionReport, error) {
	return application.ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: "done"}}, nil
}

func channelRecord(side domain.ChannelSide, team, other domain.TeamName) domain.ChannelRecord {
	return domain.ChannelRecord{
		Side: side, Team: team, Other: other, Question: "task-q1", Asker: "qa-jefe", Responder: "dev-go",
		Text: "Who owns the importer?", Answered: true, Answer: "Alice.", Status: domain.TaskCompleted,
	}
}

func TestTheNodeWritesOnlyTheTeamsWhoseMemoryItShares(t *testing.T) {
	logs := &bytes.Buffer{}
	memory := &fakeMemory{}
	agent := Agent{Memory: memory, Options: Options{NodeID: "dev-node", MemoryTeams: []string{"dev"}, Log: log.New(logs, "", 0)}}
	answered, asked := channelRecord(domain.ChannelAnswered, "dev", "qa"), channelRecord(domain.ChannelAsked, "qa", "dev")
	agent.remember(&answered, "the report")
	agent.remember(&asked, "the answer's message")
	agent.remember(nil, "nothing")
	if got := memory.records(); len(got) != 1 || got[0] != answered {
		t.Fatalf("written %+v", got)
	}
	if !strings.Contains(logs.String(), "question task-q1 (answered, from the report) written into kmp/dev, about project:dev.canal") ||
		!strings.Contains(logs.String(), "question task-q1 (asked, from the answer's message) not written: this node does not share the memory of team qa") {
		t.Fatalf("logs:\n%s", logs)
	}
	agent.Options.MemoryTeams = []string{domain.AllMemoryTeams}
	agent.remember(&asked, "the answer's task")
	if got := memory.records(); len(got) != 2 {
		t.Fatalf("written %+v", got)
	}
	// A write that fails is a line in the log, nothing more.
	memory.err = errors.New("kmp-mcp: the store is locked")
	agent.remember(&asked, "the answer's task")
	if !strings.Contains(logs.String(), "not written into kmp/qa: kmp-mcp: the store is locked") {
		t.Fatalf("logs:\n%s", logs)
	}
	// Without the switch, nothing is written and nothing is said.
	off := Agent{Options: Options{MemoryTeams: []string{"*"}, Log: log.New(logs, "", 0)}}
	before := logs.Len()
	off.remember(&asked, "the report")
	off.goRemember(&asked, "the report")
	if logs.Len() != before {
		t.Fatalf("an agent without memory said: %s", logs.String()[before:])
	}
}

func TestTheReportsAnswerIsWrittenIntoTheAnsweringTeamsStore(t *testing.T) {
	answered := channelRecord(domain.ChannelAnswered, "dev", "qa")
	transport := &memoryTransport{fakeTransport: &fakeTransport{}, ack: &answered}
	memory := &fakeMemory{}
	agent := Agent{Transport: transport, Memory: memory, Options: Options{NodeID: "dev-node", MemoryTeams: []string{"dev"}, Log: log.New(&bytes.Buffer{}, "", 0)}}
	agent.report("dev-node", "exec-1", application.ExecutionReport{Outcome: domain.ExecutionCompleted})
	if got := memory.records(); len(got) != 1 || got[0] != answered {
		t.Fatalf("written %+v", got)
	}
}

func TestARelayedAnswerIsWrittenIntoTheAskingTeamsStore(t *testing.T) {
	asked := channelRecord(domain.ChannelAsked, "qa", "dev")
	snapshot := asked.Snapshot()
	broken := snapshot
	broken.Team = "dev"
	transport := &memoryTransport{fakeTransport: &fakeTransport{}, messages: []application.RelayedMessage{
		{ID: "m1", Text: "Team dev answered your question", Memory: &snapshot},
		{ID: "m2", Text: "from the operator"},
		{ID: "m3", Text: "a record that is not one", Memory: &broken},
	}}
	memory := &fakeMemory{}
	logs := &bytes.Buffer{}
	steering := &steeringExecutor{}
	agent := Agent{Transport: transport, Executor: steering, Memory: memory, Options: Options{NodeID: "qa-node", MemoryTeams: []string{"qa"}, Log: log.New(logs, "", 0)}}
	if cursor := agent.relay(context.Background(), "qa-node", "exec-1", ""); cursor != "m3" {
		t.Fatalf("cursor %q", cursor)
	}
	if got := memory.records(); len(got) != 1 || got[0] != asked {
		t.Fatalf("written %+v", got)
	}
	if len(steering.steered) != 3 || !strings.Contains(logs.String(), "team channel record of message m3 dropped") {
		t.Fatalf("steered %v, logs:\n%s", steering.steered, logs)
	}
}

func TestAnAnswersRunWritesItsRecordWhileItRuns(t *testing.T) {
	asked := channelRecord(domain.ChannelAsked, "qa", "dev")
	transport := &memoryTransport{fakeTransport: &fakeTransport{}}
	memory := &fakeMemory{}
	agent := Agent{Transport: transport, Executor: &steeringExecutor{}, Memory: memory, Options: Options{NodeID: "qa-node", MemoryTeams: []string{"qa"}, Log: log.New(&bytes.Buffer{}, "", 0)}, wg: &sync.WaitGroup{}}
	agent.carry(context.Background(), "qa-node", application.Assignment{
		ExecutionID: "exec-1", Kind: domain.ExecutionRun,
		Run: &application.ExecutionRequest{ExecutionID: "exec-1", TaskID: "task-a", BotID: "qa-jefe", ChannelMemory: &asked},
	})
	agent.wg.Wait()
	if got := memory.records(); len(got) != 1 || got[0] != asked {
		t.Fatalf("written %+v", got)
	}
	if transport.reports != 1 || transport.report.Outcome != domain.ExecutionCompleted {
		t.Fatalf("report %+v", transport.report)
	}
}
