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

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

// callTool initializes, lists the tools and calls one; the tools
// tools/list gave, the text of the result and whether it is an error.
func callTool(socket, name string, arguments map[string]any) ([]map[string]any, string, bool, error) {
	conn, err := net.Dial("unix", socket)
	if err != nil {
		return nil, "", false, err
	}
	defer conn.Close()
	c := &mcpClient{conn: conn, scanner: bufio.NewScanner(conn)}
	c.scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	if _, err := c.request("initialize", map[string]any{"protocolVersion": "2025-11-25"}); err != nil {
		return nil, "", false, err
	}
	listed, err := c.request("tools/list", map[string]any{})
	if err != nil {
		return nil, "", false, err
	}
	var tools []map[string]any
	for _, tool := range listed["result"].(map[string]any)["tools"].([]any) {
		tools = append(tools, tool.(map[string]any))
	}
	answer, err := c.request("tools/call", map[string]any{"name": name, "arguments": arguments})
	if err != nil {
		return nil, "", false, err
	}
	result, _ := answer["result"].(map[string]any)
	if result == nil {
		return tools, "", false, fmt.Errorf("no result: %v", answer)
	}
	first := result["content"].([]any)[0].(map[string]any)
	return tools, fmt.Sprint(first["text"]), result["isError"] == true, nil
}

// emittingExecutor stands in for AXLR: the bot its script names calls
// the emit tool with the arguments given; every other bot answers.
type emittingExecutor struct {
	mu     sync.Mutex
	script map[domain.BotID]map[string]any
	tools  map[domain.BotID][]string
	listed map[domain.BotID][]map[string]any
	seen   []application.ExecutionRequest
}

func (s *emittingExecutor) Execute(ctx context.Context, request application.ExecutionRequest) (application.ExecutionReport, error) {
	s.mu.Lock()
	s.seen = append(s.seen, request)
	arguments, emits := s.script[request.BotID]
	s.mu.Unlock()
	answer := "I am " + request.BotID.String() + ": " + request.Objective.String()
	if emits {
		tools, text, isError, err := callTool(request.DelegationSocket, application.EmitEventToolName, arguments)
		if err != nil {
			return application.ExecutionReport{}, err
		}
		var names []string
		for _, tool := range tools {
			names = append(names, fmt.Sprint(tool["name"]))
		}
		s.mu.Lock()
		s.tools[request.BotID] = names
		if s.listed == nil {
			s.listed = map[domain.BotID][]map[string]any{}
		}
		s.listed[request.BotID] = tools
		s.mu.Unlock()
		answer = fmt.Sprintf("emitted: error=%v %s", isError, text)
	}
	return application.ExecutionReport{Outcome: domain.ExecutionCompleted, Result: domain.Result{Answer: answer}}, nil
}

func (s *emittingExecutor) Decide(context.Context, application.DecisionRequest) (application.ExecutionReport, error) {
	return application.ExecutionReport{}, errors.New("no decisions here")
}
func (s *emittingExecutor) OpenDelivered(domain.BotID, string) (io.ReadCloser, error) {
	return nil, os.ErrNotExist
}
func (s *emittingExecutor) SupportsPolicy(context.Context) bool      { return true }
func (s *emittingExecutor) SupportsCorrelation(context.Context) bool { return true }
func (s *emittingExecutor) Version(context.Context) string           { return "scripted" }

func TestThePlaneWarnsQuadOneThroughTheNode(t *testing.T) {
	f := control(t, "completed")
	ctx := context.Background()
	for _, id := range []string{"plane", "quad-1"} {
		definition := application.BotDefinition{
			ID: id, Tenant: "t", Name: id, Owner: "o", Mission: "You are " + id + ".", AllowedTools: []string{"local_read"},
			Groups: []string{"port"}, Budgets: application.BudgetsDefinition{TaskTimeoutSeconds: 120},
		}
		if _, err := f.client.RegisterBot(ctx, definition); err != nil {
			t.Fatal(err)
		}
		if _, err := f.client.EnableBot(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	executor := &emittingExecutor{tools: map[domain.BotID][]string{}, script: map[domain.BotID]map[string]any{
		"plane": {"name": "stop.recall", "message": "Ship 244 is entering your zone", "severity": "warning", "to": []string{"quad-1"}},
	}}
	runAgent(t, f, executor)
	submitted, err := f.client.SubmitTask(ctx, unixapiSubmit("plane", "Tell quad-1 to pull back: a ship"))
	if err != nil {
		t.Fatal(err)
	}
	plane := waitFor(t, f.client, submitted.Task.ID, "Completed")
	executions, _ := f.client.ListExecutions(ctx, plane.ID)
	answer := executions[0].Result.Answer
	if !strings.Contains(answer, "emitted: error=false") || !strings.Contains(answer, `"botId":"quad-1"`) {
		t.Fatalf("plane read = %s", answer)
	}
	var view struct {
		Tasks []application.EventDelivery `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(answer, "emitted: error=false ")), &view); err != nil || len(view.Tasks) != 1 {
		t.Fatalf("view = %+v %v", view, err)
	}
	quad := waitFor(t, f.client, view.Tasks[0].TaskID, "Completed")
	if quad.Delegation == nil || quad.Delegation.EventID == "" || quad.CorrelationID != plane.CorrelationID {
		t.Errorf("quad-1's task = %+v", quad)
	}
	if tools := executor.tools["plane"]; len(tools) != 3 || tools[1] != application.EmitEventToolName || tools[2] != application.SubscribeToolName {
		t.Errorf("tools/list = %v", tools)
	}
	// The emit tool over the socket offers no severity and names the kinds.
	emitListed := executor.listed["plane"][1]
	properties := emitListed["inputSchema"].(map[string]any)["properties"].(map[string]any)
	if _, ok := properties["severity"]; ok || !strings.Contains(fmt.Sprint(emitListed["description"]), "stop. and danger. are critical") {
		t.Errorf("emit tool = %v", emitListed)
	}
	events, err := f.client.ListGroupEvents(ctx, application.GroupEventsQuery{Name: "port"})
	if err != nil {
		t.Fatal(err)
	}
	emitted := false
	for _, event := range events {
		emitted = emitted || (event.Type == domain.EventEventEmitted && event.Payload["message"] == "Ship 244 is entering your zone")
	}
	if !emitted {
		t.Errorf("no emitted event among %d", len(events))
	}
	if !strings.Contains(f.logs.String(), "may emit events to quad-1") {
		t.Errorf("logs = %s", f.logs.String())
	}
}

// emitTransport answers emits as its fields say.
type emitTransport struct {
	fakeTransport
	answer application.EmitAnswer
	err    error
}

func (e *emitTransport) Emit(context.Context, domain.NodeID, domain.ExecutionID, application.EmitEventCommand) (application.EmitAnswer, error) {
	return e.answer, e.err
}

func TestTheEmitterHandsTheModelWhatHappened(t *testing.T) {
	transport := &emitTransport{}
	e := emitter{transport: transport, node: "web-1", execution: "exec-1", log: log.New(io.Discard, "", 0)}
	if result := e.call(context.Background(), json.RawMessage(`{"name": ""}`)); !result.IsError {
		t.Error("an empty name was taken")
	}
	transport.err = errors.New("down")
	if result := e.call(context.Background(), json.RawMessage(`{"name": "recall", "message": "x"}`)); !result.IsError || !strings.Contains(result.Content[0].Text, "down") {
		t.Errorf("transport error = %+v", result)
	}
	transport.err = nil
	transport.answer = application.EmitAnswer{Refused: &domain.EventRefusal{Reason: domain.RefusedRateLimited, Message: "wait"}}
	if result := e.call(context.Background(), json.RawMessage(`{"name": "recall", "message": "x"}`)); !result.IsError || !strings.Contains(result.Content[0].Text, domain.RefusedRateLimited) {
		t.Errorf("refusal = %+v", result)
	}
	transport.answer = application.EmitAnswer{EventID: "evt-1", Name: "recall", Severity: domain.SeverityInfo}
	if result := e.call(context.Background(), json.RawMessage(`{"name": "recall", "message": "x"}`)); result.IsError || !strings.Contains(result.Content[0].Text, "no teammate heard it") {
		t.Errorf("nobody = %+v", result)
	}
}

func TestTheEmitToolDescribesWhatTheBotMayDo(t *testing.T) {
	tool := emitTool(application.EventsOffer{Names: []string{"recall", "ship.*"}, Teammates: []string{"quad-1"}})
	var schema struct {
		Properties map[string]map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(tool.InputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if _, ok := schema.Properties["severity"]; ok {
		t.Errorf("the schema offers a severity: %s", tool.InputSchema)
	}
	for _, kind := range []string{"stop.", "danger.", "warn.", "info.", "error.", "done."} {
		if !strings.Contains(tool.Description, kind) {
			t.Errorf("the description does not name the kind %s", kind)
		}
	}
	if items := schema.Properties["to"]["items"].(map[string]any); fmt.Sprint(items["enum"]) != "[quad-1]" {
		t.Errorf("to = %v", items)
	}
	if !strings.Contains(tool.Description, "recall, ship.*") || !strings.Contains(tool.Description, "Your teammates: quad-1") {
		t.Errorf("description = %s", tool.Description)
	}
	alone := emitTool(application.EventsOffer{Names: []string{"**"}})
	if !strings.Contains(alone.Description, "none Enabled now") || strings.Contains(string(alone.InputSchema), `"enum":["quad`) {
		t.Errorf("alone = %s %s", alone.Description, alone.InputSchema)
	}
}
