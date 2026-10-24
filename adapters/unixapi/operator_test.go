package unixapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/pki"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/compose"
	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/internal/fakeaxlr"
)

const (
	testSecret = "whsec-0123456789abcdef"
	testToken  = "operator-token-for-tests"
)

// triggered is a definition with a manual, a webhook and a timer trigger.
func triggered(name string) application.BotDefinition {
	d := definition(name, "observe")
	d.Triggers = []domain.TriggerSnapshot{
		{Type: "manual"},
		{Type: "webhook", Name: "deploy", Objective: "Handle event {{.type}} for {{.service}}", Secret: testSecret},
		{Type: "webhook", Name: "signed", Objective: "Signed {{.type}}", SigningKey: testSecret},
		{Type: "timer", Name: "tick", Objective: "say the time", Every: "10m"},
	}
	return d
}

func TestOperatorRoutesOverTheSocket(t *testing.T) {
	client, local, _ := serve(t, "slow")
	ctx := context.Background()
	bot, err := client.RegisterBot(ctx, triggered("bot-t"))
	if err != nil {
		t.Fatal(err)
	}
	if len(bot.Triggers) != 4 || bot.Triggers[1].Secret != domain.Redacted || bot.Triggers[2].SigningKey != domain.Redacted {
		t.Errorf("the API should redact the credentials: %+v", bot.Triggers)
	}
	stored, _ := local.Dependencies().Bots.Get(ctx, "bot-t")
	if stored.Snapshot().Triggers[1].Secret != testSecret {
		t.Error("the repository should keep the secret")
	}
	if _, err := client.EnableBot(ctx, "bot-t"); err != nil {
		t.Fatal(err)
	}
	mission := "A patched mission"
	patched, err := client.UpdateBot(ctx, "bot-t", application.BotPatch{Mission: &mission})
	if err != nil || patched.Mission != mission || patched.Triggers[1].Secret != domain.Redacted {
		t.Errorf("patch: %v %+v", err, patched)
	}
	var remote RemoteError
	if _, err := client.UpdateBot(ctx, "bot-t", application.BotPatch{}); !errorsAs(err, &remote) || remote.Kind != KindValidation {
		t.Errorf("empty patch: %v", err)
	}
	if _, err := client.UpdateBot(ctx, "nobody", application.BotPatch{Mission: &mission}); !domain.IsNotFound(err) {
		t.Errorf("patch unknown bot: %v", err)
	}
	events, err := client.ListEvents(ctx, "bot-t", 0)
	if err != nil || len(events) != 3 || events[2].Type != domain.EventBotUpdated {
		t.Errorf("events: %v %+v", err, events)
	}
	if two, _ := client.ListEvents(ctx, "bot-t", 2); len(two) != 2 || two[0].Type != domain.EventBotEnabled {
		t.Errorf("limited events: %+v", two)
	}
	if _, err := client.ListEvents(ctx, "nobody", 1); !domain.IsNotFound(err) {
		t.Errorf("events of an unknown bot: %v", err)
	}

	// The webhook over the socket needs no secret: the caller is trusted.
	body := json.RawMessage(`{"type": "deploy", "service": "web"}`)
	fired, err := client.FireTrigger(ctx, FireRequest{BotID: "bot-t", Trigger: "deploy", DeliveryID: "d-1", Body: body})
	if err != nil || !fired.Created || fired.Task.Objective != "Handle event deploy for web" || fired.Trigger.Name != "deploy" || !strings.HasPrefix(fired.IdempotencyKey, "webhook:deploy:") {
		t.Fatalf("fire: %v %+v", err, fired)
	}
	again, err := client.FireTrigger(ctx, FireRequest{BotID: "bot-t", Trigger: "deploy", DeliveryID: "d-1", Body: body})
	if err != nil || again.Created || again.Task.ID != fired.Task.ID {
		t.Errorf("redelivery: %v %+v", err, again)
	}
	if _, err := client.FireTrigger(ctx, FireRequest{BotID: "bot-t", Trigger: "nope", Body: body}); !domain.IsNotFound(err) {
		t.Errorf("unknown trigger: %v", err)
	}
	if _, err := client.FireTrigger(ctx, FireRequest{BotID: "bot-t", Trigger: "deploy", Body: json.RawMessage(`"text"`)}); !errorsAs(err, &remote) || remote.Kind != KindValidation {
		t.Errorf("non-object body: %v", err)
	}

	// Cancel: a Pending task, then a Running one through the API while
	// the control plane runs it in process.
	cancelled, err := client.CancelTask(ctx, fired.Task.ID)
	if err != nil || !cancelled.Changed || cancelled.Task.Status != "Cancelled" || cancelled.Execution != nil {
		t.Errorf("cancel pending: %v %+v", err, cancelled)
	}
	if same, err := client.CancelTask(ctx, fired.Task.ID); err != nil || same.Changed {
		t.Errorf("cancel again: %v %+v", err, same)
	}
	running, err := client.SubmitTask(ctx, SubmitRequest{BotID: "bot-t", Objective: "take a while", IdempotencyKey: "slow-1"})
	if err != nil {
		t.Fatal(err)
	}
	ran := make(chan RunResponse, 1)
	go func() {
		result, _ := client.RunTask(ctx, running.Task.ID)
		ran <- result
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		task, _ := client.GetTask(ctx, running.Task.ID)
		if task.Status == "Running" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the task never ran")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancelled, err = client.CancelTask(ctx, running.Task.ID)
	if err != nil || !cancelled.Changed || cancelled.Execution == nil || cancelled.Execution.Outcome != "Cancelled" {
		t.Fatalf("cancel running: %v %+v", err, cancelled)
	}
	select {
	case result := <-ran:
		if result.Task.Status != "Cancelled" || result.Execution.Outcome != "Cancelled" {
			t.Errorf("interrupted run = %s / %s", result.Task.Status, result.Execution.Outcome)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the interrupted run did not return")
	}
	executions, err := client.ListExecutions(ctx, running.Task.ID)
	if err != nil || len(executions) != 1 || executions[0].Outcome != "Cancelled" || executions[0].Correlation == nil || executions[0].Correlation.TraceID != running.Task.CorrelationID {
		t.Errorf("executions: %v %+v", err, executions)
	}
	if _, err := client.ListExecutions(ctx, "nobody"); !domain.IsNotFound(err) {
		t.Errorf("executions of an unknown task: %v", err)
	}
	if _, err := client.CancelTask(ctx, "bad id"); !errorsAs(err, &remote) || remote.Kind != KindValidation {
		t.Errorf("cancel bad id: %v", err)
	}
	// A completed task is refused.
	done, _ := client.SubmitTask(ctx, SubmitRequest{BotID: "bot-t", Objective: "quick", IdempotencyKey: "done-1"})
	if _, err := client.RunTask(ctx, done.Task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CancelTask(ctx, done.Task.ID); !errorsAs(err, &remote) || remote.Kind != KindState {
		t.Errorf("cancel completed: %v", err)
	}
}

func TestPanelRoutesOverTheSocket(t *testing.T) {
	client, _, _ := serve(t, "complete")
	ctx := context.Background()
	if _, err := client.RegisterBot(ctx, definition("bot-p", "observe")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableBot(ctx, "bot-p"); err != nil {
		t.Fatal(err)
	}
	first, _ := client.SubmitTask(ctx, SubmitRequest{BotID: "bot-p", Objective: "first", IdempotencyKey: "p-1"})
	if _, err := client.RunTask(ctx, first.Task.ID); err != nil {
		t.Fatal(err)
	}
	second, _ := client.SubmitTask(ctx, SubmitRequest{BotID: "bot-p", Objective: "second", IdempotencyKey: "p-2"})

	tasks, err := client.ListTasks(ctx, application.TaskQuery{})
	if err != nil || len(tasks) != 2 || tasks[0].ID != second.Task.ID {
		t.Errorf("tasks: %v %+v", err, tasks)
	}
	completed, err := client.ListTasks(ctx, application.TaskQuery{BotID: "bot-p", Status: "Completed", Since: time.Now().Add(-time.Hour), Limit: 5})
	if err != nil || len(completed) != 1 || completed[0].ID != first.Task.ID {
		t.Errorf("completed: %v %+v", err, completed)
	}
	if later, err := client.ListTasks(ctx, application.TaskQuery{Since: time.Now().Add(time.Hour)}); err != nil || len(later) != 0 {
		t.Errorf("later: %v %+v", err, later)
	}
	var remote RemoteError
	if _, err := client.ListTasks(ctx, application.TaskQuery{Status: "Sleeping"}); !errorsAs(err, &remote) || remote.Kind != KindValidation {
		t.Errorf("bad status: %v", err)
	}
	if _, err := client.ListTasks(ctx, application.TaskQuery{BotID: "nobody"}); !domain.IsNotFound(err) {
		t.Errorf("unknown bot: %v", err)
	}

	timeline, err := client.ListTaskEvents(ctx, first.Task.ID, 0)
	if err != nil || len(timeline) != 3 || timeline[0].Type != domain.EventTaskSubmitted || timeline[2].Type != domain.EventTaskCompleted {
		t.Errorf("timeline: %v %+v", err, timeline)
	}
	if last, err := client.ListTaskEvents(ctx, first.Task.ID, 1); err != nil || len(last) != 1 {
		t.Errorf("last event: %v %+v", err, last)
	}
	if _, err := client.ListTaskEvents(ctx, "task-none", 0); !domain.IsNotFound(err) {
		t.Errorf("unknown task: %v", err)
	}
	if _, err := client.ListTaskEvents(ctx, "bad id", 0); !errorsAs(err, &remote) || remote.Kind != KindValidation {
		t.Errorf("bad task id: %v", err)
	}

	overview, err := client.GetOverview(ctx, time.Time{})
	if err != nil || overview.Bots["Enabled"] != 1 || overview.Tasks["Completed"] != 1 || overview.Tasks["Pending"] != 1 || overview.TasksSince["Pending"] != 1 || overview.ExecutionsSince != 1 {
		t.Errorf("overview: %v %+v", err, overview)
	}
	if later, err := client.GetOverview(ctx, time.Now().Add(time.Hour)); err != nil || later.ExecutionsSince != 0 || later.Tasks["Completed"] != 1 || later.TasksSince["Completed"] != 0 {
		t.Errorf("overview later: %v %+v", err, later)
	}

	paused, err := client.PauseBot(ctx, "bot-p")
	if err != nil || paused.Status != "Paused" {
		t.Errorf("pause: %v %+v", err, paused)
	}
	if _, err := client.SubmitTask(ctx, SubmitRequest{BotID: "bot-p", Objective: "third", IdempotencyKey: "p-3"}); !errorsAs(err, &remote) || remote.Kind != KindState {
		t.Errorf("a paused bot accepted a task: %v", err)
	}
	if _, err := client.PauseBot(ctx, "bot-p"); !errorsAs(err, &remote) || remote.Kind != KindState {
		t.Errorf("pause twice: %v", err)
	}
	if _, err := client.PauseBot(ctx, "Bad Id"); !errorsAs(err, &remote) || remote.Kind != KindValidation {
		t.Errorf("pause bad id: %v", err)
	}
	if enabled, err := client.EnableBot(ctx, "bot-p"); err != nil || enabled.Status != "Enabled" {
		t.Errorf("enable after pause: %v %+v", err, enabled)
	}
}

func TestPanelRoutesRefuseBadQueries(t *testing.T) {
	_, _, dir := serve(t, "complete")
	socket := filepath.Join(dir, "cp.sock")
	httpClient := &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", socket)
	}}}
	for _, path := range []string{
		"/v1/tasks?limit=x", "/v1/tasks?limit=-1", "/v1/tasks?since=yesterday",
		"/v1/overview?since=yesterday", "/v1/tasks/task-1/events?limit=x", "/v1/bots/bot-1/events?limit=x",
	} {
		response, err := httpClient.Get("http://axlr-bots" + path)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", path, response.StatusCode)
		}
	}
}

func TestTaskRoutesTakeTheKeyFromTheHeaderOrTheBody(t *testing.T) {
	_, local, dir := serve(t, "completed")
	ctx := context.Background()
	if _, err := local.RegisterBot(ctx, definition("bot-h", "observe")); err != nil {
		t.Fatal(err)
	}
	_, _ = local.EnableBot(ctx, "bot-h")
	httpClient := NewClient(filepath.Join(dir, "cp.sock")).http
	post := func(path, key, body string) (int, map[string]any) {
		t.Helper()
		request, _ := http.NewRequest(http.MethodPost, "http://axlr-bots"+path, strings.NewReader(body))
		if key != "" {
			request.Header.Set(IdempotencyKeyHeader, key)
		}
		response, err := httpClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var decoded map[string]any
		_ = json.NewDecoder(response.Body).Decode(&decoded)
		return response.StatusCode, decoded
	}
	status, first := post("/v1/bots/bot-h/tasks", "h-1", `{"objective": "o"}`)
	if status != http.StatusCreated || first["created"] != true {
		t.Errorf("header key: %d %v", status, first)
	}
	status, again := post("/v1/bots/bot-h/tasks", "h-1", `{"objective": "o", "idempotencyKey": "other"}`)
	if status != http.StatusOK || again["created"] != false {
		t.Errorf("the header should win over the body: %d %v", status, again)
	}
	if status, body := post("/v1/bots/bot-h/tasks", "", `{"objective": "o", "idempotencyKey": "b-1"}`); status != http.StatusCreated || body["created"] != true {
		t.Errorf("body key: %d %v", status, body)
	}
	if status, body := post("/v1/bots/bot-h/tasks", "", `{"objective": "o"}`); status != http.StatusBadRequest || body["kind"] != KindValidation {
		t.Errorf("no key: %d %v", status, body)
	}
	if status, _ := post("/v1/bots/bot-h/tasks", "x", `{"objective": "o", "unknown": 1}`); status != http.StatusBadRequest {
		t.Errorf("unknown field: %d", status)
	}
	if status, body := post("/v1/tasks", "t-1", `{"botId": "bot-h", "objective": "o"}`); status != http.StatusCreated || body["created"] != true {
		t.Errorf("legacy route with header: %d %v", status, body)
	}
	if status, _ := post("/v1/bots/nobody/tasks", "n-1", `{"objective": "o"}`); status != http.StatusNotFound {
		t.Errorf("unknown bot: %d", status)
	}
	if status, _ := post("/v1/bots/bot-h/events?limit=x", "", ``); status != http.StatusMethodNotAllowed {
		t.Errorf("post to events: %d", status)
	}
	request, _ := http.NewRequest(http.MethodGet, "http://axlr-bots/v1/bots/bot-h/events?limit=x", nil)
	if response, err := httpClient.Do(request); err != nil || response.StatusCode != http.StatusBadRequest {
		t.Errorf("bad limit: %v", err)
	}
}

// operatorAddress serves the socket and the operator address over TLS.
type operatorAddress struct {
	socket  *Client
	url     string
	ca      pki.Authority
	http    *http.Client
	api     *Client
	local   *Local
	noToken *Client
}

func serveOperatorAddress(t *testing.T) *operatorAddress {
	t.Helper()
	dir := t.TempDir()
	bin, _, err := fakeaxlr.Write(dir, fakeaxlr.Options{Scenario: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, "state")
	deps, err := compose.Dependencies(state, bin, "test:api")
	if err != nil {
		t.Fatal(err)
	}
	ca, err := pki.Generate(filepath.Join(state, pki.CADirName))
	if err != nil {
		t.Fatal(err)
	}
	local := NewLocal(deps)
	server := NewServer(local)
	socket := filepath.Join(dir, "cp.sock")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 2)
	go func() { done <- server.Serve(ctx, socket) }()
	go func() {
		done <- server.ServeAPI(ctx, listener, pki.ServerTLSConfig(ca, []string{"127.0.0.1"}), testToken)
	}()
	t.Cleanup(func() {
		cancel()
		for range 2 {
			if err := <-done; err != nil {
				t.Error(err)
			}
		}
	})
	for !Reachable(context.Background(), socket) {
		time.Sleep(10 * time.Millisecond)
	}
	config, err := pki.EnrollTLSConfig(ca.CertificatePEM())
	if err != nil {
		t.Fatal(err)
	}
	url := "https://" + listener.Addr().String()
	return &operatorAddress{
		socket: NewClient(socket), url: url, ca: ca, local: local,
		http:    &http.Client{Transport: &http.Transport{TLSClientConfig: config.Clone(), ForceAttemptHTTP2: true}},
		api:     NewTLSClient(url, config.Clone()).WithToken(testToken),
		noToken: NewTLSClient(url, config.Clone()),
	}
}

// post sends a raw request to the operator address.
func (o *operatorAddress) post(t *testing.T, path string, headers map[string]string, body []byte) (int, map[string]any) {
	t.Helper()
	request, _ := http.NewRequest(http.MethodPost, o.url+path, bytes.NewReader(body))
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := o.http.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	var decoded map[string]any
	_ = json.Unmarshal(raw, &decoded)
	return response.StatusCode, decoded
}

func TestOperatorAddressTakesTheTokenAndTheWebhooksTheirSecret(t *testing.T) {
	o := serveOperatorAddress(t)
	ctx := context.Background()
	if _, err := o.socket.RegisterBot(ctx, triggered("bot-o")); err != nil {
		t.Fatal(err)
	}
	if _, err := o.socket.EnableBot(ctx, "bot-o"); err != nil {
		t.Fatal(err)
	}
	// The token opens the operator routes; without it, or with a wrong
	// one, nothing but the health check answers.
	if bots, err := o.api.ListBots(ctx); err != nil || len(bots) != 1 || bots[0].Triggers[1].Secret != domain.Redacted {
		t.Errorf("with token: %v %+v", err, bots)
	}
	var unauthorized UnauthorizedError
	if _, err := o.noToken.ListBots(ctx); !errors.As(err, &unauthorized) {
		t.Errorf("without token: %v", err)
	}
	if _, err := o.noToken.WithToken("wrong").GetBot(ctx, "bot-o"); !errors.As(err, &unauthorized) {
		t.Errorf("wrong token: %v", err)
	}
	response, err := o.http.Get(o.url + "/healthz")
	if err != nil || response.StatusCode != http.StatusOK {
		t.Errorf("healthz: %v", err)
	}
	if response != nil {
		response.Body.Close()
	}
	// No node route is on the operator address, token or not.
	for _, path := range []string{"/v1/nodes/web-1/heartbeat", "/v1/enroll", "/v1/nodes"} {
		if status, _ := o.post(t, path, map[string]string{"Authorization": "Bearer " + testToken}, []byte(`{}`)); status != http.StatusNotFound && status != http.StatusMethodNotAllowed {
			t.Errorf("%s answered %d on the operator address", path, status)
		}
	}
	submitted, err := o.api.SubmitTask(ctx, SubmitRequest{BotID: "bot-o", Objective: "o", IdempotencyKey: "api-1"})
	if err != nil || !submitted.Created {
		t.Errorf("submit over the operator address: %v", err)
	}

	// Webhooks: the trigger's secret is the credential, not the token.
	body := []byte(`{"type": "deploy", "service": "web"}`)
	if status, decoded := o.post(t, "/v1/triggers/bot-o/deploy", nil, body); status != http.StatusForbidden || decoded["kind"] != KindForbidden {
		t.Errorf("no secret: %d %v", status, decoded)
	}
	if status, _ := o.post(t, "/v1/triggers/bot-o/deploy", map[string]string{WebhookSecretHeader: "wrong-wrong-wrong-wrong"}, body); status != http.StatusForbidden {
		t.Errorf("wrong secret: %d", status)
	}
	if status, _ := o.post(t, "/v1/triggers/bot-o/deploy", map[string]string{"Authorization": "Bearer " + testToken}, body); status != http.StatusForbidden {
		t.Errorf("the operator token is not a webhook credential: %d", status)
	}
	status, first := o.post(t, "/v1/triggers/bot-o/deploy", map[string]string{WebhookSecretHeader: testSecret, IdempotencyKeyHeader: "gh-1"}, body)
	if status != http.StatusCreated || first["created"] != true {
		t.Fatalf("with secret: %d %v", status, first)
	}
	status, again := o.post(t, "/v1/triggers/bot-o/deploy", map[string]string{WebhookSecretHeader: testSecret, IdempotencyKeyHeader: "gh-1"}, body)
	if status != http.StatusOK || again["created"] != false || again["task"].(map[string]any)["id"] != first["task"].(map[string]any)["id"] {
		t.Errorf("redelivery: %d %v", status, again)
	}
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write(body)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if status, _ := o.post(t, "/v1/triggers/bot-o/signed", map[string]string{WebhookSignatureHeader: signature}, body); status != http.StatusCreated {
		t.Errorf("signed: %d", status)
	}
	if status, _ := o.post(t, "/v1/triggers/bot-o/signed", map[string]string{"X-Hub-Signature-256": signature}, body); status != http.StatusOK {
		t.Errorf("github header: %d", status)
	}
	if status, _ := o.post(t, "/v1/triggers/bot-o/signed", map[string]string{WebhookSignatureHeader: signature}, []byte(`{"type": "tampered"}`)); status != http.StatusForbidden {
		t.Errorf("tampered body: %d", status)
	}
	for _, path := range []string{"/v1/triggers/bot-o/nope", "/v1/triggers/nobody/deploy", "/v1/triggers/Bad%20Id/deploy", "/v1/triggers/bot-o/tick"} {
		if status, _ := o.post(t, path, map[string]string{WebhookSecretHeader: testSecret}, body); status != http.StatusForbidden {
			t.Errorf("%s: %d, want 403", path, status)
		}
	}
	if status, decoded := o.post(t, "/v1/triggers/bot-o/deploy", map[string]string{WebhookSecretHeader: testSecret}, []byte(`[1, 2]`)); status != http.StatusBadRequest || decoded["kind"] != KindValidation {
		t.Errorf("array body: %d %v", status, decoded)
	}
	huge := append([]byte(`{"type": "deploy", "service": "`), bytes.Repeat([]byte("x"), domain.MaxTaskInputBytes)...)
	huge = append(huge, []byte(`"}`)...)
	if status, decoded := o.post(t, "/v1/triggers/bot-o/deploy", map[string]string{WebhookSecretHeader: testSecret}, huge); status != http.StatusRequestEntityTooLarge || decoded["kind"] != KindTooLarge {
		t.Errorf("huge body: %d %v", status, decoded)
	}
	if events, err := o.api.ListEvents(ctx, "bot-o", 0); err != nil || countType(events, domain.EventTriggerFired) != 4 {
		t.Errorf("fired events: %v %d", err, countType(events, domain.EventTriggerFired))
	}
}

func countType(events []domain.Event, kind domain.EventType) int {
	n := 0
	for _, e := range events {
		if e.Type == kind {
			n++
		}
	}
	return n
}

func TestServeAPINeedsAToken(t *testing.T) {
	deps, _ := compose.Dependencies(filepath.Join(t.TempDir(), "state"), "/nonexistent", "test:api")
	listener, _ := net.Listen("tcp", "127.0.0.1:0")
	defer listener.Close()
	if err := NewServer(NewLocal(deps)).ServeAPI(context.Background(), listener, nil, ""); err == nil {
		t.Error("served without a token")
	}
}

func TestSimultaneousRedeliveriesMakeOneTask(t *testing.T) {
	client, _, _ := serve(t, "completed")
	ctx := context.Background()
	if _, err := client.RegisterBot(ctx, triggered("bot-c")); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableBot(ctx, "bot-c"); err != nil {
		t.Fatal(err)
	}
	const deliveries = 12
	results := make(chan FireResponse, deliveries)
	for range deliveries {
		go func() {
			response, err := client.FireTrigger(ctx, FireRequest{BotID: "bot-c", Trigger: "deploy", DeliveryID: "burst-1", Body: json.RawMessage(`{"type": "deploy", "service": "web"}`)})
			if err != nil {
				t.Error(err)
			}
			results <- response
		}()
	}
	created, ids := 0, map[string]bool{}
	for range deliveries {
		response := <-results
		if response.Created {
			created++
		}
		ids[response.Task.ID] = true
	}
	if created != 1 || len(ids) != 1 {
		t.Errorf("%d tasks created, %d distinct ids, want one of each", created, len(ids))
	}
}
