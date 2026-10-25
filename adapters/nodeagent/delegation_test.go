package nodeagent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/unixapi"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// mcpClient speaks to the agent's socket as AXLR does through the relay.
type mcpClient struct {
	conn    net.Conn
	scanner *bufio.Scanner
	next    int
}

func dialMCP(t *testing.T, socket string) *mcpClient {
	t.Helper()
	conn, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	return &mcpClient{conn: conn, scanner: scanner}
}

func (c *mcpClient) send(line string) { _, _ = c.conn.Write([]byte(line + "\n")) }

// request sends method with params and reads until its answer.
func (c *mcpClient) request(method string, params any) (map[string]any, error) {
	c.next++
	id := c.next
	data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	c.send(string(data))
	for c.scanner.Scan() {
		var answer map[string]any
		if err := json.Unmarshal(c.scanner.Bytes(), &answer); err != nil {
			return nil, err
		}
		if fmt.Sprint(answer["id"]) == fmt.Sprint(id) {
			return answer, nil
		}
	}
	return nil, io.EOF
}

// callDelegate initializes, lists the tools and calls the delegation
// tool; the text of the result and whether it is an error.
func callDelegate(socket string, arguments map[string]any) (string, bool, error) {
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return "", false, err
	}
	defer conn.Close()
	c := &mcpClient{conn: conn, scanner: bufio.NewScanner(conn)}
	c.scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	if _, err := c.request("initialize", map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test"}}); err != nil {
		return "", false, err
	}
	c.send(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	if _, err := c.request("tools/list", map[string]any{}); err != nil {
		return "", false, err
	}
	answer, err := c.request("tools/call", map[string]any{"name": application.DelegateToolName, "arguments": arguments})
	if err != nil {
		return "", false, err
	}
	result, _ := answer["result"].(map[string]any)
	if result == nil {
		return "", false, fmt.Errorf("no result: %v", answer)
	}
	content, _ := result["content"].([]any)
	first, _ := content[0].(map[string]any)
	return fmt.Sprint(first["text"]), result["isError"] == true, nil
}

// scriptedExecutor stands in for AXLR: an execution with the delegation
// tool calls it as its bot's script says and answers with what it read.
type scriptedExecutor struct {
	mu     sync.Mutex
	script map[domain.BotID]map[string]any
	seen   []application.ExecutionRequest
}

func (s *scriptedExecutor) Execute(ctx context.Context, request application.ExecutionRequest) (application.ExecutionReport, error) {
	s.mu.Lock()
	s.seen = append(s.seen, request)
	arguments, asks := s.script[request.BotID]
	s.mu.Unlock()
	answer := "I am " + request.BotID.String()
	if asks {
		if request.DelegationSocket == "" {
			return application.ExecutionReport{}, errors.New("no delegation socket for " + request.BotID.String())
		}
		text, isError, err := callDelegate(request.DelegationSocket, arguments)
		if err != nil {
			return application.ExecutionReport{}, err
		}
		answer = fmt.Sprintf("%s; asked: error=%v %s", answer, isError, text)
	}
	return application.ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: answer}}, nil
}

func (s *scriptedExecutor) Decide(context.Context, application.DecisionRequest) (application.ExecutionReport, error) {
	return application.ExecutionReport{}, errors.New("no decisions here")
}
func (s *scriptedExecutor) OpenDelivered(domain.BotID, string) (io.ReadCloser, error) {
	return nil, os.ErrNotExist
}
func (s *scriptedExecutor) SupportsPolicy(context.Context) bool      { return true }
func (s *scriptedExecutor) SupportsCorrelation(context.Context) bool { return true }
func (s *scriptedExecutor) Version(context.Context) string           { return "scripted" }

// delegatingFleet registers bot-a, which may ask bot-b, and bot-b, which
// may ask bot-a back when back is true; both take delegated tasks.
func delegatingFleet(t *testing.T, f *fixture, back bool) {
	t.Helper()
	ctx := context.Background()
	for _, bot := range []struct {
		id, to string
	}{{"bot-a", "bot-b"}, {"bot-b", "bot-a"}} {
		definition := application.BotDefinition{
			ID: bot.id, Tenant: "t", Name: bot.id, Owner: "o", Mission: "answer", AllowedTools: []string{"local_read"},
			Triggers: []domain.TriggerSnapshot{{Type: "manual"}, {Type: "delegation"}},
			Budgets:  application.BudgetsDefinition{TaskTimeoutSeconds: 120},
		}
		if bot.id == "bot-a" || back {
			definition.Delegation = &domain.DelegationSnapshot{To: []string{bot.to}, MaxDepth: 1}
		}
		if _, err := f.client.RegisterBot(ctx, definition); err != nil {
			t.Fatal(err)
		}
		if _, err := f.client.EnableBot(ctx, bot.id); err != nil {
			t.Fatal(err)
		}
	}
}

func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "axn")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func runAgent(t *testing.T, f *fixture, executor Executor) context.CancelFunc {
	t.Helper()
	agent := Agent{
		Transport: f.client,
		Executor:  executor,
		Options:   Options{NodeID: "web-1", HostName: "web-1", Heartbeat: 50 * time.Millisecond, PollWait: 200 * time.Millisecond, Renew: 50 * time.Millisecond, SocketDir: shortDir(t) + "/run", Log: log.New(f.logs, "", 0)},
	}
	ctx, stop := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- agent.Run(ctx) }()
	t.Cleanup(func() {
		stop()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})
	return stop
}

func TestAgentRunsTheChildOnTheSameNodeWhileTheParentWaits(t *testing.T) {
	f := control(t, "completed")
	ctx := context.Background()
	delegatingFleet(t, f, false)
	executor := &scriptedExecutor{script: map[domain.BotID]map[string]any{"bot-a": {"bot": "bot-b", "objective": "Say who you are"}}}
	runAgent(t, f, executor)
	submitted, err := f.client.SubmitTask(ctx, unixapiSubmit("bot-a", "Ask bot-b who it is"))
	if err != nil {
		t.Fatal(err)
	}
	parent := waitFor(t, f.client, submitted.Task.ID, "Completed")
	if len(parent.Children) != 1 {
		t.Fatalf("children = %v (%s)", parent.Children, f.logs.String())
	}
	child := waitFor(t, f.client, parent.Children[0], "Completed")
	if child.Delegation == nil || child.Delegation.ParentTaskID != parent.ID || child.Delegation.Depth != 1 || child.CorrelationID != parent.CorrelationID {
		t.Errorf("child = %+v", child)
	}
	executions, err := f.client.ListExecutions(ctx, parent.ID)
	if err != nil || len(executions) != 1 {
		t.Fatal(err)
	}
	answer := executions[0].Result.Answer
	if !strings.Contains(answer, `asked: error=false`) || !strings.Contains(answer, `"answer":"I am bot-b"`) || !strings.Contains(answer, `"status":"Completed"`) {
		t.Errorf("parent's answer = %s", answer)
	}
	executor.mu.Lock()
	defer executor.mu.Unlock()
	for _, request := range executor.seen {
		switch request.BotID {
		case "bot-a":
			if request.Delegation == nil || request.Delegation.Targets[0] != "bot-b" || !containsTool(request.AllowedTools) {
				t.Errorf("bot-a's request = %+v", request)
			}
		case "bot-b":
			if request.Delegation != nil || request.DelegationSocket != "" {
				t.Errorf("bot-b may not delegate: %+v", request)
			}
		}
	}
	if !strings.Contains(f.logs.String(), "may delegate to bot-b") || !strings.Contains(f.logs.String(), "ended Completed") {
		t.Errorf("logs = %s", f.logs.String())
	}
}

func TestAgentHandsTheModelTheRefusalOfALoop(t *testing.T) {
	f := control(t, "completed")
	ctx := context.Background()
	delegatingFleet(t, f, true)
	executor := &scriptedExecutor{script: map[domain.BotID]map[string]any{
		"bot-a": {"bot": "bot-b", "objective": "Ask bot-a back"},
		"bot-b": {"bot": "bot-a", "objective": "Answer bot-b"},
	}}
	runAgent(t, f, executor)
	submitted, err := f.client.SubmitTask(ctx, unixapiSubmit("bot-a", "Start the loop"))
	if err != nil {
		t.Fatal(err)
	}
	parent := waitFor(t, f.client, submitted.Task.ID, "Completed")
	child := waitFor(t, f.client, parent.Children[0], "Completed")
	if len(child.Children) != 0 {
		t.Errorf("the loop went on: %v", child.Children)
	}
	executions, _ := f.client.ListExecutions(ctx, child.ID)
	if answer := executions[0].Result.Answer; !strings.Contains(answer, "asked: error=true") || !strings.Contains(answer, domain.RefusedDepthExceeded) {
		t.Errorf("bot-b read = %s", answer)
	}
	events, err := f.client.ListEvents(ctx, "bot-b", 50)
	if err != nil {
		t.Fatal(err)
	}
	refused := false
	for _, event := range events {
		refused = refused || (event.Type == domain.EventDelegationRefused && event.Payload["reason"] == domain.RefusedDepthExceeded)
	}
	if !refused {
		t.Errorf("no refusal event among %d", len(events))
	}
}

func unixapiSubmit(bot, objective string) unixapi.SubmitRequest {
	return unixapi.SubmitRequest{BotID: bot, Objective: objective, IdempotencyKey: "k-" + bot}
}

func containsTool(tools []string) bool {
	for _, tool := range tools {
		if tool == application.DelegateToolName {
			return true
		}
	}
	return false
}
