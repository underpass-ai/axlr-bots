package unixapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/underpass-ai/axlr-bots/application"
	"github.com/underpass-ai/axlr-bots/domain"
)

const slackHook = "https://hooks.slack.test/services/T000/B000/SECRETPATH"

// The outputs over the socket and the operator address: added, listed
// and removed, and no answer ever carries a URL, a secret or a command.
func TestOutputsOverTheSocketAndTheOperatorAddress(t *testing.T) {
	o := serveOperatorAddress(t)
	ctx := context.Background()
	chat, err := o.socket.AddOutput(ctx, application.OutputDefinition{Name: "chat", Type: "slack", URL: slackHook, Severities: []string{"sev1", "sev2"}})
	if err != nil || chat.Host != "hooks.slack.test" || chat.Fingerprint == "" || len(chat.Severities) != 2 {
		t.Fatalf("add = %+v %v", chat, err)
	}
	if _, err := o.api.AddOutput(ctx, application.OutputDefinition{Name: "hook", Type: "webhook", URL: "https://receiver.test/axlr", Secret: "a-webhook-secret-of-32-characters"}); err != nil {
		t.Fatal(err)
	}
	var remote RemoteError
	if _, err := o.api.AddOutput(ctx, application.OutputDefinition{Name: "chat", Type: "slack", URL: slackHook}); !errors.As(err, &remote) || remote.Kind != KindConflict {
		t.Errorf("added twice = %v", err)
	}
	if _, err := o.socket.AddOutput(ctx, application.OutputDefinition{Name: "bad", Type: "email"}); err == nil {
		t.Error("an email output was added")
	}
	listed, err := o.api.ListOutputs(ctx)
	if err != nil || len(listed) != 2 || listed[0].Name != "chat" || listed[1].Name != "hook" {
		t.Fatalf("list = %+v %v", listed, err)
	}
	for _, path := range []string{"/v1/outputs"} {
		request, _ := http.NewRequest(http.MethodGet, o.url+path, nil)
		request.Header.Set("Authorization", "Bearer "+testToken)
		response, err := o.http.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := io.ReadAll(response.Body)
		response.Body.Close()
		for _, secret := range []string{"SECRETPATH", "/services/", "a-webhook-secret", "/axlr"} {
			if bytes.Contains(raw, []byte(secret)) {
				t.Errorf("GET %s shows %q: %s", path, secret, raw)
			}
		}
	}
	status, body := o.post(t, "/v1/outputs", map[string]string{"Authorization": "Bearer " + testToken}, []byte(`{"name":"relay","type":"command","command":["/usr/local/bin/relay","--token","t0ps3cr3t"]}`))
	if status != http.StatusCreated || body["type"] != "command" || body["fingerprint"] == "" {
		t.Errorf("POST = %d %v", status, body)
	}
	if raw, _ := json.Marshal(body); strings.Contains(string(raw), "t0ps3cr3t") || strings.Contains(string(raw), "/usr/local/bin/relay") {
		t.Errorf("the answer shows the command: %s", raw)
	}
	removed, err := o.api.RemoveOutput(ctx, "chat")
	if err != nil || removed.Fingerprint != chat.Fingerprint {
		t.Errorf("remove = %+v %v", removed, err)
	}
	if _, err := o.socket.RemoveOutput(ctx, "chat"); !domain.IsNotFound(err) {
		t.Errorf("removed twice = %v", err)
	}
	if left, _ := o.socket.ListOutputs(ctx); len(left) != 2 {
		t.Errorf("left = %+v", left)
	}
}

// An incident opened over the socket is queued to the outputs that want
// it, in the outbox beside the approvals'.
func TestAnIncidentReachesTheOutputs(t *testing.T) {
	client, local, _ := serve(t, "completed")
	ctx := context.Background()
	d := definition("triage", "observe")
	d.Triggers = []domain.TriggerSnapshot{{Type: "incident"}}
	if _, err := client.RegisterBot(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnableBot(ctx, "triage"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.AddOutput(ctx, application.OutputDefinition{Name: "chat", Type: "slack", URL: slackHook}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.AddOutput(ctx, application.OutputDefinition{Name: "sev1-only", Type: "slack", URL: slackHook, Severities: []string{"sev1"}}); err != nil {
		t.Fatal(err)
	}
	opened, err := client.OpenIncident(ctx, application.OpenIncidentCommand{Tenant: "ops", BotID: "triage", GroupKey: "g", Title: "Disk full", Severity: "sev2"})
	if err != nil {
		t.Fatal(err)
	}
	pending, err := local.Dependencies().OutputDeliveries.ListPending(ctx)
	if err != nil || len(pending) != 1 || pending[0].Output() != "chat" || pending[0].IncidentID().String() != opened.Incident.ID || pending[0].Event() != domain.EventIncidentOpened {
		t.Fatalf("pending = %+v %v", pending, err)
	}
	if !strings.Contains(string(pending[0].Payload()), `"blocks"`) {
		t.Errorf("payload = %s", pending[0].Payload())
	}
}
