package unixapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/compose"
	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/nodeproto"
)

type stubModels struct{ asked []application.ModelCall }

func (s *stubModels) Complete(_ context.Context, call application.ModelCall) (application.ModelAnswer, error) {
	s.asked = append(s.asked, call)
	return application.ModelAnswer{Text: "done", PromptTokens: 3, CompletionTokens: 2, ToolCalls: []application.ModelToolCall{
		{CallID: "call-1", Name: "gpio_write", Arguments: json.RawMessage(`{"pin":"2"}`)},
	}}, nil
}

type stubJudge struct{}

func (stubJudge) Choose(context.Context, application.JudgeQuestion) (application.JudgeVerdict, error) {
	return application.JudgeVerdict{Choice: "water", Probability: 0.8, Confidence: 0.6}, nil
}

// proxied is a control plane with the gateways, a node "web-1" holding
// one running execution, and the node routes behind httptest.
func proxied(t *testing.T) (*httptest.Server, *stubModels, domain.ExecutionID) {
	t.Helper()
	ctx := context.Background()
	deps, err := compose.Dependencies(filepath.Join(t.TempDir(), "state"), "/nonexistent/axlr-tui", "test:api")
	if err != nil {
		t.Fatal(err)
	}
	models := &stubModels{}
	deps.ModelGateway = models
	deps.JudgeGateway = stubJudge{}
	local := NewLocal(deps)
	if _, err := local.RegisterBot(ctx, definition("bot-a", "observe")); err != nil {
		t.Fatal(err)
	}
	if _, err := local.EnableBot(ctx, "bot-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := local.SubmitTask(ctx, SubmitRequest{BotID: "bot-a", Objective: "light", IdempotencyKey: "k-1"}); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(NewServer(local).Handler())
	t.Cleanup(server.Close)
	if status, body := post(t, server.URL+"/v1/nodes", `{"id":"web-1","hostName":"web-1.example"}`); status >= 300 {
		t.Fatalf("register: %d %s", status, body)
	}
	response, err := http.Get(server.URL + "/v1/nodes/web-1/assignments/next")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var assignment nodeproto.AssignmentMessage
	if err := json.NewDecoder(response.Body).Decode(&assignment); err != nil {
		t.Fatalf("assignment (%d): %v", response.StatusCode, err)
	}
	return server, models, domain.ExecutionID(assignment.ExecutionID)
}

func post(t *testing.T, url, body string) (int, string) {
	t.Helper()
	response, err := http.Post(url, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var out strings.Builder
	buffer := make([]byte, 4096)
	for {
		n, readErr := response.Body.Read(buffer)
		out.Write(buffer[:n])
		if readErr != nil {
			break
		}
	}
	return response.StatusCode, out.String()
}

func TestTheModelRouteAnswersTheNodeHoldingTheExecution(t *testing.T) {
	server, models, execution := proxied(t)
	url := server.URL + "/v1/nodes/web-1/executions/" + execution.String()
	status, body := post(t, url+"/model", `{"messages":[{"role":"user","content":"light"}],"tools":[{"name":"gpio_write","description":"d","parameters":{"type":"object"}}]}`)
	if status != http.StatusOK {
		t.Fatalf("model: %d %s", status, body)
	}
	var reply nodeproto.ModelReply
	if err := json.Unmarshal([]byte(body), &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Text != "done" || reply.PromptTokens != 3 || len(reply.ToolCalls) != 1 || reply.ToolCalls[0].Name != "gpio_write" {
		t.Errorf("reply = %+v", reply)
	}
	if len(models.asked) != 1 || models.asked[0].Messages[0].Content != "light" || models.asked[0].Tools[0].Name != "gpio_write" {
		t.Errorf("asked = %+v", models.asked)
	}
	status, body = post(t, url+"/jev", `{"state":"dry","question":"water?","options":["water","wait"]}`)
	var verdict nodeproto.JevReply
	if err := json.Unmarshal([]byte(body), &verdict); err != nil || status != http.StatusOK || verdict.Choice != "water" {
		t.Errorf("jev: %d %v %s", status, err, body)
	}
}

func TestTheModelRouteRefusesWhatIsNotTheNodes(t *testing.T) {
	server, models, execution := proxied(t)
	calls := []struct {
		name, path, body string
		want             int
	}{
		{"another node, model", "/v1/nodes/db-1/executions/" + execution.String() + "/model", `{}`, http.StatusForbidden},
		{"another node, Jev", "/v1/nodes/db-1/executions/" + execution.String() + "/jev", `{}`, http.StatusForbidden},
		{"unknown execution", "/v1/nodes/web-1/executions/exec-ghost/model", `{}`, http.StatusNotFound},
		{"bad node id", "/v1/nodes/Bad%20Id/executions/" + execution.String() + "/model", `{}`, http.StatusBadRequest},
		{"bad body, model", "/v1/nodes/web-1/executions/" + execution.String() + "/model", `not json`, http.StatusBadRequest},
		{"bad body, Jev", "/v1/nodes/web-1/executions/" + execution.String() + "/jev", `not json`, http.StatusBadRequest},
	}
	for _, c := range calls {
		if status, body := post(t, server.URL+c.path, c.body); status != c.want {
			t.Errorf("%s: %d %s, want %d", c.name, status, body, c.want)
		}
	}
	if len(models.asked) != 0 {
		t.Errorf("a refused call reached the model: %+v", models.asked)
	}
}
