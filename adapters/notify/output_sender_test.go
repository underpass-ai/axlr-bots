package notify

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

var _ application.OutputSender = (*OutputSender)(nil)

const outputSecret = "a-webhook-secret-of-32-characters"

func outputTarget(t *testing.T, s domain.OutputTargetSnapshot) domain.OutputTarget {
	t.Helper()
	target, err := domain.NewOutputTarget(s)
	if err != nil {
		t.Fatal(err)
	}
	return target
}

func outputIncident(t *testing.T) domain.Incident {
	t.Helper()
	incident, err := domain.NewIncident("inc-1", "shop", "triage", "g", "alertmanager", "KubePodCrashLooping in shop", domain.Sev2, []string{"fp-a"}, t0)
	if err != nil {
		t.Fatal(err)
	}
	if err := incident.Diagnose("OOMKilled: limit 32Mi, usage 60Mi", t0); err != nil {
		t.Fatal(err)
	}
	return incident
}

// outputBody is the body the application queues for the target: the
// incident event as JSON, or its Block Kit message for slack.
func outputBody(t *testing.T, target domain.OutputTarget) domain.OutputDelivery {
	t.Helper()
	event := application.IncidentOutputEvent{
		Schema: application.IncidentOutputSchema, DeliveryID: "dlv-out", Event: "incident.diagnosed", EventID: "evt-1", Target: target.Name().String(),
		IncidentID: "inc-1", TenantID: "shop", BotName: "triage", Title: "KubePodCrashLooping in shop", Severity: "sev2", Status: "Diagnosed",
		Cycle: 1, Diagnosis: "OOMKilled: limit 32Mi, usage 60Mi", Links: application.IncidentOutputLinks{Panel: "https://panel.test/incidents/inc-1"},
	}
	var payload []byte
	var err error
	if target.Kind() == domain.OutputSlack {
		payload, err = json.Marshal(application.NewSlackIncidentMessage(event))
	} else {
		payload, err = json.Marshal(event)
	}
	if err != nil {
		t.Fatal(err)
	}
	d, err := domain.NewOutputDelivery("dlv-out", target, outputIncident(t), "evt-1", domain.EventIncidentDiagnosed, payload, t0)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// localReceiver is what an operator runs to try an output: it keeps
// each request's headers and body.
type localReceiver struct {
	mu       sync.Mutex
	headers  []http.Header
	bodies   [][]byte
	paths    []string
	statuses []int
	echo     bool
}

func (r *localReceiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	body, _ := io.ReadAll(req.Body)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.headers, r.bodies, r.paths = append(r.headers, req.Header.Clone()), append(r.bodies, body), append(r.paths, req.URL.Path)
	status := http.StatusOK
	if len(r.statuses) > 0 {
		status, r.statuses = r.statuses[0], r.statuses[1:]
	}
	w.WriteHeader(status)
	if r.echo {
		_, _ = w.Write([]byte("no_service"))
	}
}

// A webhook output is signed over the very bytes it posts, under the
// target's secret, with the incident event named in its header.
func TestAWebhookOutputIsSignedLikeAnApprovalWebhook(t *testing.T) {
	r := &localReceiver{}
	server := httptest.NewServer(r)
	defer server.Close()
	target := outputTarget(t, domain.OutputTargetSnapshot{Name: "hook", Type: "webhook", URL: server.URL + "/axlr", Secret: outputSecret})
	d := outputBody(t, target)
	if err := NewOutputSender().Send(context.Background(), target, d); err != nil {
		t.Fatal(err)
	}
	header, body := r.headers[0], r.bodies[0]
	check, _ := domain.NewTrigger(domain.TriggerSnapshot{Type: "webhook", SigningKey: outputSecret, Objective: "check"})
	if !check.Authenticates("", header.Get(SignatureHeader), body) {
		t.Errorf("the signature %q does not check", header.Get(SignatureHeader))
	}
	if header.Get(EventHeader) != "incident.diagnosed" || header.Get(DeliveryHeader) != "dlv-out" || header.Get("Idempotency-Key") != "dlv-out" || header.Get("Content-Type") != "application/json" {
		t.Errorf("headers = %v", header)
	}
	var event application.IncidentOutputEvent
	if err := json.Unmarshal(body, &event); err != nil || event.Schema != application.IncidentOutputSchema || event.IncidentID != "inc-1" || event.Diagnosis == "" {
		t.Errorf("body = %s %v", body, err)
	}
}

// A slack output posts the Block Kit message, unsigned: the incoming
// webhook's URL is the credential.
func TestASlackOutputPostsBlockKit(t *testing.T) {
	r := &localReceiver{}
	server := httptest.NewServer(r)
	defer server.Close()
	target := outputTarget(t, domain.OutputTargetSnapshot{Name: "chat", Type: "slack", URL: server.URL + "/services/T000/B000/SECRET"})
	if err := NewOutputSender().Send(context.Background(), target, outputBody(t, target)); err != nil {
		t.Fatal(err)
	}
	if r.paths[0] != "/services/T000/B000/SECRET" {
		t.Errorf("path = %s", r.paths[0])
	}
	for _, name := range []string{SignatureHeader, EventHeader, DeliveryHeader} {
		if r.headers[0].Get(name) != "" {
			t.Errorf("a slack post carries %s", name)
		}
	}
	var message struct {
		Text   string `json:"text"`
		Blocks []struct {
			Type string `json:"type"`
			Text *struct {
				Type, Text string
			} `json:"text"`
			Fields   []struct{ Type, Text string } `json:"fields"`
			Elements []struct {
				Type, URL string
			} `json:"elements"`
		} `json:"blocks"`
	}
	if err := json.Unmarshal(r.bodies[0], &message); err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, block := range message.Blocks {
		kinds = append(kinds, block.Type)
	}
	if strings.Join(kinds, ",") != "header,section,section,section,actions" || message.Text == "" {
		t.Fatalf("blocks = %v", kinds)
	}
	if message.Blocks[0].Text.Type != "plain_text" || !strings.Contains(message.Blocks[0].Text.Text, "SEV2") || len(message.Blocks[2].Fields) != 4 ||
		!strings.Contains(message.Blocks[3].Text.Text, "OOMKilled") || message.Blocks[4].Elements[0].URL != "https://panel.test/incidents/inc-1" {
		t.Errorf("message = %s", r.bodies[0])
	}
}

// What fails says why without the URL: a chat webhook's URL is its
// secret, and the error goes to the audit log and the daemon's log.
func TestAnOutputErrorNeverShowsTheURL(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	target := outputTarget(t, domain.OutputTargetSnapshot{Name: "chat", Type: "slack", URL: closed.URL + "/services/T/B/SECRET"})
	err := NewOutputSender().Send(context.Background(), target, outputBody(t, target))
	if err == nil || strings.Contains(err.Error(), "/services/") || strings.Contains(err.Error(), "SECRET") || !strings.HasPrefix(err.Error(), "output chat: ") {
		t.Errorf("closed port: %v", err)
	}
	r := &localReceiver{statuses: []int{http.StatusNotFound, http.StatusInternalServerError}, echo: true}
	server := httptest.NewServer(r)
	defer server.Close()
	target = outputTarget(t, domain.OutputTargetSnapshot{Name: "chat", Type: "slack", URL: server.URL + "/services/T/B/SECRET"})
	if err := NewOutputSender().Send(context.Background(), target, outputBody(t, target)); err == nil || err.Error() != "HTTP 404: no_service" {
		t.Errorf("404: %v", err)
	}
	r.echo = false
	if err := NewOutputSender().Send(context.Background(), target, outputBody(t, target)); err == nil || err.Error() != "HTTP 500" {
		t.Errorf("500: %v", err)
	}
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		select {
		case <-req.Context().Done():
		case <-time.After(2 * time.Second):
		}
	}))
	defer slow.Close()
	target = outputTarget(t, domain.OutputTargetSnapshot{Name: "chat", Type: "slack", URL: slow.URL + "/services/T/B/SECRET"})
	if err := (&OutputSender{Timeout: 50 * time.Millisecond}).Send(context.Background(), target, outputBody(t, target)); err == nil || err.Error() != "output chat: timed out" {
		t.Errorf("slow: %v", err)
	}
	if err := NewOutputSender().Send(context.Background(), domain.OutputTarget{}, outputBody(t, target)); err == nil {
		t.Error("a target of no type was delivered")
	}
}

// A command output reads the body on stdin, with the incident's ids in
// its environment and none of the daemon's secrets.
func TestACommandOutputReadsTheEventOnStdin(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "leak")
	dir := t.TempDir()
	out, env := filepath.Join(dir, "event.json"), filepath.Join(dir, "env")
	target := outputTarget(t, domain.OutputTargetSnapshot{Name: "relay", Type: "command", Command: []string{
		"/bin/sh", "-c", `cat > "$1" && printf '%s|%s|%s|%s|%s' "$AXLR_BOTS_DELIVERY_ID" "$AXLR_BOTS_INCIDENT_ID" "$AXLR_BOTS_TENANT_ID" "$AXLR_BOTS_EVENT" "$OPENROUTER_API_KEY" > "$2"`, "relay", out, env,
	}})
	d := outputBody(t, target)
	if err := NewOutputSender().Send(context.Background(), target, d); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(out); string(got) != string(d.Payload()) {
		t.Errorf("stdin = %s", got)
	}
	if got, _ := os.ReadFile(env); string(got) != "dlv-out|inc-1|shop|incident.diagnosed|" {
		t.Errorf("environment = %q", got)
	}
	for words, want := range map[string]string{`echo "teams said no" >&2; exit 3`: "exit status 3: teams said no", "exit 2": "exit status 2"} {
		failing := outputTarget(t, domain.OutputTargetSnapshot{Name: "relay", Type: "command", Command: []string{"/bin/sh", "-c", words}})
		if err := NewOutputSender().Send(context.Background(), failing, d); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v", words, err)
		}
	}
	slow := outputTarget(t, domain.OutputTargetSnapshot{Name: "relay", Type: "command", Command: []string{"/bin/sh", "-c", "sleep 5"}})
	if err := (&OutputSender{Timeout: 100 * time.Millisecond}).Send(context.Background(), slow, d); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Errorf("slow: %v", err)
	}
}
