package unixapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/adapters/pki"
	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/compose"
	"github.com/underpass-ai/axlr-bots/domain"
	"github.com/underpass-ai/axlr-bots/internal/fakeaxlr"
)

const testSecret = "whsec-0123456789abcdef"

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

// webhookAddress is a control plane serving the socket and the webhook
// address over TLS.
type webhookAddress struct {
	local  *application.LocalOperator
	socket *Client
	url    string
	http   *http.Client
}

func serveWebhookAddress(t *testing.T) *webhookAddress {
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
	local := application.NewLocalOperator(deps)
	server := NewServer(local)
	socket := filepath.Join(dir, "cp.sock")
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 2)
	go func() { done <- server.Serve(ctx, socket) }()
	go func() { done <- server.ServeWebhooks(ctx, listener, pki.ServerTLSConfig(ca, []string{"127.0.0.1"})) }()
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
	return &webhookAddress{
		local: local, socket: NewClient(socket), url: "https://" + listener.Addr().String(),
		http: &http.Client{Transport: &http.Transport{TLSClientConfig: config.Clone(), ForceAttemptHTTP2: true}},
	}
}

// post sends a raw request to the webhook address.
func (w *webhookAddress) post(t *testing.T, path string, headers map[string]string, body []byte) (int, map[string]any) {
	t.Helper()
	request, _ := http.NewRequest(http.MethodPost, w.url+path, bytes.NewReader(body))
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := w.http.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	var decoded map[string]any
	_ = json.Unmarshal(raw, &decoded)
	return response.StatusCode, decoded
}

func TestTheWebhookAddressServesTriggersAndNothingElse(t *testing.T) {
	w := serveWebhookAddress(t)
	ctx := context.Background()
	if _, err := w.local.RegisterBot(ctx, triggered("bot-o")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.local.EnableBot(ctx, "bot-o"); err != nil {
		t.Fatal(err)
	}
	response, err := w.http.Get(w.url + "/healthz")
	if err != nil || response.StatusCode != http.StatusOK {
		t.Errorf("healthz: %v", err)
	}
	if response != nil {
		response.Body.Close()
	}
	// No operator route and no node route is on it: the operator API is
	// gRPC, the node protocol has its own address.
	for _, path := range []string{"/v1/bots", "/v1/tasks", "/v1/nodes/web-1/heartbeat", "/v1/enroll", "/v1/nodes", "/v1/overview"} {
		if status, _ := w.post(t, path, nil, []byte(`{}`)); status != http.StatusNotFound && status != http.StatusMethodNotAllowed {
			t.Errorf("%s answered %d on the webhook address", path, status)
		}
	}
	// The trigger's secret is the credential.
	body := []byte(`{"type": "deploy", "service": "web"}`)
	if status, decoded := w.post(t, "/v1/triggers/bot-o/deploy", nil, body); status != http.StatusForbidden || decoded["kind"] != application.KindForbidden {
		t.Errorf("no secret: %d %v", status, decoded)
	}
	if status, _ := w.post(t, "/v1/triggers/bot-o/deploy", map[string]string{WebhookSecretHeader: "wrong-wrong-wrong-wrong"}, body); status != http.StatusForbidden {
		t.Errorf("wrong secret: %d", status)
	}
	status, first := w.post(t, "/v1/triggers/bot-o/deploy", map[string]string{WebhookSecretHeader: testSecret, IdempotencyKeyHeader: "gh-1"}, body)
	if status != http.StatusCreated || first["created"] != true {
		t.Fatalf("with secret: %d %v", status, first)
	}
	status, again := w.post(t, "/v1/triggers/bot-o/deploy", map[string]string{WebhookSecretHeader: testSecret, IdempotencyKeyHeader: "gh-1"}, body)
	if status != http.StatusOK || again["created"] != false || again["task"].(map[string]any)["id"] != first["task"].(map[string]any)["id"] {
		t.Errorf("redelivery: %d %v", status, again)
	}
	mac := hmac.New(sha256.New, []byte(testSecret))
	mac.Write(body)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if status, _ := w.post(t, "/v1/triggers/bot-o/signed", map[string]string{WebhookSignatureHeader: signature}, body); status != http.StatusCreated {
		t.Errorf("signed: %d", status)
	}
	if status, _ := w.post(t, "/v1/triggers/bot-o/signed", map[string]string{"X-Hub-Signature-256": signature}, body); status != http.StatusOK {
		t.Errorf("github header: %d", status)
	}
	if status, _ := w.post(t, "/v1/triggers/bot-o/signed", map[string]string{WebhookSignatureHeader: signature}, []byte(`{"type": "tampered"}`)); status != http.StatusForbidden {
		t.Errorf("tampered body: %d", status)
	}
	for _, path := range []string{"/v1/triggers/bot-o/nope", "/v1/triggers/nobody/deploy", "/v1/triggers/Bad%20Id/deploy", "/v1/triggers/bot-o/tick"} {
		if status, _ := w.post(t, path, map[string]string{WebhookSecretHeader: testSecret}, body); status != http.StatusForbidden {
			t.Errorf("%s: %d, want 403", path, status)
		}
	}
	if status, decoded := w.post(t, "/v1/triggers/bot-o/deploy", map[string]string{WebhookSecretHeader: testSecret}, []byte(`[1, 2]`)); status != http.StatusBadRequest || decoded["kind"] != application.KindValidation {
		t.Errorf("array body: %d %v", status, decoded)
	}
	huge := append([]byte(`{"type": "deploy", "service": "`), bytes.Repeat([]byte("x"), domain.MaxTaskInputBytes)...)
	huge = append(huge, []byte(`"}`)...)
	if status, decoded := w.post(t, "/v1/triggers/bot-o/deploy", map[string]string{WebhookSecretHeader: testSecret}, huge); status != http.StatusRequestEntityTooLarge || decoded["kind"] != application.KindTooLarge {
		t.Errorf("huge body: %d %v", status, decoded)
	}
	events, err := w.local.ListEvents(ctx, "bot-o", 0)
	fired := 0
	for _, e := range events {
		if e.Type == domain.EventTriggerFired {
			fired++
		}
	}
	if err != nil || fired != 4 {
		t.Errorf("fired events: %v %d", err, fired)
	}
}

// Over the socket the caller is trusted, so the command fires a trigger
// with no secret, and the answer comes back whole.
func TestAnEventFiresATriggerOverTheSocket(t *testing.T) {
	w := serveWebhookAddress(t)
	ctx := context.Background()
	if _, err := w.local.RegisterBot(ctx, triggered("bot-c")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.local.EnableBot(ctx, "bot-c"); err != nil {
		t.Fatal(err)
	}
	request := application.FireRequest{BotID: "bot-c", Trigger: "deploy", DeliveryID: "d-1", Body: json.RawMessage(`{"type": "deploy", "service": "web"}`)}
	first, err := w.socket.FireTrigger(ctx, request)
	if err != nil || !first.Created || first.Task == nil || first.Trigger.Name != "deploy" {
		t.Fatalf("fire: %v %+v", err, first)
	}
	again, err := w.socket.FireTrigger(ctx, request)
	if err != nil || again.Created || again.Task == nil || again.Task.ID != first.Task.ID {
		t.Errorf("redelivery: %v %+v", err, again)
	}
	if _, err := w.socket.FireTrigger(ctx, application.FireRequest{BotID: "bot-c", Trigger: "nope", Body: json.RawMessage(`{}`)}); err == nil {
		t.Error("an unknown trigger fired")
	}
}

func TestSimultaneousRedeliveriesMakeOneTask(t *testing.T) {
	_, local, _ := serve(t, "completed")
	ctx := context.Background()
	if _, err := local.RegisterBot(ctx, triggered("bot-c")); err != nil {
		t.Fatal(err)
	}
	if _, err := local.EnableBot(ctx, "bot-c"); err != nil {
		t.Fatal(err)
	}
	const deliveries = 12
	results := make(chan application.FireResponse, deliveries)
	for range deliveries {
		go func() {
			response, err := local.FireTrigger(ctx, application.FireRequest{BotID: "bot-c", Trigger: "deploy", DeliveryID: "burst-1", Body: json.RawMessage(`{"type": "deploy", "service": "web"}`)})
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
